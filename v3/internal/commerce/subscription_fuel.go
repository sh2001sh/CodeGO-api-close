package commerce

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type FuelQuote struct {
	SubscriptionID int64         `json:"subscription_id"`
	PlanID         int64         `json:"plan_id"`
	Credits        credits.Micro `json:"credits"`
	AmountMinor    int64         `json:"amount_minor"`
	Currency       string        `json:"currency"`
	ExpiresAt      time.Time     `json:"expires_at"`
	MinCredits     credits.Micro `json:"min_credits"`
	CreditStep     credits.Micro `json:"credit_step"`
}

func quoteFuel(p Plan, id int64, amount credits.Micro, end time.Time) (FuelQuote, error) {
	q := FuelQuote{SubscriptionID: id, PlanID: p.ID, Credits: amount, Currency: p.Currency, ExpiresAt: end, MinCredits: p.FuelMinCredits, CreditStep: p.FuelCreditStep}
	if !p.FuelEnabled || (p.PlanType != "monthly" && p.DurationUnit != "month") || p.FuelUnitPriceMicro <= 0 || p.FuelMinCredits <= 0 || p.FuelCreditStep <= 0 || amount < p.FuelMinCredits || amount%p.FuelCreditStep != 0 {
		return q, ErrInvalid
	}
	numerator := new(big.Int).Mul(big.NewInt(int64(amount)), big.NewInt(p.FuelUnitPriceMicro))
	numerator.Mul(numerator, big.NewInt(paymentScale(p.Currency)))
	denominator := new(big.Int).Mul(big.NewInt(credits.PerCredit), big.NewInt(1000000))
	value, err := roundPositiveRat(new(big.Rat).SetFrac(numerator, denominator))
	if err != nil {
		return q, err
	}
	if value <= 0 {
		return q, ErrInvalid
	}
	q.AmountMinor = value
	return q, nil
}

func (s *Service) QuoteSubscriptionFuel(ctx context.Context, user, id int64, amount credits.Micro) (FuelQuote, error) {
	var plan int64
	var end time.Time
	err := s.pool.QueryRow(ctx, `SELECT plan_id,expires_at FROM v3_commerce.subscriptions WHERE id=$1 AND user_id=$2 AND state='active' AND expires_at>$3 AND deleted_at IS NULL`, id, user, s.cfg.Now()).Scan(&plan, &end)
	if errors.Is(err, pgx.ErrNoRows) {
		return FuelQuote{}, ErrNotFound
	}
	if err != nil {
		return FuelQuote{}, err
	}
	p, err := scanPlan(s.pool.QueryRow(ctx, `SELECT `+planColumns+` FROM v3_commerce.plans WHERE id=$1`, plan))
	if err != nil {
		return FuelQuote{}, err
	}
	return quoteFuel(p, id, amount, end)
}

