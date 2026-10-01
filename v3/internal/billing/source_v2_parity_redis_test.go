//go:build pgintegration

package billing

import (
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestSourceV2GrossRoundsOldQuotaBeforeTimesTwo(t *testing.T) {
	for _, quantum := range []int64{1, 2} {
		s, rdb, _, clock := setup(t, 10_000)
		req, snapshot := sourceFixture(clock.now())
		req.Targets[0].MultiplierPPM = 1_000_000
		snapshot.Channels[1].MultiplierCardUserEnabled = true
		policy := snapshot.Market.Channels[1]
		policy.ModelPrices = map[string]catalog.Price{"model": {Mode: "per_request", PerRequest: 606, Rules: map[string]any{"money_quantum": quantum}}}
		snapshot.Market.Channels[1] = policy
		profile := snapshot.AccountProfiles[7]
		profile.Cards = []catalog.MultiplierCard{{ID: 9, PropType: "consume_discount_10", MultiplierPPM: 100_000, ExpiresAt: clock.now().Add(s.cfg.ReservationExpiry)}}
		snapshot.AccountProfiles[7] = profile
		s.snapshot = func() *catalog.Snapshot { return snapshot }
		if err := s.Reserve(ctx, req); err != nil {
			t.Fatal(err)
		}
		if err := s.Finalize(ctx, req, gateway.Outcome{Charge: true, Target: &req.Targets[0]}); err != nil {
			t.Fatal(err)
		}
		all := events(t, rdb)
		gross := "61"
		if quantum == 2 {
			gross = "60"
		}
		if len(all) != 1 || all[0]["usage_total_amount"] != "606" || all[0][FieldMarketGross] != gross || all[0][FieldCardBefore] != "6060" || all[0][FieldCardAfter] != "606" {
			t.Fatalf("quantum%d old303->old30×2 gross, event=%#v", quantum, all)
		}
	}
}

func TestSourceV2MixedResidualCannotCreateOddQuotaOrFreeShare(t *testing.T) {
	s, rdb, _, clock := setup(t, 1000)
	req, snapshot := sourceFixture(clock.now())
	policy := snapshot.Market.Channels[1]
	policy.ModelPrices["model"] = catalog.Price{Mode: "per_request", PerRequest: 900, Rules: map[string]any{"money_quantum": 2}}
	snapshot.Market.Channels[1] = policy
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	if err := rdb.HSet(ctx, BalanceKey(43), "balance", 451, "reserved", 0, "ver", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, req, gateway.Outcome{Charge: true, Target: &req.Targets[0]}); err != nil {
		t.Fatal(err)
	}
	all := events(t, rdb)
	// Source contribution450 (225old), residual wallet ceil45micro->46(23old),
	// aggregate496 (248old), gross round((225/10)+23)=46old->92micro.
	if len(all) != 2 || all[0][FieldAmount] != "450" || all[1][FieldAmount] != "46" || all[0]["usage_total_amount"] != "496" || all[0][FieldMarketGross] != "92" {
		t.Fatalf("v2 mixed quantum facts=%#v", all)
	}
}
