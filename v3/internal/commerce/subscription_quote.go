package commerce

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func roundedRatio(amount, num, den int64) (int64, error) {
	if amount < 0 || num < 0 || den <= 0 {
		return 0, ErrInvalid
	}
	return roundPositiveRat(new(big.Rat).SetFrac(new(big.Int).Mul(big.NewInt(amount), big.NewInt(num)), big.NewInt(den)))
}
func roundPositiveRat(value *big.Rat) (int64, error) {
	if value.Sign() < 0 {
		return 0, ErrInvalid
	}
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(value.Num(), value.Denom(), r)
	if r.Lsh(r, 1).Cmp(value.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() {
		return 0, credits.ErrOverflow
	}
	return q.Int64(), nil
}

func (s *Service) quotePackageCheckout(ctx context.Context, id int64) (Order, string, string, error) {
	var o Order
	var success, cancel string
	var terminal error
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		o, err = scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE id=$1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		var target, account int64
		var state, requested string
		if err = tx.QueryRow(ctx, `SELECT target_subscription_id,source_account_id,state,action,success_url,cancel_url
		 FROM v3_commerce.package_checkouts WHERE order_id=$1 FOR UPDATE`, id).Scan(&target, &account, &state, &requested, &success, &cancel); err != nil {
			return err
		}
		if state != "preparing" || o.State != "created" {
			return nil
		}
		if !o.ExpiresAt.After(s.cfg.Now()) {
			terminal = ErrStateConflict
			_, err = tx.Exec(ctx, `UPDATE v3_commerce.orders SET state='expired' WHERE id=$1 AND state='created'`, id)
			return err
		}
		price, resolved, used, remaining, preserve, quoteTerminal, err := s.computePackageQuoteTx(ctx, tx, o, target, account, requested)
		if err != nil {
			return err
		}
		if quoteTerminal != nil {
			terminal = quoteTerminal
			_, err = tx.Exec(ctx, `UPDATE v3_commerce.orders SET state='failed' WHERE id=$1 AND state='created'`, id)
			return err
		}
		bonus, err := s.starterPurchaseBonus(ctx, tx, o)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE v3_commerce.orders SET amount_minor=$2 WHERE id=$1`, id, price); err != nil {
			return err
		}
		o.AmountMinor = price
		if err = s.ApplyCheckoutDiscountTx(ctx, tx, &o); err != nil {
			return err
		}
		if err = s.preparePaidOrderTx(ctx, tx, &o); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE v3_commerce.package_checkouts SET state='checkout',resolved_action=$2,quoted_used=$3,quoted_remaining=$4,preserve_remaining=$5,bonus_credits=$6 WHERE order_id=$1`, id, resolved, int64(used), int64(remaining), preserve, int64(bonus))
		return err
	})
	if err != nil {
		return o, success, cancel, err
	}
	if terminal != nil {
		return o, success, cancel, errors.Join(terminal, s.RestorePackageCheckout(ctx, id))
	}
	return o, success, cancel, nil
}

// computePackageQuoteTx tallies usage against the target subscription,
// validates it still matches what was quoted, and prices the resolved
// renew/upgrade action. A non-nil terminal return means the quote cannot
// proceed and the caller must fail the order; err signals an infra error
// that must abort the transaction outright.
func (s *Service) computePackageQuoteTx(ctx context.Context, tx pgx.Tx, o Order, target, account int64, requested string) (price int64, resolved string, used, remaining credits.Micro, preserve bool, terminal, err error) {
	var total credits.Micro
	var p Plan
	var resetUsed bool
	used, total, p, resolved, terminal, err = s.loadPackageQuoteUsageTx(ctx, tx, o, target, account, requested)
	if err != nil {
		return
	}
	remaining = max(total-used, 0)
	if err = tx.QueryRow(ctx, `SELECT reset_opportunity_used FROM v3_commerce.subscriptions WHERE id=$1`, target).Scan(&resetUsed); err != nil {
		return
	}
	price = o.AmountMinor
	preserve = resetUsed && resolved == "upgrade" && remaining > 0
	if terminal == nil && resolved == "renew" && total > 0 && !resetUsed {
		if new(big.Int).Mul(big.NewInt(int64(used)), big.NewInt(10)).Cmp(new(big.Int).Mul(big.NewInt(int64(total)), big.NewInt(3))) < 0 {
			terminal = ErrStateConflict
		} else if price, err = roundedRatio(o.AmountMinor, min(int64(used), int64(total)), int64(total)); err != nil {
			return
		}
	}
	if terminal == nil && resolved == "upgrade" && total > 0 && !resetUsed {
		discount := new(big.Rat).SetFrac(new(big.Int).Mul(big.NewInt(p.PriceMinor), big.NewInt(int64(remaining))), big.NewInt(int64(total)))
		value := new(big.Rat).Sub(new(big.Rat).SetInt64(o.AmountMinor), discount)
		if value.Sign() <= 0 {
			price = max(paymentScale(o.Currency)/100, 1)
		} else {
			if price, err = roundPositiveRat(value); err != nil {
				return
			}
			price = max(price, max(paymentScale(o.Currency)/100, 1))
		}
	}
	return
}

