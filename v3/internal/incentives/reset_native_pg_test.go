//go:build pgintegration

package incentives

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestOpportunityUsesRealCommerceRotationAndKeepsConsumedFuel(t *testing.T) {
	s, pool, now := fixture(t)
	ctx := context.Background()
	poster := ledger.NewPoster(pool)
	shop := commerce.New(pool, poster, nil, commerce.Config{Now: func() time.Time { return *now }})
	id, err := shop.BindSubscription(ctx, 1, 1, "native-opportunity")
	if err != nil {
		t.Fatal(err)
	}
	s.cfg.ResetSubscriptionTx = shop.ResetRewardSubscriptionTx
	var account int64
	if err = pool.QueryRow(ctx, `SELECT account_id FROM v3_commerce.subscriptions WHERE id=$1`, id).Scan(&account); err != nil {
		t.Fatal(err)
	}
	if _, err = poster.Post(ctx, billing.Entry{AccountID: account, Amount: 1000000, Kind: "subscription_grant", OperationID: "fuel-extra"}); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET total_credits=2000000,model_usage='{"paid-model":123}'::jsonb WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = poster.Post(ctx, billing.Entry{AccountID: account, Amount: -1500000, Kind: "usage", OperationID: "consumed-fuel", RequestID: "consumed-fuel"}); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_commerce.subscription_reset_opportunity_accounts(user_id,earned_total,available_total) VALUES(1,1,1)`); err != nil {
		t.Fatal(err)
	}
	result, err := s.UseReset(ctx, 1)
	if err != nil || result.UsedBefore != 1500000 || result.UsedAfter != 500000 || result.ClearedUsed != 1000000 || result.ResetOpportunity.AvailableCount != 0 {
		t.Fatalf("reset=%+v err=%v", result, err)
	}
	var balance int64
	var rotated, modelCleared bool
	if err = pool.QueryRow(ctx, `SELECT a.balance,s.model_usage='{}'::jsonb,s.account_id<>$2 FROM v3_commerce.subscriptions s JOIN v3_billing.accounts a ON a.id=s.account_id WHERE s.id=$1`, id, account).Scan(&balance, &modelCleared, &rotated); err != nil {
		t.Fatal(err)
	}
	if balance != 1500000 || !rotated || !modelCleared {
		t.Fatalf("balance=%d rotated=%v model-cleared=%v", balance, rotated, modelCleared)
	}
}
func TestConvertedCurrentCycleIsIneligibleButPriorCycleIsEligible(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	seedDraw(t, s, 0)
	if _, err := pool.Exec(ctx, `INSERT INTO v3_commerce.subscription_reset_opportunity_accounts(user_id,earned_total,available_total) VALUES(1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_commerce.subscription_value_conversions(user_id,subscription_id,request_id,status,source_credits,target_credits,plan_price_amount,unused_ratio,conversion_percent,ratio_numerator,ratio_denominator,created_at)
 VALUES(1,1,'imported-current-conversion','completed',100,100,1,1,10,1,1,$1)`, s.now()); err != nil {
		t.Fatal(err)
	}
	s.cfg.ResetSubscriptionTx = func(context.Context, pgx.Tx, int64, string) error { return nil }
	if _, err := s.UseReset(ctx, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("converted cycle reset accepted %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.subscription_value_conversions SET created_at=$1`, s.now().Add(-time.Hour*2)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UseReset(ctx, 1); err != nil {
		t.Fatalf("old cycle conversion blocked reset: %v", err)
	}
}
