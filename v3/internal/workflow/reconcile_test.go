package workflow_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestWorkflowReconcileUsesActualUsageAndFinalizesOnce(t *testing.T) {
	f := newAPIFixture(t)
	task := f.submit(t)
	count, err := f.handler.Reconcile(context.Background(), 10)
	stored := f.repo.one(t)
	if err != nil || count != 1 || stored.Status != "completed" || stored.CostState != "settled" || stored.ActualCredits != 37 {
		t.Fatalf("completion = count %d err %v task %+v", count, err, stored)
	}
	if len(f.settler.calls) != 1 || f.settler.debits != 1 {
		t.Fatal("completion not finalized exactly once")
	}
	call := f.settler.calls[0]
	if call.id != task.ID || call.result.Units != 8 || call.result.Usage.PromptTokens != 3 || call.result.Usage.CompletionTokens != 17 || string(call.reservation.Data) != string(task.Reservation.Data) {
		t.Fatalf("actual usage or durable reservation lost: %+v", call)
	}
	if count, err = f.handler.Reconcile(context.Background(), 10); count != 0 || err != nil || len(f.settler.calls) != 1 || f.provider.polls.Load() != 1 {
		t.Fatalf("repeat reconciled settled task: count=%d err=%v", count, err)
	}
}

func TestWorkflowConcurrentReconcileCannotDoubleDebit(t *testing.T) {
	f := newAPIFixture(t)
	f.submit(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f.provider.pollFn = func(ctx context.Context, _ gateway.Target, task native.Task) (native.Result, error) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
		case <-ctx.Done():
			return native.Result{}, ctx.Err()
		}
		return native.Result{ID: task.UpstreamID, Status: "completed", Units: 4}, nil
	}
	errCh := make(chan error, 1)
	go func() { _, err := f.handler.Reconcile(context.Background(), 10); errCh <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("reconciliation never reached provider poll")
	}
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if n, err := f.handler.Reconcile(context.Background(), 10); err != nil || n != 0 {
				t.Errorf("leased task reconciled concurrently: %d %v", n, err)
			}
		}()
	}
	workers.Wait()
	close(release)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if f.provider.polls.Load() != 1 || len(f.settler.calls) != 1 || f.settler.debits != 1 || f.repo.one(t).CostState != "settled" {
		t.Fatal("concurrent reconciliation double-polled or double-debited")
	}
}

func TestWorkflowSettlementFailureRetriesStableTaskIdentity(t *testing.T) {
	f := newAPIFixture(t)
	task := f.submit(t)
	f.settler.failures = 1
	if n, err := f.handler.Reconcile(context.Background(), 10); n != 0 || err == nil {
		t.Fatalf("settlement failure swallowed: %d %v", n, err)
	}
	stored := f.repo.one(t)
	if stored.Status != "completed" || stored.CostState != "reserved" || stored.LeaseID != "" || f.settler.debits != 0 {
		t.Fatalf("failed settlement not retryable: %+v", stored)
	}
	if n, err := f.handler.Reconcile(context.Background(), 10); n != 1 || err != nil {
		t.Fatalf("settlement retry: %d %v", n, err)
	}
	if len(f.settler.calls) != 2 || f.settler.calls[0].id != task.ID || f.settler.calls[1].id != task.ID || f.settler.debits != 1 || f.provider.polls.Load() != 1 {
		t.Fatal("settlement retry changed operation ID, repeated polling or double-debited")
	}
}

func TestWorkflowSaveFailureRetriesIdempotentSettlementAfterLeaseExpiry(t *testing.T) {
	f := newAPIFixture(t)
	task := f.submit(t)
	f.repo.saveFailures = 1
	if n, err := f.handler.Reconcile(context.Background(), 10); n != 0 || err == nil {
		t.Fatalf("save failure swallowed: %d %v", n, err)
	}
	stored := f.repo.one(t)
	if stored.CostState != "reserved" || stored.LeaseID == "" || f.settler.debits != 1 {
		t.Fatalf("save failure lost durable hold or lease: %+v", stored)
	}
	if n, err := f.handler.Reconcile(context.Background(), 10); n != 0 || err != nil {
		t.Fatalf("unexpired failed save retried: %d %v", n, err)
	}
	f.repo.expireLeases()
	if n, err := f.handler.Reconcile(context.Background(), 10); n != 1 || err != nil {
		t.Fatalf("post-expiry retry: %d %v", n, err)
	}
	if len(f.settler.calls) != 2 || f.settler.calls[0].id != task.ID || f.settler.calls[1].id != task.ID || f.settler.debits != 1 || f.repo.one(t).ActualCredits != 37 {
		t.Fatal("save retry changed stable settlement ID or double-debited")
	}
}

