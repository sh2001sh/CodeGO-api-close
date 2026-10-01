//go:build pgintegration

package billing

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestWorkflowExactActualMediaAndTypedUsagePriority(t *testing.T) {
	for _, tc := range []struct {
		name  string
		units float64
		usage gateway.Usage
		want  int64
	}{
		{"decimal seconds", 9.5, gateway.Usage{}, 1439},
		{"authoritative microseconds", 999, gateway.Usage{VideoDurationMicros: 4500000}, 682},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, rdb, _, _ := setup(t, 10000)
			snapshot := &catalog.Snapshot{Groups: map[string]catalog.Group{"default": {Multiplier: 1.5}},
				Prices: map[string]catalog.Price{"video": {Mode: "per_request", PerRequest: 101, Rules: map[string]any{"billing_unit": "video_second"}}}}
			s.snapshot = func() *catalog.Snapshot { return snapshot }
			request := workflowTestRequest()
			reservation, err := NewWorkflowSettler(s).Reserve(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			if reservation.EstimatedCredits != 606 {
				t.Fatalf("estimate=%d", reservation.EstimatedCredits)
			}
			actual, err := NewWorkflowSettler(s).Finalize(ctx, workflowTestRequest(), reservation, native.Result{Status: "completed", Units: tc.units, Usage: tc.usage})
			if err != nil || int64(actual) != tc.want {
				t.Fatalf("actual=%d want=%d err=%v", actual, tc.want, err)
			}
			if bal, held := balance(t, rdb); bal != 10000-tc.want || held != 0 {
				t.Fatalf("balance=%d held=%d", bal, held)
			}
		})
	}
}

func TestWorkflowMissingActualUnitsCannotBecomeFree(t *testing.T) {
	s, rdb, _, _ := setup(t, 10000)
	snapshot := &catalog.Snapshot{Groups: map[string]catalog.Group{"default": {Multiplier: 1}},
		Prices: map[string]catalog.Price{"video": {Mode: "per_request", PerRequest: 100, Rules: map[string]any{"billing_unit": "video_second"}}}}
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	request := workflowTestRequest()
	reservation, err := NewWorkflowSettler(s).Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []native.Result{{Status: "completed"}, {Status: "in_progress", Units: 4}, {Status: "completed", Units: -1}} {
		if _, err := NewWorkflowSettler(s).Finalize(ctx, request, reservation, result); err == nil {
			t.Fatal("invalid result finalized")
		}
	}
	if bal, held := balance(t, rdb); bal != 10000 || held != 400 {
		t.Fatalf("bad units mutated money: %d/%d", bal, held)
	}
}

func TestWorkflowFrozenConsumptionCardSurvivesRestart(t *testing.T) {
	s, rdb, _, c := setup(t, 1000)
	snapshot := &catalog.Snapshot{Groups: map[string]catalog.Group{"default": {Multiplier: 1}},
		Prices:   map[string]catalog.Price{"video": {Mode: "per_request", PerRequest: 200}},
		Channels: map[int64]*catalog.Channel{3: {MultiplierCardSupported: true, MultiplierCardUserEnabled: true}},
		AccountProfiles: map[int64]catalog.AccountProfile{7: {WalletAccountID: 42, Cards: []catalog.MultiplierCard{{ID: 99,
			PropType: "consume_discount_10", MultiplierPPM: 100000, MaxDiscountMicro: 50, ExpiresAt: c.now().Add(time.Hour)}}}}}
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	request := workflowTestRequest()
	request.Received = c.now()
	reservation, err := NewWorkflowSettler(s).Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	// The live catalog changes while generation runs; the original eligible
	// consumption card still defines the eventual charge.
	snapshot.AccountProfiles = nil
	snapshot.Channels[3].MultiplierCardUserEnabled = false
	actual, err := NewWorkflowSettler(s).Finalize(ctx, workflowTestRequest(), reservation, native.Result{Status: "completed"})
	if err != nil || actual != 20 {
		t.Fatalf("consumption charge=%d err=%v", actual, err)
	}
	if bal, held := balance(t, rdb); bal != 980 || held != 0 {
		t.Fatalf("balance=%d held=%d", bal, held)
	}
	actual, err = NewWorkflowSettler(s).Finalize(ctx, workflowTestRequest(), reservation, native.Result{Status: "completed", Units: 10})
	if err != nil || actual != 20 || len(events(t, rdb)) != 1 {
		t.Fatalf("duplicate actual=%d err=%v", actual, err)
	}
}
