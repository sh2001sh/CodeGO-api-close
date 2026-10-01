package commerce

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// CreatePackageCheckout persistently suspends the old package before pricing a
// renewal or upgrade. The same request key resumes after an outstanding stream.
func (s *Service) CreatePackageCheckout(ctx context.Context, in CreateOrder) (Order, error) {
	var o Order
	if in.UserID <= 0 || in.PlanID <= 0 || in.ProductID != "" || !in.Selection.validFor(in.Provider) || (in.PurchaseType != "" && in.PurchaseType != "normal" && !isGroupPurchase(in.PurchaseType)) || (in.PurchaseAction != "auto" && in.PurchaseAction != "renew" && in.PurchaseAction != "upgrade") || (in.GroupBuyID != 0 && in.PurchaseType != "join_group") {
		return o, ErrInvalid
	}
	if s.providers[in.Provider] == nil {
		return o, ErrProviderUnavailable
	}
	if !s.allowedReturn(in.SuccessURL) || !s.allowedReturn(in.CancelURL) {
		return o, ErrInvalid
	}
	if in.RequestID == "" {
		key, err := tradeNumber()
		if err != nil {
			return o, err
		}
		in.RequestID = key
	}
	if !validOperation(in.RequestID) {
		return o, ErrInvalid
	}
	normal := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var user int64
		if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, in.UserID).Scan(&user); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		found, err := s.resumePriorPackageCheckoutTx(ctx, tx, in, &o)
		if found || err != nil {
			return err
		}
		o, normal, err = s.createPackageCheckoutOrderTx(ctx, tx, in)
		return err
	})
	if err != nil {
		return o, err
	}
	if normal {
		in.PurchaseAction = ""
		return s.Create(ctx, in)
	}
	return s.resumePackageCheckout(ctx, o.ID)
}

