//go:build pgintegration

package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

type asyncLoader struct {
	*loader
	exists bool
	err    error
}

func (l *asyncLoader) AsyncTaskExists(context.Context, string, int64, int64) (bool, error) {
	return l.exists, l.err
}

func TestWorkflowRestartChargesFrozenPriceAndRefunds(t *testing.T) {
	s, rdb, _, _ := setup(t, 1000)
	snapshot := &catalog.Snapshot{Groups: map[string]catalog.Group{"default": {Multiplier: 1.5}},
		Prices: map[string]catalog.Price{"video": {Mode: "per_request", PerRequest: 101}}}
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	request := workflowTestRequest()
	reservation, err := NewWorkflowSettler(s).Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if ttl, _ := rdb.PTTL(ctx, keysFor(42, request.ID).reservation).Result(); ttl != -time.Nanosecond {
		t.Fatalf("async hold has TTL: %v", ttl)
	}
	snapshot.Prices["video"] = catalog.Price{Mode: "per_request", PerRequest: 999}
	snapshot.Groups["default"] = catalog.Group{Multiplier: 99}
	restarted := NewWorkflowSettler(s)
	actual, err := restarted.Finalize(ctx, workflowTestRequest(), reservation, native.Result{Status: "completed", Units: 6.5})
	if err != nil || actual != 152 {
		t.Fatalf("actual %d err %v", actual, err)
	}
	if bal, held := balance(t, rdb); bal != 848 || held != 0 {
		t.Fatalf("balance %d held %d", bal, held)
	}
	if ttl, _ := rdb.PTTL(ctx, keysFor(42, request.ID).done).Result(); ttl != -time.Nanosecond {
		t.Fatalf("async completion has TTL: %v", ttl)
	}
	if _, err := restarted.Finalize(ctx, workflowTestRequest(), reservation, native.Result{Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	if len(events(t, rdb)) != 1 {
		t.Fatal("duplicate reconciliation charged again")
	}
	snapshot.Prices["video"], snapshot.Groups["default"] = catalog.Price{Mode: "per_request", PerRequest: 101}, catalog.Group{Multiplier: 1.5}
	request = workflowTestRequest()
	request.ID = "failed-task"
	reservation, err = restarted.Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	actual, err = restarted.Finalize(ctx, request, reservation, native.Result{Status: "failed", Units: 8})
	if err != nil || actual != 0 {
		t.Fatalf("refund %d %v", actual, err)
	}
	if bal, held := balance(t, rdb); bal != 848 || held != 0 {
		t.Fatalf("failed task charged %d held %d", bal, held)
	}
}

func TestWorkflowFundingAndBudgetSurviveRestart(t *testing.T) {
	s, rdb, _, _ := setup(t, 1000)
	s.accounts = fundedResolver{sources: []int64{43}}
	s.snapshot = func() *catalog.Snapshot { return snap }
	for id, amount := range map[int64]int64{43: 100, 44: 500} {
		if err := rdb.HSet(ctx, BalanceKey(id), "balance", amount, "reserved", 0, "ver", 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	request := newReq("async-funded")
	request.Targets = []gateway.Target{{ChannelID: 3, CredentialID: 300}}
	request.Principal.BudgetLimited, request.Principal.BudgetAccountID = true, 44
	reservation, err := NewWorkflowSettler(s).Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	restoredRequest := *request
	restoredRequest.Reserve = nil
	restoredRequest.Principal.BudgetLimited, restoredRequest.Principal.BudgetAccountID = false, 0 // frozen funding, not live authorization
	actual, err := NewWorkflowSettler(s).Finalize(ctx, &restoredRequest, reservation, native.Result{Status: "completed", Usage: gateway.Usage{PromptTokens: 10, CompletionTokens: 120}})
	if err != nil || actual != 250 {
		t.Fatalf("actual %d err %v", actual, err)
	}
	for id, want := range map[int64]int64{42: 850, 43: 0, 44: 250} {
		if bal, _ := rdb.HGet(ctx, BalanceKey(id), "balance").Int64(); bal != want {
			t.Fatalf("account %d balance %d expected %d", id, bal, want)
		}
		if held, _ := rdb.HGet(ctx, BalanceKey(id), "reserved").Int64(); held != 0 {
			t.Fatalf("account %d retained %d", id, held)
		}
	}
}

func TestWorkflowSweeperRetainsExistingAndUnknownTasks(t *testing.T) {
	for _, state := range []string{"queued", "in_progress", "submitting", "submission_unknown", "completed", "failed", "lookup_failure", "missing"} {
		t.Run(state, func(t *testing.T) {
			s, rdb, l, c := setup(t, 1000)
			facts := &asyncLoader{loader: l, exists: state != "missing"}
			if state == "lookup_failure" {
				facts.err = errors.New("PG unavailable")
			}
			s.loader = facts
			request := newReq("sweep-async")
			request.Targets = []gateway.Target{{ChannelID: 3, CredentialID: 300}}
			if _, err := NewWorkflowSettler(s).Reserve(ctx, request); err != nil {
				t.Fatal(err)
			}
			c.ms.Add((s.cfg.ReservationExpiry + 48*time.Hour).Milliseconds())
			n, err := s.SweepExpired(ctx, 100)
			if state == "lookup_failure" {
				if err == nil {
					t.Fatal("lookup error swallowed")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			_, held := balance(t, rdb)
			if state == "missing" {
				if n != 1 || held != 0 {
					t.Fatalf("missing task retained n=%d held=%d", n, held)
				}
			} else if n != 0 || held != 208 {
				t.Fatalf("live/unknown task released n=%d held=%d", n, held)
			}
		})
	}
}
