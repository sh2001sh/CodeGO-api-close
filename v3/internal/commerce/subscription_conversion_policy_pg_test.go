//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestSubscriptionConversionCurrentSettingBlocksNewIntentsAndKeepsReplay(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := packagePlan(t, s, 1000, 1000)
	packageCallback(t, s, create(t, s, p.ID))
	sub := onlySubscription(t, s)
	for _, value := range []string{`false`, `"false"`, `"invalid"`} {
		if _, err := pool.Exec(ctx, `INSERT INTO v3_platform.settings(key,value) VALUES('SubscriptionClaudeConversionEnabled',$1)
		 ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`, value); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ConvertSubscription(ctx, 1, sub.ID, 10, "policy-denied"); !errors.Is(err, commerce.ErrInvalid) {
			t.Fatalf("setting %s admitted conversion: %v", value, err)
		}
	}
	var operations int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscription_operations`).Scan(&operations); err != nil || operations != 0 {
		t.Fatalf("denied setting created intent=%d err=%v", operations, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_platform.settings SET value='true' WHERE key='SubscriptionClaudeConversionEnabled'`); err != nil {
		t.Fatal(err)
	}
	first, err := s.ConvertSubscription(ctx, 1, sub.ID, 10, "policy-authorized")
	if err != nil || first.SourceCredits != 100 || first.TargetCredits != 1_000_000 {
		t.Fatalf("authorized conversion=%+v err=%v", first, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_platform.settings SET value='false' WHERE key='SubscriptionClaudeConversionEnabled'`); err != nil {
		t.Fatal(err)
	}
	replay, err := s.ConvertSubscription(ctx, 1, sub.ID, 10, "policy-authorized")
	if err != nil || replay != first {
		t.Fatalf("authorized replay changed=%+v err=%v", replay, err)
	}
	if _, err = s.ConvertSubscription(ctx, 1, sub.ID, 10, "policy-new-denied"); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("new disabled request admitted: %v", err)
	}
	var conversions int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscription_conversions`).Scan(&conversions); err != nil || conversions != 1 {
		t.Fatalf("conversion replay posted again count=%d err=%v", conversions, err)
	}
}
