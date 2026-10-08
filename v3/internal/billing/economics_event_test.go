package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestProcurementExactPPMRejectsMalformedOrOverflow(t *testing.T) {
	for _, row := range []struct {
		raw  string
		want int64
	}{
		{"0.2", 200000}, {"0.0000005", 1}, {"0.0000004", 0}, {"9007199254.740993", 9007199254740993},
	} {
		pool, ppm, err := freezeProcurement(gateway.Target{RoutePoolID: 17, ProcurementCostMultiplier: row.raw})
		if err != nil || pool != 17 || ppm != row.want {
			t.Fatalf("%s: %d/%d %v", row.raw, pool, ppm, err)
		}
	}
	for _, target := range []gateway.Target{
		{RoutePoolID: -1}, {RoutePoolID: 17}, {ProcurementCostMultiplier: "0.2"},
		{RoutePoolID: 17, ProcurementCostMultiplier: "-1"}, {RoutePoolID: 17, ProcurementCostMultiplier: "NaN"},
		{RoutePoolID: 17, ProcurementCostMultiplier: "1e1001"},
	} {
		if _, _, err := freezeProcurement(target); err == nil {
			t.Fatalf("accepted invalid procurement target %#v", target)
		}
	}
	if _, _, err := freezeProcurement(gateway.Target{RoutePoolID: 17, ProcurementCostMultiplier: "9223372036854.775808"}); !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("overflow=%v", err)
	}
}

func TestProcurementFrozenThroughTargetMutationAndWorkflowRestore(t *testing.T) {
	req, snapshot := targetPricingFixture()
	req.Targets = req.Targets[:1]
	req.ID = "frozen-cost"
	req.Targets[0].RoutePoolID, req.Targets[0].ProcurementCostMultiplier = 17, "0.2000005"
	frozen, price, factor, err := (&Settler{}).freezeTargetPrices(req, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	h := &hold{account: 42, amount: 90, keys: keysFor(42, req.ID), price: price, multiplier: factor, targetPrices: frozen}
	reservation, err := encodeWorkflowHold(req, h)
	if err != nil {
		t.Fatal(err)
	}
	req.Targets[0].RoutePoolID, req.Targets[0].ProcurementCostMultiplier = 99, "999"
	restored, err := restoreWorkflowHold(req, reservation)
	if err != nil {
		t.Fatal(err)
	}
	for _, admitted := range []*hold{h, restored} {
		rec, err := appendEconomicsCall(walRecord{}, admitted, gateway.Outcome{Charge: true, Target: &req.Targets[0]})
		if err != nil || len(rec.Args) != 8 || rec.Args[1] != "17" || rec.Args[3] != "200001" || rec.Args[5] != "180" || rec.Args[7] != "90" {
			t.Fatalf("frozen cost=%v %v", rec.Args, err)
		}
		rec, err = appendEconomicsCall(walRecord{}, admitted, gateway.Outcome{Target: &req.Targets[0]})
		if err != nil || len(rec.Args) != 0 {
			t.Fatalf("refund retained economics=%v %v", rec.Args, err)
		}
	}
	// Malformed selected procurement refuses before querying an account or Redis.
	req.Targets[0].ProcurementCostMultiplier = "broken"
	s := &Settler{snapshot: func() *catalog.Snapshot { return snapshot }}
	if err := s.Reserve(context.Background(), req); err == nil {
		t.Fatal("invalid cost reached admission")
	}
}
