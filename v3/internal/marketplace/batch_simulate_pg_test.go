//go:build pgintegration

package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
)

func simulationSnapshot(t *testing.T, f *fixture) string {
	t.Helper()
	var snapshot string
	err := f.pool.QueryRow(testContext, `SELECT jsonb_build_object(
 'accounts',(SELECT jsonb_agg(a ORDER BY id) FROM v3_billing.accounts a),
 'ledger',(SELECT jsonb_agg(e ORDER BY id) FROM v3_billing.ledger_entries e),
 'funding',(SELECT jsonb_agg(l ORDER BY lot_id) FROM v3_billing.funding_lots l),
 'batches',(SELECT jsonb_agg(b ORDER BY id) FROM v3_marketplace.blind_box_batches b),
 'records',(SELECT jsonb_agg(r ORDER BY id) FROM v3_marketplace.blind_box_open_records r),
 'props',(SELECT jsonb_agg(p ORDER BY id) FROM v3_marketplace.blind_box_props p),
 'pity',(SELECT jsonb_agg(p ORDER BY user_id) FROM v3_marketplace.blind_box_batch_pity p),
 'requests',(SELECT jsonb_agg(r ORDER BY user_id,kind,request_id) FROM v3_marketplace.operations r)
 )::text`).Scan(&snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestPaidRandomSimulationUsesOfficialPityAndMakesNoAssetWrites(t *testing.T) {
	f := newFixture(t)
	b := randomBatch(t, f, 20)
	if _, err := f.pool.Exec(testContext, `INSERT INTO v3_marketplace.blind_box_batch_pity(user_id,opened,small_progress,big_progress) VALUES(2,49,9,49)`); err != nil {
		t.Fatal(err)
	}
	before := simulationSnapshot(t, f)
	sim, err := f.s.SimulateBatch(testContext, 2, b.ID, 2)
	if err != nil || sim.Charged != 0 || sim.BaseCredits != 0 || len(sim.Records) != 2 || sim.Records[0].ID != 0 || sim.Records[0].Guarantee != "big" || sim.Records[0].GuaranteeCredits != 140 || sim.Pity != (PityState{Opened: 51, SmallProgress: 1, BigProgress: 1}) {
		t.Fatalf("simulation: %+v %v", sim, err)
	}
	if simulationSnapshot(t, f) != before {
		t.Fatal("simulation changed an asset, official progress or request")
	}
	again, err := f.s.SimulateBatch(testContext, 2, b.ID, 2)
	if err != nil || !reflect.DeepEqual(sim, again) {
		t.Fatalf("simulation reused simulated progress: %+v %v", again, err)
	}
	real, err := f.s.DrawBatch(testContext, 2, b.ID, "official", 2)
	if err != nil || real.Pity != sim.Pity {
		t.Fatalf("official draw differs from simulation: %+v %v", real, err)
	}
	for i := range real.Records {
		r := real.Records[i]
		if r.Reward.Amount != sim.Records[i].Reward.Amount || r.GuaranteeCredits != sim.Records[i].GuaranteeCredits || r.Guarantee != sim.Records[i].Guarantee {
			t.Fatalf("simulation/official prize %d: %+v / %+v", i, r, sim.Records[i])
		}
	}
	paused, err := f.s.ChangeBatchState(testContext, 1, b.ID, b.Revision, "pause-for-simulation", false)
	if err != nil {
		t.Fatal(err)
	}
	before = simulationSnapshot(t, f)
	if _, err = f.s.SimulateBatch(testContext, 2, paused.ID, 3); err != nil {
		t.Fatalf("paused simulation denied: %v", err)
	}
	if simulationSnapshot(t, f) != before {
		t.Fatal("paused simulation wrote assets")
	}
	if _, err = f.s.DrawBatch(testContext, 2, paused.ID, "paused-purchase", 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("simulation enabled real paused purchases: %v", err)
	}
	for _, count := range []int{0, 101} {
		if _, err = f.s.SimulateBatch(testContext, 2, b.ID, count); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("count %d: %v", count, err)
		}
	}
	if _, err = f.s.SimulateBatch(testContext, 2, b.ID, 19); !errors.Is(err, ErrInventory) {
		t.Fatalf("virtual inventory exceeded: %v", err)
	}
}

func TestPaidRandomPolicyFrozenServerSideAndSimulationHTTPTrustBoundary(t *testing.T) {
	f := newFixture(t)
	draft, err := f.s.SaveBatch(testContext, 1, Batch{Name: "policy", Purpose: "paid_random", Price: 100, Budget: 1000, CostsConfirmed: true, ContributionSharePPM: 100000, PityPolicy: BatchPityPolicy{SmallAfter: 1, SmallMinimum: 1000000}, Rewards: []BatchReward{{ID: "low", Title: "low", Kind: "credits", Amount: 60, Quantity: 2}}})
	if err != nil || draft.PityPolicy != batchPityPolicy(100) || draft.RequiredBudget != 400 {
		t.Fatalf("client policy accepted: %+v %v", draft, err)
	}
	if _, err = f.s.SimulateBatch(testContext, 2, draft.ID, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("draft simulated: %v", err)
	}
	if _, err = f.pool.Exec(testContext, `UPDATE v3_marketplace.blind_box_batches SET pity_policy='{}' WHERE id=$1`, draft.ID); err == nil {
		t.Fatal("database accepted missing policy")
	}
	b, err := f.s.ChangeBatchState(testContext, 1, draft.ID, draft.Revision, "publish-frozen", true)
	if err != nil {
		t.Fatal(err)
	}
	b.PityPolicy = BatchPityPolicy{SmallAfter: 1}
	if _, err = f.s.SaveBatch(testContext, 1, b); !errors.Is(err, ErrConflict) {
		t.Fatalf("published policy mutable: %v", err)
	}
	h := f.s.Handler(func(r *http.Request) (int64, bool, error) {
		if r.Header.Get("X-Test-User") == "user" {
			return 2, false, nil
		}
		return 0, false, errors.New("unauthenticated")
	})
	for _, tc := range []struct {
		user, body string
		status     int
	}{
		{"", `{"count":1}`, 401},
		{"user", `{"count":1,"pity":{"big_progress":49}}`, 400},
		{"user", `{"count":0}`, 400},
		{"user", `{"count":1}`, 200},
	} {
		r := httptest.NewRequest("POST", "/api/blind-box/batches/1/simulate", bytes.NewBufferString(tc.body))
		r.Header.Set("X-Test-User", tc.user)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("simulation HTTP %s: %d %s", tc.body, w.Code, w.Body.String())
		}
	}
}