func (s *Service) CreateSubscriptionFuel(ctx context.Context, in CreateOrder) (Order, error) {
	var o Order
	provider := s.providers[in.Provider]
	if provider == nil {
		return o, ErrProviderUnavailable
	}
	if in.UserID <= 0 || in.TargetSubscriptionID <= 0 || in.FuelCredits <= 0 || in.PurchaseAction != "" || in.ProductID != "" || !in.Selection.validFor(in.Provider) || !s.allowedReturn(in.SuccessURL) || !s.allowedReturn(in.CancelURL) {
		return o, ErrInvalid
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var user, plan int64
		var end time.Time
		if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, in.UserID).Scan(&user); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT plan_id,expires_at FROM v3_commerce.subscriptions WHERE id=$1 AND user_id=$2 AND state='active' AND expires_at>$3 AND deleted_at IS NULL FOR UPDATE`, in.TargetSubscriptionID, in.UserID, s.cfg.Now()).Scan(&plan, &end); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if err := s.checkPackagePending(ctx, tx, in.TargetSubscriptionID); err != nil {
			return err
		}
		p, err := scanPlan(tx.QueryRow(ctx, `SELECT `+planColumns+` FROM v3_commerce.plans WHERE id=$1 FOR SHARE`, plan))
		if err != nil {
			return err
		}
		q, err := quoteFuel(p, in.TargetSubscriptionID, in.FuelCredits, end)
		if err != nil {
			return err
		}
		trade, err := tradeNumber()
		if err != nil {
			return err
		}
		o = Order{Selection: in.Selection, UserID: in.UserID, PlanID: &p.ID, Provider: in.Provider, Kind: "subscription", PurchaseType: "fuel", TargetSubscriptionID: in.TargetSubscriptionID, FuelExpiresAt: &end, Credits: q.Credits, AmountMinor: q.AmountMinor, Currency: q.Currency, PeriodSeconds: p.PeriodSeconds, ResetPeriod: "never", GroupBuyTarget: 3, GroupBuyLifetimeSeconds: 86400, TradeNo: trade, CreatedAt: s.cfg.Now(), ExpiresAt: s.cfg.Now().Add(s.cfg.OrderTTL)}
		o, err = s.insertOrderTx(ctx, tx, o, 0, true)
		return err
	})
	if err != nil {
		return o, err
	}
	checkout, err := s.checkout(ctx, o, in.SuccessURL, in.CancelURL)
	if err == nil && (checkout.Reference == "" || !validCheckoutURL(checkout.URL)) {
		err = ErrProviderUnavailable
	}
	if err != nil {
		return o, errors.Join(err, s.failCheckout(ctx, o))
	}
	_, err = s.pool.Exec(ctx, `UPDATE v3_commerce.orders SET provider_reference=$2,payment_url=$3 WHERE id=$1`, o.ID, checkout.Reference, checkout.URL)
	if err != nil {
		return o, err
	}
	return s.GetOrder(ctx, in.UserID, o.TradeNo)
}

func (s *Service) applySubscriptionFuelTx(ctx context.Context, tx pgx.Tx, o Order) error {
	if o.TargetSubscriptionID <= 0 || o.FuelExpiresAt == nil || o.Credits <= 0 {
		return ErrInvalid
	}
	var account int64
	var total, used, period, periodUsed, balance, spent credits.Micro
	var end time.Time
	var state string
	err := tx.QueryRow(ctx, `SELECT account_id,total_credits,used_credits,period_credits,period_used,expires_at,state FROM v3_commerce.subscriptions WHERE id=$1 AND user_id=$2 FOR UPDATE`, o.TargetSubscriptionID, o.UserID).Scan(&account, &total, &used, &period, &periodUsed, &end, &state)
	if err != nil {
		return err
	}
	if state != "active" || !end.After(s.cfg.Now()) || !end.Equal(*o.FuelExpiresAt) {
		return s.recordPackagePaymentReview(ctx, tx, o, "fuel target expired or changed before payment confirmation")
	}
	if err = s.checkPackagePending(ctx, tx, o.TargetSubscriptionID); err != nil {
		return err
	}
	// Current period caps still apply; fuel enlarges lifetime, never renewable quota.
	if err = tx.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE id=$1 FOR UPDATE`, account).Scan(&balance); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT COALESCE(GREATEST(-SUM(amount),0),0)::bigint FROM v3_billing.ledger_entries WHERE account_id=$1 AND kind IN ('usage','refund')`, account).Scan(&spent); err != nil {
		return err
	}
	total, err = total.Add(o.Credits)
	if err != nil {
		return err
	}
	grant := o.Credits
	if period > 0 {
		remaining := max(period-periodUsed-spent, 0)
		grant = min(grant, max(remaining-balance, 0))
	}
	if grant > 0 {
		if _, err = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: grant, Kind: "subscription_grant", OperationID: "subscription:fuel:" + o.TradeNo, Metadata: map[string]any{"order_id": o.ID, "subscription_id": o.TargetSubscriptionID}}); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET total_credits=$2 WHERE id=$1`, o.TargetSubscriptionID, int64(total))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_fuel_fulfillments(order_id,subscription_id,credits) VALUES($1,$2,$3)`, o.ID, o.TargetSubscriptionID, int64(o.Credits))
	return err
}
