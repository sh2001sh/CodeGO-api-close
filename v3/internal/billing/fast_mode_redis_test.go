//go:build pgintegration

package billing

import (
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func fastReq(id, tier string) *gateway.Request {
	req := newReq(id)
	req.Protocol, req.Path = gateway.ProtocolResponses, "/v1/responses"
	req.Body = []byte(`{"model":"gpt","input":"hi","max_output_tokens":100,"service_tier":"` + tier + `"}`)
	return req
}

func TestFastModeRedisReservationSettlementAndIdempotency(t *testing.T) {
	for _, tc := range []struct {
		tier string
		want int64
	}{{"fast", 100}, {"priority", 100}, {"default", 50}, {"", 100}} {
		t.Run(tc.tier, func(t *testing.T) {
			s, rdb, _, _ := setup(t, 1_000_000)
			req := fastReq("fast-r1", "fast")
			for i := 0; i < 2; i++ {
				if err := s.Reserve(ctx, req); err != nil {
					t.Fatal(err)
				}
			}
			wantHold := 2 * (int64(len(req.Body)+3)/4 + 200)
			if b, r := balance(t, rdb); b != 1_000_000 || r != wantHold {
				t.Fatalf("reserve balance=%d hold=%d; want 1000000/%d", b, r, wantHold)
			}
			out := completed(10, 20)
			out.Usage.ServiceTier = tc.tier
			for i := 0; i < 2; i++ {
				if err := s.Finalize(ctx, req, out); err != nil {
					t.Fatal(err)
				}
			}
			if b, r := balance(t, rdb); b != 1_000_000-tc.want || r != 0 {
				t.Fatalf("settled balance=%d hold=%d; want %d/0", b, r, 1_000_000-tc.want)
			}
			ev := events(t, rdb)
			multiplier := "2"
			if tc.tier == "default" {
				multiplier = "1"
			}
			if len(ev) != 1 || ev[0]["service_tier"] != tc.tier || ev[0]["service_tier_multiplier"] != multiplier {
				t.Fatalf("events=%v", ev)
			}
		})
	}
}

func TestFastModeRedisInsufficientBalanceAndRefund(t *testing.T) {
	s, rdb, _, _ := setup(t, 300)
	if err := s.Reserve(ctx, fastReq("fast-insufficient", "priority")); !errors.Is(err, gateway.ErrInsufficientCredits) {
		t.Fatalf("reserve=%v; want insufficient credits", err)
	}
	if b, r := balance(t, rdb); b != 300 || r != 0 {
		t.Fatalf("denied hold changed balance=%d hold=%d", b, r)
	}
	if len(events(t, rdb)) != 0 {
		t.Fatal("rejected request generated a charge")
	}
	s, rdb, _, _ = setup(t, 1000)
	req := fastReq("fast-refund", "fast")
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, req, gateway.Outcome{Terminal: gateway.TerminalUpstreamErrorBeforeOutput}); err != nil {
		t.Fatal(err)
	}
	if b, r := balance(t, rdb); b != 1000 || r != 0 {
		t.Fatalf("failed request balance=%d hold=%d", b, r)
	}
	if ev := events(t, rdb); len(ev) != 1 || ev[0][FieldAmount] != "0" {
		t.Fatalf("refund events=%v", ev)
	}
}