// resumePriorPackageCheckoutTx reuses an existing package checkout for this
// request key, verifying it still matches the requested parameters.
func (s *Service) resumePriorPackageCheckoutTx(ctx context.Context, tx pgx.Tx, in CreateOrder, o *Order) (bool, error) {
	var priorPlan int64
	var action string
	var priorTarget int64
	var priorSuccess, priorCancel string
	err := tx.QueryRow(ctx, `SELECT order_id,action,target_subscription_id,success_url,cancel_url FROM v3_commerce.package_checkouts WHERE user_id=$1 AND request_id=$2`, in.UserID, in.RequestID).Scan(&priorPlan, &action, &priorTarget, &priorSuccess, &priorCancel)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	*o, err = scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE id=$1`, priorPlan))
	if err == nil && (o.PlanID == nil || *o.PlanID != in.PlanID || o.Provider != in.Provider || o.Selection != in.Selection || action != in.PurchaseAction || (in.TargetSubscriptionID != 0 && in.TargetSubscriptionID != priorTarget) || in.SuccessURL != priorSuccess || in.CancelURL != priorCancel) {
		return true, ErrStateConflict
	}
	if err == nil {
		err = s.MatchGroupCheckoutTx(ctx, tx, *o, in.PurchaseType, in.GroupBuyID)
	}
	return true, err
}

// createPackageCheckoutOrderTx locks the group-buy room (if any), finds the
// target subscription to renew or upgrade, and inserts the new order. The
// bool return is true when no eligible target subscription exists, meaning
// the caller should fall through to a normal (non-package) purchase.
func (s *Service) createPackageCheckoutOrderTx(ctx context.Context, tx pgx.Tx, in CreateOrder) (Order, bool, error) {
	var o Order
	p, err := scanPlan(tx.QueryRow(ctx, `SELECT `+planColumns+` FROM v3_commerce.plans WHERE id=$1 AND enabled AND NOT internal_only FOR SHARE`, in.PlanID))
	if errors.Is(err, pgx.ErrNoRows) {
		return o, false, ErrNotFound
	}
	if err != nil {
		return o, false, err
	}
	// Group settlement locks rooms before member subscriptions. Keep quotes
	// in that order too so a renewal cannot invert the settlement locks.
	groupQuote := Order{Kind: "subscription", UserID: in.UserID, PlanID: &p.ID, PurchaseType: in.PurchaseType,
		GroupBuyEnabled: p.GroupBuyEnabled, DurationUnit: p.DurationUnit, DurationValue: p.DurationValue}
	if err = s.PrepareGroupCheckoutTx(ctx, tx, &groupQuote, in.GroupBuyID); err != nil {
		return o, false, err
	}
	target, account, err := s.findPackageRenewalTargetTx(ctx, tx, p, in)
	if err != nil {
		return o, false, err
	}
	if target == 0 {
		if in.PurchaseAction != "auto" || in.TargetSubscriptionID != 0 {
			return o, false, ErrNotFound
		}
		return o, true, nil
	}
	if err = s.checkPackagePending(ctx, tx, target); err != nil {
		return o, false, err
	}
	var pending bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.package_checkouts WHERE target_subscription_id=$1 AND state IN ('preparing','checkout'))`, target).Scan(&pending); err != nil {
		return o, false, err
	}
	if pending {
		return o, false, ErrStateConflict
	}
	trade, err := tradeNumber()
	if err != nil {
		return o, false, err
	}
	o = Order{Selection: in.Selection, PurchaseType: in.PurchaseType, UserID: in.UserID, Provider: in.Provider, Kind: "subscription", PlanID: &p.ID, AmountMinor: p.PriceMinor, Credits: p.Credits, Currency: p.Currency, PeriodSeconds: p.PeriodSeconds,
		GroupBuyEnabled: p.GroupBuyEnabled, GroupBuyTarget: p.GroupBuyTarget, GroupBuyBonus: p.GroupBuyBonus, GroupBuyLifetimeSeconds: p.GroupBuyLifetimeSeconds,
		PeriodCredits: p.PeriodCredits, ResetPeriod: p.ResetPeriod, ResetCustomSeconds: p.ResetCustomSeconds, LegacyPeriodic: p.PeriodCredits == 0 && p.ResetPeriod != "never",
		DurationUnit: p.DurationUnit, DurationValue: p.DurationValue, CustomSeconds: p.CustomSeconds, TradeNo: trade, CreatedAt: s.cfg.Now(), ExpiresAt: s.cfg.Now().Add(s.cfg.OrderTTL)}
	o.GroupBuyBonus2, o.GroupBuyBonus3, o.GroupBuyBonus5 = p.GroupBuyBonus2, p.GroupBuyBonus3, p.GroupBuyBonus5
	o.TargetSubscriptionID = target
	o, err = s.insertOrderTx(ctx, tx, o, in.GroupBuyID, true)
	if err != nil {
		return o, false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_commerce.orders SET fulfillment_state='pending' WHERE id=$1`, o.ID); err != nil {
		return o, false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.package_checkouts(order_id,user_id,request_id,target_subscription_id,source_account_id,action,success_url,cancel_url)
	 VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, o.ID, in.UserID, in.RequestID, target, account, in.PurchaseAction, in.SuccessURL, in.CancelURL)
	return o, false, err
}

// findPackageRenewalTargetTx locks and returns the subscription a renewal or
// upgrade should apply to. Day passes may coexist, matching the source
// package rules; other plans choose the highest-price active package, then
// credits and newest expiry.
func (s *Service) findPackageRenewalTargetTx(ctx context.Context, tx pgx.Tx, p Plan, in CreateOrder) (int64, int64, error) {
	if p.DurationUnit == "day" && p.DurationValue <= 2 {
		return 0, 0, nil
	}
	var target, account int64
	err := tx.QueryRow(ctx, `SELECT s.id,s.account_id FROM v3_commerce.subscriptions s JOIN v3_commerce.plans p ON p.id=s.plan_id
	 WHERE s.user_id=$1 AND s.state='active' AND s.starts_at<=$2 AND s.expires_at>$2 AND s.deleted_at IS NULL
	 AND ($3::bigint=0 OR s.id=$3) AND NOT(p.duration_unit='day' AND p.duration_value<=2)
	 ORDER BY p.price_minor DESC,p.credits DESC,p.period_credits DESC,p.id DESC,s.expires_at DESC,s.id DESC LIMIT 1 FOR UPDATE OF s`, in.UserID, s.cfg.Now(), in.TargetSubscriptionID).Scan(&target, &account)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, err
	}
	return target, account, nil
}

func (s *Service) resumePackageCheckout(ctx context.Context, orderID int64) (result Order, resultErr error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Release()
	lock := "commerce:checkout:" + strconv.FormatInt(orderID, 10)
	var acquired bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, lock).Scan(&acquired); err != nil {
		return result, err
	}
	if !acquired {
		return result, ErrFundingPending
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, lock); err != nil {
			resultErr = errors.Join(resultErr, err)
			_ = conn.Hijack().Close(unlockCtx)
		}
	}()
	o, success, cancel, err := s.quotePackageCheckout(ctx, orderID)
	if err != nil {
		return o, err
	}
	if o.State != "created" || o.PaymentURL != "" {
		return o, nil
	}
	provider := s.providers[o.Provider]
	if provider == nil {
		return o, ErrProviderUnavailable
	}
	checkout, err := s.checkout(ctx, o, success, cancel)
	if err == nil && (checkout.Reference == "" || !validCheckoutURL(checkout.URL)) {
		err = ErrProviderUnavailable
	}
	if err != nil {
		saveErr := s.failCheckout(ctx, o)
		restoreErr := s.RestorePackageCheckout(ctx, o.ID)
		return o, errors.Join(err, saveErr, restoreErr)
	}
	_, err = s.pool.Exec(ctx, `UPDATE v3_commerce.orders SET provider_reference=$2,payment_url=$3 WHERE id=$1 AND state='created'`, o.ID, checkout.Reference, checkout.URL)
	if err != nil {
		return o, err
	}
	return s.GetOrder(ctx, o.UserID, o.TradeNo)
}
