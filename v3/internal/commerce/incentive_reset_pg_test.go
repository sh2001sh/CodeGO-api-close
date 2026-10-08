//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/incentives"
)

func TestEarnedResetRotatesActualFundingAndRejectsMonthlyReplay(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	p := packagePlan(t, s, 100, 1000000)
	packageCallback(t, s, create(t, s, p.ID))
	before := onlySubscription(t, s)
	spendPackage(t, pool, before.AccountID, 400000, "earned-reset:usage")
	if _, err := pool.Exec(ctx, `INSERT INTO v3_commerce.subscription_reset_opportunity_accounts(user_id,available_total,earned_total) VALUES(1,2,2)`); err != nil {
		t.Fatal(err)
	}
	i := incentives.New(pool, ledger.NewPoster(pool), incentives.Config{Now: func() time.Time { return *now }, ResetSubscriptionTx: s.ResetRewardSubscriptionTx})
	result, err := i.UseReset(ctx, 1)
	if err != nil || result.SubscriptionID != before.ID || result.UsedBefore != 400000 || result.UsedAfter != 0 || result.PeriodUsedAfter != 0 || result.ClearedUsed != 400000 || result.ResetOpportunity.AvailableCount != 1 || result.ResetOpportunity.UsedTotal != 1 || !result.ResetOpportunity.UsedThisMonth {
		t.Fatalf("earned actual reset: %+v err=%v", result, err)
	}
	after := onlySubscription(t, s)
	if after.AccountID == before.AccountID || after.Balance != 1000000 || !after.ExpiresAt.Equal(before.ExpiresAt) || after.UsedCredits != 0 {
		t.Fatalf("earned reset did not rotate funding with original expiry: before=%+v after=%+v", before, after)
	}
	var oldBalance, usedEvents int64
	if err := pool.QueryRow(ctx, `SELECT balance,(SELECT count(*) FROM v3_commerce.subscription_reset_opportunity_ledgers WHERE user_id=1 AND change_type='use') FROM v3_billing.accounts WHERE id=$1`, before.AccountID).Scan(&oldBalance, &usedEvents); err != nil || oldBalance != 0 || usedEvents != 1 {
		t.Fatalf("old funding or opportunity duplicated: balance=%d events=%d err=%v", oldBalance, usedEvents, err)
	}
	if _, err := i.UseReset(ctx, 1); !errors.Is(err, incentives.ErrMonthlyUsed) {
		t.Fatalf("same Shanghai month reset replay=%v", err)
	}
	if final := onlySubscription(t, s); final.AccountID != after.AccountID || final.Balance != after.Balance {
		t.Fatalf("monthly rejection changed funding: before=%+v after=%+v", after, final)
	}
	if _, err := i.UseReset(ctx, 2); !errors.Is(err, incentives.ErrUnavailable) {
		t.Fatalf("other user spent owner's opportunity: %v", err)
	}
}

func TestEarnedResetDeliveryFailureDoesNotSpendOpportunity(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	p := packagePlan(t, s, 100, 1000000)
	packageCallback(t, s, create(t, s, p.ID))
	before := onlySubscription(t, s)
	if _, err := pool.Exec(ctx, `INSERT INTO v3_commerce.subscription_reset_opportunity_accounts(user_id,available_total,earned_total) VALUES(1,1,1);
 UPDATE v3_commerce.subscriptions SET state='expired' WHERE user_id=1`); err != nil {
		t.Fatal(err)
	}
	i := incentives.New(pool, ledger.NewPoster(pool), incentives.Config{Now: func() time.Time { return *now }, ResetSubscriptionTx: s.ResetRewardSubscriptionTx})
	if _, err := i.UseReset(ctx, 1); !errors.Is(err, incentives.ErrNotFound) {
		t.Fatalf("expired subscription consumed reward: %v", err)
	}
	summary, err := i.ResetOpportunities(ctx, 1)
	if err != nil || summary.AvailableCount != 1 || summary.UsedTotal != 0 || summary.UsedThisMonth {
		t.Fatalf("failed reset consumed opportunity: %+v err=%v", summary, err)
	}
	var account int64
	if err := pool.QueryRow(ctx, `SELECT account_id FROM v3_commerce.subscriptions WHERE id=$1`, before.ID).Scan(&account); err != nil || account != before.AccountID {
		t.Fatalf("failed reset changed funding: account=%d err=%v", account, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET state='active' WHERE user_id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := i.UseReset(ctx, 1); err != nil {
		t.Fatalf("valid retry could not use retained opportunity: %v", err)
	}
}
