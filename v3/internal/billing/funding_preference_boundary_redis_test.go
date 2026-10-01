//go:build pgintegration

package billing

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func imagePreference(req *gateway.Request, snapshot *catalog.Snapshot) {
	req.Body = []byte(`{"n":1}`)
	policy := snapshot.Market.Channels[1]
	policy.ModelPrices["model"] = catalog.Price{Mode: "per_request", PerRequest: 100, Rules: map[string]any{"billing_unit": "image"}}
	snapshot.Market.Channels[1] = policy
}

func TestFundingPreferenceOnlyActualExtendsOrSettlesVisibleShortfall(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		sub, cap, accepted, remainder, missing int64
	}{
		{"additional-active-money", 300, 0, 200, 100, 0},
		{"available-money-shortfall", 150, 0, 150, 0, 50},
		{"model-cap-shortfall", 300, 120, 120, 180, 80},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, rdb, req, snapshot, _ := preferenceFixture(t, 1000, tc.sub, "subscription_only")
			imagePreference(req, snapshot)
			if tc.cap > 0 {
				profile := snapshot.AccountProfiles[7]
				profile.Subscriptions[0].ModelLimits = map[string]int64{"model": tc.cap}
				snapshot.AccountProfiles[7] = profile
			}
			req.Principal.BudgetLimited, req.Principal.BudgetAccountID = true, 55
			if err := rdb.HSet(ctx, BalanceKey(55), "balance", 1000, "reserved", 0, "ver", 0).Err(); err != nil {
				t.Fatal(err)
			}
			var log bytes.Buffer
			s.log = slog.New(slog.NewTextHandler(&log, nil))
			if err := s.Reserve(ctx, req); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := s.Finalize(ctx, req, gateway.Outcome{Charge: true, Target: &req.Targets[0], Usage: gateway.Usage{ImageCount: 2}}); err != nil {
					t.Fatal(err)
				}
			}
			if b, held := balance(t, rdb); b != 1000 || held != 0 {
				t.Fatalf("excluded wallet=%d/%d", b, held)
			}
			if b, _ := rdb.HGet(ctx, BalanceKey(43), "balance").Int64(); b != tc.remainder {
				t.Fatalf("accepted sub balance=%d want%d", b, tc.remainder)
			}
			if held, _ := rdb.HGet(ctx, BalanceKey(43), "reserved").Int64(); held != 0 {
				t.Fatalf("unfinished hold=%d", held)
			}
			if b, _ := rdb.HGet(ctx, BalanceKey(55), "balance").Int64(); b != 1000-tc.accepted {
				t.Fatalf("key mirror=%d", b)
			}
			all := events(t, rdb)
			if len(all) != 2 || all[0][FieldAmount] != fmt.Sprint(tc.accepted) || all[0]["usage_total_amount"] != fmt.Sprint(tc.accepted) || all[0][FieldBillingSource] != "subscription" {
				t.Fatalf("accepted events=%#v", all)
			}
			warnings := strings.Count(log.String(), "subscription-only settlement shortfall")
			if tc.missing > 0 {
				if all[0]["source_shortfall_micro"] != fmt.Sprint(tc.missing) || all[0]["source_requested_micro"] != "200" || warnings != 1 {
					t.Fatalf("missing amount not persisted/reported=%#v logs=%s", all, log.String())
				}
			} else if all[0]["source_shortfall_micro"] != nil || warnings != 0 {
				t.Fatalf("false shortfall=%#v %s", all, log.String())
			}
		})
	}
}

func TestFundingPreferenceOnlyExtensionProtectsOtherHoldAndRefund(t *testing.T) {
	s, rdb, req, snapshot, _ := preferenceFixture(t, 1000, 300, "subscription_only")
	imagePreference(req, snapshot)
	other := *req
	other.ID = "other-only-hold"
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, &other); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, req, gateway.Outcome{Charge: true, Target: &req.Targets[0], Usage: gateway.Usage{ImageCount: 4}}); err != nil {
		t.Fatal(err)
	}
	if b, _ := rdb.HGet(ctx, BalanceKey(43), "balance").Int64(); b != 100 {
		t.Fatalf("other admitted money consumed=%d", b)
	}
	if held, _ := rdb.HGet(ctx, BalanceKey(43), "reserved").Int64(); held != 100 {
		t.Fatalf("other hold released=%d", held)
	}
	for range 2 {
		if err := s.Finalize(ctx, &other, gateway.Outcome{}); err != nil {
			t.Fatal(err)
		}
	}
	if b, _ := rdb.HGet(ctx, BalanceKey(43), "balance").Int64(); b != 100 {
		t.Fatalf("refund debited=%d", b)
	}
	if held, _ := rdb.HGet(ctx, BalanceKey(43), "reserved").Int64(); held != 0 {
		t.Fatalf("refund retained hold=%d", held)
	}
	if b, held := balance(t, rdb); b != 1000 || held != 0 {
		t.Fatalf("excluded wallet=%d/%d", b, held)
	}
	if all := events(t, rdb); len(all) != 2 || all[0][FieldAmount] != "200" || all[0]["source_shortfall_micro"] != "200" || all[1][FieldAmount] != "0" {
		t.Fatalf("partial and refund=%#v", all)
	}
}

func TestFundingPreferenceFrozenWorkflowWALAndShortfallWarningOnce(t *testing.T) {
	s, rdb, req, snapshot, _ := preferenceFixture(t, 1000, 150, "subscription_only")
	imagePreference(req, snapshot)
	var logs bytes.Buffer
	s.log = slog.New(slog.NewTextHandler(&logs, nil))
	var err error
	s.wal, err = openWAL(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	reservation, err := encodeWorkflowHold(req, req.Reserve.(*hold))
	if err != nil {
		t.Fatal(err)
	}
	profile := snapshot.AccountProfiles[7]
	profile.BillingPreference = "wallet_only"
	snapshot.AccountProfiles[7] = profile
	restored, err := restoreWorkflowHold(req, reservation)
	if err != nil || restored.fundingPreference != "subscription_only" {
		t.Fatalf("lost frozen preference=%#v %v", restored, err)
	}
	req.Reserve = restored
	for range 3 {
		s.br.fail()
	}
	for range 2 {
		if err := s.Finalize(ctx, req, gateway.Outcome{Charge: true, Target: &req.Targets[0], Usage: gateway.Usage{ImageCount: 2}}); err != nil {
			t.Fatal(err)
		}
	}
	if len(events(t, rdb)) != 0 {
		t.Fatal("WAL path settled before replay")
	}
	s.snapshot = func() *catalog.Snapshot { return nil }
	for range 2 {
		if err := s.Replay(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if b, held := balance(t, rdb); b != 1000 || held != 0 {
		t.Fatalf("replay used excluded wallet=%d/%d", b, held)
	}
	if b, _ := rdb.HGet(ctx, BalanceKey(43), "balance").Int64(); b != 0 {
		t.Fatalf("accepted subscription replay=%d", b)
	}
	if all := events(t, rdb); len(all) != 1 || all[0][FieldAmount] != "150" || all[0]["source_shortfall_micro"] != "50" || all[0][FieldFundingPreference] != "subscription_only" {
		t.Fatalf("frozen preference WAL=%#v", all)
	}
	if count := strings.Count(logs.String(), "subscription-only settlement shortfall"); count != 1 {
		t.Fatalf("replay warning count=%d logs=%s", count, logs.String())
	}
}
