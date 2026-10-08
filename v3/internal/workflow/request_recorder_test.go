package workflow_test

import (
	"context"
	"sync"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/audit"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

type taskRecords struct {
	mu   sync.Mutex
	rows []audit.RequestRecord
}

func (s *taskRecords) RecordRequest(req *gateway.Request, out gateway.Outcome, settled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, audit.ProjectRequestRecord(req, out, settled))
}

func TestWorkflowMetadataWaitsForRealTerminalSettlement(t *testing.T) {
	ctx := context.Background()
	r := &taskRecords{}
	f := newAPIFixture(t, func(c *workflow.Config) { c.Requests = r })
	task := f.submit(t)
	if len(r.rows) != 0 {
		t.Fatal("queued submit fabricated finished success")
	}
	f.provider.pollFn = func(_ context.Context, _ gateway.Target, in native.Task) (native.Result, error) {
		return native.Result{ID: in.UpstreamID, Status: "in_progress"}, nil
	}
	if n, err := f.handler.Reconcile(ctx, 10); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	if len(r.rows) != 0 {
		t.Fatal("in-progress poll fabricated finished success")
	}
	f.provider.pollFn = nil
	f.settler.failures = 1
	if _, err := f.handler.Reconcile(ctx, 10); err == nil {
		t.Fatal("expected actual settlement failure")
	}
	if len(r.rows) != 0 {
		t.Fatal("unaccepted settlement produced finished metadata")
	}
	if n, err := f.handler.Reconcile(ctx, 10); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	if len(r.rows) != 1 || r.rows[0].RequestID != task.ID || r.rows[0].Status != "success" || r.rows[0].Amount != 37 || r.rows[0].PromptTokens != 3 || r.rows[0].CompletionTokens != 17 || r.rows[0].ChannelID != 4 || r.rows[0].UserID != 11 || r.rows[0].KeyID != 111 {
		t.Fatalf("wrong frozen terminal metadata: %+v", r.rows)
	}
	if _, err := f.handler.Reconcile(ctx, 10); err != nil || len(r.rows) != 1 {
		t.Fatal("repeat emitted another logical terminal", err)
	}
}

func TestWorkflowMetadataRecordsFailureOnlyAfterRefund(t *testing.T) {
	r := &taskRecords{}
	f := newAPIFixture(t, func(c *workflow.Config) { c.Requests = r })
	task := f.submit(t)
	f.provider.pollFn = func(_ context.Context, _ gateway.Target, in native.Task) (native.Result, error) {
		return native.Result{ID: in.UpstreamID, Status: "failed", Error: "private-upstream-body"}, nil
	}
	if n, err := f.handler.Reconcile(context.Background(), 10); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	if len(r.rows) != 1 || r.rows[0].RequestID != task.ID || r.rows[0].Status != "failed" || r.rows[0].Amount != 0 || r.rows[0].Billable || !r.rows[0].Counted || r.rows[0].ErrorCode != "task_failed" || f.repo.one(t).CostState != "refunded" {
		t.Fatalf("failure/refund metadata differs: %+v", r.rows)
	}
}
