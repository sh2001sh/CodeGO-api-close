//go:build pgintegration

package billing

import (
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestSourceWorkflowRestoresFundingAndDurableAggregate(t *testing.T) {
	s, rdb, _, clock := setup(t, 1000)
	req, snapshot := sourceFixture(clock.now())
	profile := snapshot.AccountProfiles[7]
	profile.Subscriptions[0].ModelLimits = map[string]int64{"model": 700}
	profile.Subscriptions[0].ModelUsage = map[string]int64{"model": 250}
	snapshot.AccountProfiles[7] = profile
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	workflow := NewWorkflowSettler(s)
	reservation, err := workflow.Reserve(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.EstimatedCredits != 495 {
		t.Fatalf("estimated aggregate=%d", reservation.EstimatedCredits)
	}
	restored, err := restoreWorkflowHold(req, reservation)
	if err != nil || !restored.sourceMode || restored.sourceLimits[43].Limit != 700 || restored.sourceLimits[43].Used != 250 {
		t.Fatalf("source restore=%+v/%v", restored, err)
	}
	for _, part := range restored.funding {
		if ttl, _ := rdb.PTTL(ctx, part.keys.reservation).Result(); ttl != -1 {
			t.Fatalf("task hold TTL=%s", ttl)
		}
	}
	// Durable completion uses a resolver with current group/price data removed.
	req.Targets[0].Group = ""
	s.snapshot = func() *catalog.Snapshot { return nil }
	for range 2 {
		actual, err := workflow.Finalize(ctx, req, reservation, native.Result{Status: "completed"})
		if err != nil || actual != 495 {
			t.Fatalf("workflow aggregate=%d/%v", actual, err)
		}
	}
	if ttl, _ := rdb.PTTL(ctx, keysFor(account, req.ID).done).Result(); ttl != -1 {
		t.Fatalf("task done TTL=%s", ttl)
	}
	if len(events(t, rdb)) != 2 {
		t.Fatal("durable completion repeated funding")
	}
}

func TestSourceWALReplayPreservesFundingMetadataAndOneDebit(t *testing.T) {
	s, rdb, _, clock := setup(t, 1000)
	req, snapshot := sourceFixture(clock.now())
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	if err := rdb.HSet(ctx, BalanceKey(43), "balance", 450, "reserved", 0, "ver", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	w, err := openWAL(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.wal = w
	t.Cleanup(s.Close)
	for range 3 {
		s.br.fail()
	}
	out := gateway.Outcome{Charge: true, Delivered: true, Terminal: gateway.TerminalCompleted, Target: &req.Targets[0]}
	for range 2 {
		if err := s.Finalize(ctx, req, out); err != nil {
			t.Fatal(err)
		}
	}
	if len(events(t, rdb)) != 0 {
		t.Fatal("outage charge was not captured by WAL")
	}
	s.snapshot = func() *catalog.Snapshot { return nil }
	if err := s.Replay(ctx); err != nil {
		t.Fatal(err)
	}
	if bal, held := balance(t, rdb); bal != 955 || held != 0 {
		t.Fatalf("wallet replay=%d/%d", bal, held)
	}
	all := events(t, rdb)
	if len(all) != 2 || all[0]["usage_total_amount"] != "495" || all[0][FieldMarketGross] != "90" || all[0][FieldBillingSource] != "mixed" {
		t.Fatalf("source replay=%#v", all)
	}
}

func TestSourceActualBeyondEstimateRoutesExcessToWallet(t *testing.T) {
	s, rdb, _, clock := setup(t, 10_000)
	req, snapshot := sourceFixture(clock.now())
	req.Body = []byte(`{"n":1}`)
	policy := snapshot.Market.Channels[1]
	policy.ModelPrices = map[string]catalog.Price{"model": {Mode: "per_request", PerRequest: 900, Rules: map[string]any{"billing_unit": "image"}}}
	snapshot.Market.Channels[1] = policy
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	if err := rdb.HSet(ctx, BalanceKey(43), "balance", 10_000, "reserved", 0, "ver", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, req, gateway.Outcome{Charge: true, Target: &req.Targets[0], Usage: gateway.Usage{ImageCount: 2}}); err != nil {
		t.Fatal(err)
	}
	if bal, held := balance(t, rdb); bal != 9910 || held != 0 {
		t.Fatalf("excess wallet=%d/%d", bal, held)
	}
	if sub, _ := rdb.HGet(ctx, BalanceKey(43), "balance").Int64(); sub != 9100 {
		t.Fatalf("admitted subscription=%d", sub)
	}
	if all := events(t, rdb); len(all) != 2 || all[0]["usage_total_amount"] != "990" || all[0][FieldMarketGross] != "180" {
		t.Fatalf("excess actual facts=%#v", all)
	}
}
