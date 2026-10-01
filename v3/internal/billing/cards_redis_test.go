//go:build pgintegration

package billing

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func cardSnapshot(now time.Time, price int64, cards ...catalog.MultiplierCard) *catalog.Snapshot {
	for i := range cards {
		cards[i].ExpiresAt = now.Add(time.Hour)
	}
	return &catalog.Snapshot{Groups: map[string]catalog.Group{"default": {Multiplier: 1}},
		Prices:          map[string]catalog.Price{"gpt": {Mode: "per_request", PerRequest: price}, "video": {Mode: "per_request", PerRequest: price}},
		Channels:        map[int64]*catalog.Channel{3: {ID: 3, MultiplierCardSupported: true, MultiplierCardUserEnabled: true}, 4: {ID: 4, MultiplierCardSupported: true, MultiplierCardUserEnabled: false}},
		AccountProfiles: map[int64]catalog.AccountProfile{7: {WalletAccountID: account, Cards: cards}}}
}

func cardRequest(id string) *gateway.Request {
	r := newReq(id)
	r.Targets = []gateway.Target{{ChannelID: 3, CredentialID: 300}}
	return r
}

func consumptionCard() catalog.MultiplierCard {
	return catalog.MultiplierCard{ID: 81, PropType: "consume_discount_90", MultiplierPPM: 900000}
}

func TestConsumptionCardsRemainUnlimitedAndConcurrentIdempotent(t *testing.T) {
	// Regression from v2 IsOfficialUnlimitedAndIdempotent: max/used are legacy
	// facts, never a new capacity limit on consumption discount cards.
	s, rdb, _, clock := setup(t, 20000)
	card := consumptionCard()
	card.MaxDiscountMicro, card.UsedDiscountMicro = 1, 9007199254740993
	snapshot := cardSnapshot(clock.now(), 1000, card)
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	const count = 12
	requests := make([]*gateway.Request, count)
	for i := range requests {
		requests[i] = cardRequest(fmt.Sprintf("consume-%d", i))
		if err := s.Reserve(ctx, requests[i]); err != nil {
			t.Fatal(err)
		}
		if requests[i].Reserve.(*hold).amount != 900 {
			t.Fatal("consumption admission price was not discounted")
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for _, req := range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.Finalize(ctx, req, completed(0, 0))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if bal, held := balance(t, rdb); bal != 9200 || held != 0 {
		t.Fatalf("unlimited discount changed: balance=%d reserved=%d", bal, held)
	}
	for _, event := range events(t, rdb) {
		if event[FieldAmount] != "900" || event[FieldCardID] != "81" || event[FieldCardBefore] != "1000" || event[FieldCardAfter] != "900" {
			t.Fatalf("incorrect consumption audit: %v", event)
		}
	}
	for _, req := range requests {
		if err := s.Finalize(ctx, req, completed(0, 0)); err != nil {
			t.Fatal(err)
		}
	}
	if len(events(t, rdb)) != count {
		t.Fatal("redelivery duplicated money or discount audit")
	}
}

func TestConsumptionIgnoresPackageCardsAndFreezesActualEligibility(t *testing.T) {
	s, rdb, _, clock := setup(t, 5000)
	snapshot := cardSnapshot(clock.now(), 1000,
		catalog.MultiplierCard{ID: 80, PropType: "monthly_pass_multiplier", MultiplierPPM: 100000},
		catalog.MultiplierCard{ID: 79, PropType: "zero_hour_multiplier", MultiplierPPM: 0})
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	req := cardRequest("packages")
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, req, completed(0, 0)); err != nil {
		t.Fatal(err)
	}
	if events(t, rdb)[0][FieldAmount] != "1000" || events(t, rdb)[0][FieldCardID] != nil {
		t.Fatal("package card became a consumption discount")
	}
	snapshot = cardSnapshot(clock.now(), 1000, consumptionCard())
	stamp := clock.now()
	for i, channel := range []int64{3, 4} {
		clock.ms.Store(stamp.UnixMilli())
		req := cardRequest(fmt.Sprintf("eligibility-%d", i))
		req.Targets = append(req.Targets, gateway.Target{ChannelID: 4})
		if err := s.Reserve(ctx, req); err != nil {
			t.Fatal(err)
		}
		if req.Reserve.(*hold).amount != 1000 {
			t.Fatal("ineligible fallback admission was discounted")
		}
		// Card expiry/snapshot replacement after admission cannot change the
		// frozen charging decision for an already executed request.
		clock.ms.Add(int64(time.Hour / time.Millisecond))
		out := completed(0, 0)
		out.Target.ChannelID = channel
		if err := s.Finalize(ctx, req, out); err != nil {
			t.Fatal(err)
		}
		want := int64(900)
		if channel == 4 {
			want = 1000
		}
		all := events(t, rdb)
		last := all[len(all)-1]
		if last[FieldAmount] != strconv.FormatInt(want, 10) || channel == 4 && last[FieldCardID] != nil {
			t.Fatalf("channel %d incorrect money/audit: %v", channel, last)
		}
	}
}

func TestConsumptionExactBigintToolsAndRefund(t *testing.T) {
	s, rdb, _, clock := setup(t, 9007199254740993)
	snapshot := cardSnapshot(clock.now(), 9007199254740993, consumptionCard())
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	req := cardRequest("bigint")
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, req, completed(0, 0)); err != nil {
		t.Fatal(err)
	}
	if event := events(t, rdb)[0]; event[FieldAmount] != "8106479329266894" {
		t.Fatalf("bigint consumption precision: %v", event)
	}
	snapshot.Prices["gpt"], snapshot.Groups["default"] = catalog.Price{Mode: "per_request", PerRequest: 1000}, catalog.Group{Multiplier: .5}
	req = cardRequest("tools")
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	out := completed(0, 0)
	out.Usage.ToolCalls = map[string]int64{"web_search": 1} // absolute 10000; model 500
	if err := s.Finalize(ctx, req, out); err != nil {
		t.Fatal(err)
	}
	if event := events(t, rdb)[1]; event[FieldAmount] != "9450" {
		t.Fatalf("consumption discount did not include tools: %v", event)
	}
	req = cardRequest("refund")
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, req, gateway.Outcome{Terminal: gateway.TerminalUpstreamErrorBeforeOutput}); err != nil {
		t.Fatal(err)
	}
	all := events(t, rdb)
	last := all[len(all)-1]
	if last[FieldAmount] != "0" || last[FieldCardID] != nil {
		t.Fatalf("refund carried a discount audit: %v", last)
	}
}

func TestWorkflowConsumptionReturnsCommittedActual(t *testing.T) {
	s, rdb, _, clock := setup(t, 3000)
	snapshot := cardSnapshot(clock.now(), 999, consumptionCard())
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	req := workflowTestRequest()
	reservation, err := NewWorkflowSettler(s).Reserve(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.AccountProfiles = nil
	w := NewWorkflowSettler(s)
	for range 2 {
		actual, err := w.Finalize(ctx, workflowTestRequest(), reservation, native.Result{Status: "completed"})
		if err != nil || actual != 899 {
			t.Fatalf("committed task actual=%d err=%v", actual, err)
		}
	}
	if bal, held := balance(t, rdb); bal != 2101 || held != 0 || len(events(t, rdb)) != 1 {
		t.Fatalf("task was not idempotent: %d %d", bal, held)
	}
	if value, _ := rdb.Get(ctx, keysFor(account, req.ID).done).Result(); value != strconv.FormatInt(899, 10) {
		t.Fatalf("durable actual marker=%s", value)
	}
}
