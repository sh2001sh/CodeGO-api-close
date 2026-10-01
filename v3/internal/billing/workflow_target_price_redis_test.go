//go:build pgintegration

package billing

import (
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestWorkflowFrozenOwnerPriceSurvivesLostRouteGroup(t *testing.T) {
	s, rdb, _, _ := setup(t, 1000)
	request, snapshot := targetPricingFixture()
	request.ID = "owner-task"
	request.Principal.UserID, request.Principal.KeyID = 7, 70
	request.Targets = request.Targets[:1]
	snapshot.Prices = nil
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	reservation, err := NewWorkflowSettler(s).Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.EstimatedCredits != 90 {
		t.Fatalf("owner estimate=%d", reservation.EstimatedCredits)
	}
	snapshot.Market.Channels[1].ModelPrices["model"] = catalog.Price{Mode: "per_request", PerRequest: 99999}
	copyReq := *request
	copyReq.Targets = []gateway.Target{{ChannelID: 1, CredentialID: 11}} // durable Task.Request omits route Group/PPM
	copyReq.Reserve = nil
	actual, err := NewWorkflowSettler(s).Finalize(ctx, &copyReq, reservation, native.Result{Status: "completed"})
	if err != nil || actual != 90 {
		t.Fatalf("owner actual=%d error=%v", actual, err)
	}
	if bal, held := balance(t, rdb); bal != 910 || held != 0 {
		t.Fatalf("money=%d/%d", bal, held)
	}
	all := events(t, rdb)
	if len(all) != 1 || all[0][FieldMarketGross] != "90" || all[0][FieldMarketMultiplier] != "100000" || all[0][FieldBillingSource] != "wallet" {
		t.Fatalf("frozen market income facts lost after restart: %v", all)
	}
}

func TestWorkflowFrozenTargetPPMRemainsExactBeyondFloatPrecision(t *testing.T) {
	const actual = int64(9007199254740993)
	s, rdb, _, _ := setup(t, actual+1)
	request, snapshot := targetPricingFixture()
	request.ID = "exact-ppm-task"
	request.Principal.UserID, request.Principal.KeyID = 7, 70
	request.Targets = request.Targets[:1]
	request.Targets[0].MultiplierPPM = actual
	snapshot.Market.Channels[1].ModelPrices["model"] = catalog.Price{Mode: "per_request", PerRequest: 1000000}
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	reservation, err := NewWorkflowSettler(s).Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if int64(reservation.EstimatedCredits) != actual {
		t.Fatalf("estimate=%d", reservation.EstimatedCredits)
	}
	request.Targets = []gateway.Target{{ChannelID: 1, CredentialID: 11}}
	got, err := NewWorkflowSettler(s).Finalize(ctx, request, reservation, native.Result{Status: "completed"})
	if err != nil || int64(got) != actual {
		t.Fatalf("actual=%d error=%v", got, err)
	}
	if bal, held := balance(t, rdb); bal != 1 || held != 0 {
		t.Fatalf("money=%d/%d", bal, held)
	}
}
