//go:build pgintegration

package commerce_test

import (
	"context"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestGroupCheckoutOmittedRulesKeepSourceFiveMembersAndFortyEightHours(t *testing.T) {
	f := newGroupFixture(t)
	ctx := context.Background()
	p, err := f.s.SavePlan(ctx, commerce.Plan{Name: "source group defaults", PriceMinor: 1000, Currency: "cny", Credits: 1000,
		DurationUnit: "month", DurationValue: 1, Enabled: true, GroupBuyEnabled: true, GroupBuyBonus2: 100, GroupBuyBonus3: 200, GroupBuyBonus5: 300})
	if err != nil || p.GroupBuyTarget != 5 || p.GroupBuyLifetimeSeconds != 172800 {
		t.Fatalf("source rule defaults=%+v err=%v", p, err)
	}
	o, status, body := f.checkout(1, map[string]any{"plan_id": p.ID}, false)
	if status != 200 {
		t.Fatalf("checkout=%d %s", status, body)
	}
	if err = f.pay(o); err != nil {
		t.Fatal(err)
	}
	g, err := f.market.GetGroup(ctx, f.room(t, o.ID))
	if err != nil || g.TargetCount != 5 || !g.ExpiresAt.Equal(f.now.Add(48*time.Hour)) {
		t.Fatalf("actual source group=%+v err=%v", g, err)
	}
	var target int
	var lifetime int64
	if err = f.pool.QueryRow(ctx, `INSERT INTO v3_commerce.plans(name,price_minor,credits,period_seconds) VALUES('import defaults',100,100,3600)
	 RETURNING group_buy_target,group_buy_lifetime_seconds`).Scan(&target, &lifetime); err != nil || target != 5 || lifetime != 172800 {
		t.Fatalf("source import defaults=%d/%d err=%v", target, lifetime, err)
	}
}