func TestWorkflowProviderFailureRefundsOnce(t *testing.T) {
	f := newAPIFixture(t)
	task := f.submit(t)
	f.provider.pollFn = func(_ context.Context, _ gateway.Target, task native.Task) (native.Result, error) {
		return native.Result{ID: task.UpstreamID, Status: "failed", Error: "render failed"}, nil
	}
	if n, err := f.handler.Reconcile(context.Background(), 10); n != 1 || err != nil {
		t.Fatalf("failure refund: %d %v", n, err)
	}
	stored := f.repo.one(t)
	if stored.Status != "failed" || stored.CostState != "refunded" || stored.ActualCredits != 0 || len(f.settler.calls) != 1 || f.settler.calls[0].id != task.ID || f.settler.debits != 0 {
		t.Fatalf("failed task charged or not refunded: %+v", stored)
	}
	if n, err := f.handler.Reconcile(context.Background(), 10); n != 0 || err != nil || len(f.settler.calls) != 1 {
		t.Fatal("failed task refunded again")
	}
}

func TestWorkflowInvalidPollPreservesHoldAndReleasesLease(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result native.Result
		err    error
	}{
		{"transport", native.Result{}, errors.New("network unavailable")},
		{"other_task", native.Result{ID: "someone-elses-task", Status: "completed"}, nil},
		{"invalid_state", native.Result{ID: "upstream-1", Status: "surprise"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAPIFixture(t)
			f.submit(t)
			f.provider.pollFn = func(context.Context, gateway.Target, native.Task) (native.Result, error) { return tc.result, tc.err }
			if n, err := f.handler.Reconcile(context.Background(), 10); n != 0 || err == nil {
				t.Fatalf("invalid poll swallowed: %d %v", n, err)
			}
			stored := f.repo.one(t)
			if stored.Status != "queued" || stored.CostState != "reserved" || stored.LeaseID != "" || len(f.settler.calls) != 0 {
				t.Fatalf("invalid poll changed task or funds: %+v", stored)
			}
			f.provider.pollFn = nil
			if n, err := f.handler.Reconcile(context.Background(), 10); n != 1 || err != nil {
				t.Fatalf("recoverable poll cannot retry: %d %v", n, err)
			}
		})
	}
}

func TestWorkflowCredentialReferenceCannotSwitchOnRestart(t *testing.T) {
	f := newAPIFixture(t, func(c *workflow.Config) {
		c.ResolveTarget = func(context.Context, int64, int64) (gateway.Target, error) {
			return gateway.Target{ChannelID: 4, CredentialID: 999, Provider: "openai_video"}, nil
		}
	})
	f.submit(t)
	if _, err := f.handler.Reconcile(context.Background(), 10); err == nil || !strings.Contains(err.Error(), "reference changed") {
		t.Fatalf("changed credential accepted: %v", err)
	}
	if f.provider.polls.Load() != 0 || len(f.settler.calls) != 0 || f.repo.one(t).CostState != "reserved" {
		t.Fatal("changed credential reached upstream or billing")
	}
}

func TestWorkflowSubmissionRefundFailureIsDurablyRetryable(t *testing.T) {
	f := newAPIFixture(t)
	f.settler.failures = 1
	f.provider.submitFn = func(context.Context, gateway.Target, native.Submit) (native.Result, error) {
		return native.Result{}, &native.Rejected{Status: 422}
	}
	w := f.request("POST", "/v1/videos", "owner", `{"model":"video"}`, "")
	stored := f.repo.one(t)
	if w.Code != 502 || stored.Status != "failed" || stored.CostState != "reserved" || stored.LeaseID != "" {
		t.Fatalf("failed refund not durable: %d %+v", w.Code, stored)
	}
	if n, err := f.handler.Reconcile(context.Background(), 10); n != 1 || err != nil {
		t.Fatalf("refund retry = %d %v", n, err)
	}
	if len(f.settler.calls) != 2 || f.settler.calls[0].id != stored.ID || f.settler.calls[1].id != stored.ID || f.repo.one(t).CostState != "refunded" || f.provider.polls.Load() != 0 || f.settler.debits != 0 {
		t.Fatal("refund retry lost task identity, polled rejected task or debited")
	}
}

func TestWorkflowPollingRetainsPersistedProviderAndUpstreamModel(t *testing.T) {
	f := newAPIFixture(t)
	task := f.submit(t)
	f.provider.pollFn = func(_ context.Context, target gateway.Target, in native.Task) (native.Result, error) {
		if target.ChannelID != task.ChannelID || target.CredentialID != task.CredentialID || target.UpstreamModel != task.UpstreamModel || in.UpstreamID != task.UpstreamID || in.Model != task.UpstreamModel {
			t.Fatal("poll did not reconstruct persisted native target")
		}
		return native.Result{ID: in.UpstreamID, Status: "in_progress"}, nil
	}
	if n, err := f.handler.Reconcile(context.Background(), 0); n != 1 || err != nil {
		t.Fatalf("pending poll = %d %v", n, err)
	}
	if stored := f.repo.one(t); stored.Status != "in_progress" || stored.CostState != "reserved" || stored.LeaseID != "" || len(f.settler.calls) != 0 {
		t.Fatalf("pending task prematurely charged: %+v", stored)
	}
}