// loadPackageQuoteUsageTx loads the target subscription's plan and tallied
// usage, resolves whether this is a renew or upgrade, and validates the
// subscription still matches what was quoted (funding drained, right
// account/currency/action). A non-nil terminal means validation failed.
func (s *Service) loadPackageQuoteUsageTx(ctx context.Context, tx pgx.Tx, o Order, target, account int64, requested string) (used, total credits.Micro, p Plan, resolved string, terminal, err error) {
	var actualAccount, planID int64
	var subState string
	if err = tx.QueryRow(ctx, `SELECT account_id,plan_id,total_credits,used_credits,state FROM v3_commerce.subscriptions WHERE id=$1 AND user_id=$2 FOR UPDATE`, target, o.UserID).Scan(&actualAccount, &planID, &total, &used, &subState); err != nil {
		return
	}
	if s.cfg.FundingDrain != nil {
		var drained bool
		if drained, err = s.cfg.FundingDrain.FreezeAndDrained(ctx, tx, account); err != nil {
			return
		}
		if !drained {
			err = ErrFundingPending
			return
		}
	}
	var spent credits.Micro
	if err = tx.QueryRow(ctx, `SELECT COALESCE(GREATEST(-SUM(amount),0),0)::bigint FROM v3_billing.ledger_entries WHERE account_id=$1 AND kind IN ('usage','refund')`, account).Scan(&spent); err != nil {
		return
	}
	if used, err = used.Add(spent); err != nil {
		return
	}
	p, err = subscriptionPlan(ctx, tx, target)
	if err != nil {
		return
	}
	remaining := max(total-used, 0)
	resolved = "renew"
	comparison := comparePackage(o, p)
	if comparison > 0 {
		resolved = "upgrade"
	}
	if actualAccount != account || subState != "active" || p.Currency != o.Currency || (comparison < 0 && (remaining > 0 || total == 0)) || (requested != "auto" && requested != resolved) {
		terminal = ErrStateConflict
	}
	return
}

func comparePackage(o Order, p Plan) int {
	left := []int64{o.AmountMinor, int64(o.Credits), int64(o.PeriodCredits), *o.PlanID}
	right := []int64{p.PriceMinor, int64(p.Credits), int64(p.PeriodCredits), p.ID}
	for i := range left {
		if left[i] < right[i] {
			return -1
		}
		if left[i] > right[i] {
			return 1
		}
	}
	return 0
}

func (s *Service) starterPurchaseBonus(ctx context.Context, tx pgx.Tx, o Order) (credits.Micro, error) {
	var name, planType string
	var eligible bool
	err := tx.QueryRow(ctx, `SELECT name,COALESCE(to_jsonb(p)->>'plan_type','') FROM v3_commerce.plans p WHERE id=$1`, *o.PlanID).Scan(&name, &planType)
	if err != nil {
		return 0, err
	}
	if planType != "monthly" && o.DurationUnit != "month" {
		return 0, nil
	}
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.subscriptions s JOIN v3_commerce.plans p ON p.id=s.plan_id
	 WHERE s.user_id=$1 AND COALESCE(to_jsonb(p)->>'plan_type','')='starter' AND s.created_at>=$2)`, o.UserID, s.cfg.Now().Add(-72*time.Hour)).Scan(&eligible)
	if err != nil || !eligible {
		return 0, err
	}
	name = strings.ToLower(name)
	for _, rule := range []struct {
		name   string
		amount credits.Micro
	}{{"ultra", 100}, {"pro", 60}, {"standard", 30}, {"lite", 10}} {
		if strings.Contains(name, rule.name) {
			return rule.amount * credits.Micro(credits.PerCredit), nil
		}
	}
	return 0, nil
}
