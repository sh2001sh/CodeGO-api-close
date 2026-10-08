//go:build pgintegration

package marketplace_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

func TestFrozenBatchPropActivationAndGiftRestrictionsPreserveLegacy(t *testing.T) {
	ctx := context.Background()
	pool, poster, accounts, cfg := marketplace.DependenciesForIntegrationTest(t)
	cs := commerce.New(pool, poster, nil, commerce.Config{Now: cfg.Now})
	ms := marketplace.New(pool, poster, accounts, cs, cs, cfg)
	p, err := cs.SavePlan(ctx, commerce.Plan{Name: "Published fixed reward", PolicyVersion: commerce.PolicyStandardV2,
		PriceMinor: 100, Currency: "usd", Credits: 1000, DurationUnit: "day", DurationValue: 7, Enabled: true,
		ModelLimits: map[string]int64{"original-model": 10}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ms.SaveBatch(ctx, 3, marketplace.Batch{Name: "Retained subscription batch", Purpose: "credits", Price: 100, BaseCredits: 100,
		Budget: 3000, CostsConfirmed: true, ContributionSharePPM: 10000,
		Rewards: []marketplace.BatchReward{{ID: "fixed", Title: "Fixed package", Kind: "subscription", PlanID: p.ID, Quantity: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	b, err = ms.ChangeBatchState(ctx, 3, b.ID, b.Revision, "publish-retained", true)
	if err != nil {
		t.Fatal(err)
	}
	drawn, err := ms.DrawBatch(ctx, 1, b.ID, "draw-retained", 1)
	if err != nil || len(drawn.Records) != 1 || drawn.Records[0].PropID <= 0 {
		t.Fatalf("frozen batch draw: %+v %v", drawn, err)
	}
	prop := drawn.Records[0].PropID
	if _, err = ms.ChangeBatchState(ctx, 3, b.ID, b.Revision, "stop-retained", false); err != nil {
		t.Fatal(err)
	}
	p.Enabled, p.Name, p.Credits, p.DurationValue = false, "Edited", 1, 1
	p.ModelLimits = map[string]int64{"changed-model": 1}
	if _, err = cs.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	view, err := ms.Overview(ctx, 1)
	if err != nil || len(view.Props) != 1 || len(view.Props[0].PlanSnapshot) == 0 {
		t.Fatalf("prop overview lost frozen specification: %+v %v", view, err)
	}
	if err = ms.GiftProp(ctx, 1, 2, prop, "forbidden-frozen-gift"); !errors.Is(err, marketplace.ErrConflict) {
		t.Fatalf("frozen reward gifted: %v", err)
	}
	if _, err = ms.UseProp(ctx, 2, prop); !errors.Is(err, marketplace.ErrNotFound) {
		t.Fatalf("foreign activation: %v", err)
	}
	for range 2 {
		used, err := ms.UseProp(ctx, 1, prop)
		if err != nil || used.Status != "used" || len(used.PlanSnapshot) == 0 {
			t.Fatalf("stopped frozen reward activation: %+v %v", used, err)
		}
	}
	subs, err := cs.ListSubscriptions(ctx, 1)
	if err != nil || len(subs) != 1 || subs[0].Balance != 1000 || subs[0].ExpiresAt.Sub(cfg.Now()) != 7*24*time.Hour || subs[0].PlanSnapshot.Name != "Published fixed reward" || subs[0].PlanSnapshot.ModelLimits["original-model"] != 10 {
		t.Fatalf("frozen promise not fulfilled once: %+v %v", subs, err)
	}
	for _, entry := range []billing.Entry{
		{AccountID: subs[0].AccountID, Amount: -100, Kind: "usage", RequestID: "frozen-stats", OperationID: "frozen-stats-debit"},
		{AccountID: subs[0].AccountID, Amount: 20, Kind: "refund", RequestID: "frozen-stats", OperationID: "frozen-stats-refund"},
	} {
		if _, err := poster.Post(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := ms.BatchStats(ctx, b.ID)
	if err != nil || stats.APIUsed != 80 || stats.SubscriptionAwardedCount != 1 || stats.SubscriptionActivatedCount != 1 {
		t.Fatalf("frozen subscription actual use/refund not reported: %+v %v", stats, err)
	}
	history, err := ms.History(ctx, 1, 0, 30)
	if err != nil || len(history) != 1 || history[0].BatchID != b.ID || history[0].ItemID != 0 || history[0].PropID != prop {
		t.Fatalf("batch history identity lost: %+v %v", history, err)
	}
	legacy, err := cs.SavePlan(ctx, commerce.Plan{Name: "Legacy gift", PriceMinor: 100, Currency: "usd", Credits: 500, PeriodSeconds: 86400, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var legacyProp int64
	if err = pool.QueryRow(ctx, `INSERT INTO v3_marketplace.blind_box_props(user_id,kind,title,plan_id) VALUES(1,'subscription','Legacy gift',$1) RETURNING id`, legacy.ID).Scan(&legacyProp); err != nil {
		t.Fatal(err)
	}
	if err = ms.GiftProp(ctx, 1, 2, legacyProp, "allowed-legacy-gift"); err != nil {
		t.Fatalf("legacy gift rejected: %v", err)
	}
	view, err = ms.Overview(ctx, 2)
	if err != nil || len(view.Props) != 1 || len(view.Props[0].PlanSnapshot) != 0 {
		t.Fatalf("empty legacy snapshot not omitted: %+v %v", view, err)
	}
	wire, err := json.Marshal(view.Props[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(wire, &fields); err != nil || fields["plan_snapshot"] != nil {
		t.Fatalf("empty legacy snapshot exposed: %s %v", wire, err)
	}
	if _, err = ms.UseProp(ctx, 2, legacyProp); err != nil {
		t.Fatalf("legacy activation changed: %v", err)
	}
	subs, err = cs.ListSubscriptions(ctx, 2)
	if err != nil || len(subs) != 1 || subs[0].Balance != 500 || subs[0].PolicyVersion != commerce.PolicyLegacy {
		t.Fatalf("legacy gifted promise changed: %+v %v", subs, err)
	}
}