type batchFrozenPlan struct{}

func (batchFrozenPlan) GrantRewardTx(context.Context, pgx.Tx, int64, int64, string) error {
	return ErrUnavailable
}
func (batchFrozenPlan) GrantFrozenRewardTx(context.Context, pgx.Tx, int64, json.RawMessage, string) error {
	return ErrUnavailable
}
func (batchFrozenPlan) FreezeRewardPlanTx(context.Context, pgx.Tx, int64) (json.RawMessage, error) {
	return json.RawMessage(`{"credits":500,"period_seconds":3600,"name":"frozen"}`), nil
}

func TestPaidRandomSubscriptionPrizeDoesNotReplacePermanentFloor(t *testing.T) {
	f := newFixture(t)
	f.s.subscriptions = batchFrozenPlan{}
	if _, err := f.pool.Exec(testContext, `INSERT INTO v3_commerce.plans(id,name,price_minor,credits,period_seconds) VALUES(1,'frozen',100,500,3600)`); err != nil {
		t.Fatal(err)
	}
	b := seedBatch(t, f, "paid_random", []BatchReward{{ID: "plan", Title: "plan", Kind: "subscription", PlanID: 1, Quantity: 1}})
	if b.RequiredBudget != 700 {
		t.Fatalf("subscription floor reserve: %+v", b)
	}
	if _, err := f.pool.Exec(testContext, `INSERT INTO v3_marketplace.blind_box_batch_pity(user_id,opened,small_progress,big_progress) VALUES(2,9,9,9)`); err != nil {
		t.Fatal(err)
	}
	out, err := f.s.DrawBatch(testContext, 2, b.ID, "subscription-small", 1)
	if err != nil || out.Records[0].PropID <= 0 || out.Records[0].Reward.Amount != 500 || out.Records[0].GuaranteeCredits != 100 || out.Records[0].Guarantee != "small" || out.Pity != (PityState{Opened: 10, BigProgress: 10}) {
		t.Fatalf("subscription floor: %+v %v", out, err)
	}
	stats, err := f.s.BatchStats(testContext, b.ID)
	if err != nil || stats.RewardCredits != 100 || stats.SubscriptionAwardedCount != 1 || stats.Spent != 600 {
		t.Fatalf("subscription supplement stats: %+v %v", stats, err)
	}
}
