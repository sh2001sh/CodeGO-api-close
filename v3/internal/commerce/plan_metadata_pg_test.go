//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestPlanMetadataPaidGrantAndRenewalFreezeModelLimitsByCycle(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := packagePlan(t, s, 1000, 1000)
	p.ModelLimits = map[string]int64{" chat ": 400, "unlimited": 0}
	var err error
	p, err = s.SavePlan(ctx, p)
	if err != nil || len(p.ModelLimits) != 1 || p.ModelLimits["chat"] != 400 {
		t.Fatalf("source limit normalization=%+v err=%v", p, err)
	}
	packageCallback(t, s, create(t, s, p.ID))
	sub := onlySubscription(t, s)
	var limit int64
	var source string
	if err = pool.QueryRow(ctx, `SELECT (model_limits->>'chat')::bigint,source FROM v3_commerce.subscriptions WHERE id=$1`, sub.ID).Scan(&limit, &source); err != nil || limit != 400 || source != "order" {
		t.Fatalf("paid package lost limits/provenance: %d %s %v", limit, source, err)
	}
	p.ModelLimits["chat"] = 600
	if _, err = s.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT (model_limits->>'chat')::bigint FROM v3_commerce.subscriptions WHERE id=$1`, sub.ID).Scan(&limit); err != nil || limit != 400 {
		t.Fatalf("plan edit changed existing cycle: %d %v", limit, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET model_usage='{"chat":300}'::jsonb WHERE id=$1`, sub.ID); err != nil {
		t.Fatal(err)
	}
	spendPackage(t, pool, sub.AccountID, 400, "model-cycle-renewal:usage")
	o, err := s.Create(ctx, packageRequest(p.ID, sub.ID, "renew", "model-cycle-renewal"))
	if err != nil {
		t.Fatal(err)
	}
	packageCallback(t, s, o)
	var cleared bool
	if err = pool.QueryRow(ctx, `SELECT (model_limits->>'chat')::bigint,model_usage='{}'::jsonb FROM v3_commerce.subscriptions WHERE id=$1`, sub.ID).Scan(&limit, &cleared); err != nil || limit != 600 || !cleared {
		t.Fatalf("renewed cycle limit=%d usage cleared=%v err=%v", limit, cleared, err)
	}
	p.ModelLimits = map[string]int64{"chat": -1}
	if _, err = s.SavePlan(ctx, p); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("negative monetary cap accepted: %v", err)
	}
	p.ModelLimits = nil
	p.UpgradeGroup = "missing-paid-group"
	if _, err = s.SavePlan(ctx, p); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("unknown paid authorization group accepted: %v", err)
	}
}

func TestPlanMetadataRenewalRetainsHistoricalResetMoneyGuard(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := packagePlan(t, s, 1000, 1000)
	packageCallback(t, s, create(t, s, p.ID))
	sub := onlySubscription(t, s)
	if err := s.ResetSubscription(ctx, sub.ID, 2, "historical-reset"); err != nil {
		t.Fatal(err)
	}
	first, err := s.Create(ctx, packageRequest(p.ID, sub.ID, "renew", "reset-first-cycle"))
	if err != nil || first.AmountMinor != p.PriceMinor {
		t.Fatalf("reset history did not require full price: %+v %v", first, err)
	}
	packageCallback(t, s, first)
	var retained bool
	if err = pool.QueryRow(ctx, `SELECT reset_opportunity_used FROM v3_commerce.subscriptions WHERE id=$1`, sub.ID).Scan(&retained); err != nil || !retained {
		t.Fatalf("renewal erased lifetime reset history: %v %v", retained, err)
	}
	// The source tests for any historical use row, irrespective of the cycle.
	// A second renewal therefore retains the full price and is not rejected by
	// the discounted renewal's minimum-consumption requirement.
	second, err := s.Create(ctx, packageRequest(p.ID, sub.ID, "renew", "reset-second-cycle"))
	if err != nil || second.AmountMinor != p.PriceMinor {
		t.Fatalf("renewed source history changed money eligibility: %+v %v", second, err)
	}
}
