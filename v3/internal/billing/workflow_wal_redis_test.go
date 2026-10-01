//go:build pgintegration

package billing

import (
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestWorkflowWALDoesNotMarkTaskBilledBeforeReplay(t *testing.T) {
	s, direct, proxy := outageSetup(t, 1000)
	request := reqFor(7, "workflow-outage")
	request.Targets = []gateway.Target{{ChannelID: 3, CredentialID: 300}}
	reservation, err := NewWorkflowSettler(s).Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	proxy.cut()
	result := native.Result{Status: "completed", Usage: gateway.Usage{PromptTokens: 10, CompletionTokens: 20}}
	if _, err := NewWorkflowSettler(s).Finalize(ctx, request, reservation, result); !errors.Is(err, gateway.ErrBillingUnavailable) {
		t.Fatalf("unconfirmed WAL settled task: %v", err)
	}
	if s.wal.pending.Load() != 1 {
		t.Fatalf("durable settlement missing from WAL: %d", s.wal.pending.Load())
	}
	if bal, held := hotBalance(t, direct, 7); bal != 1000 || held != 208 {
		t.Fatalf("outage money=%d/%d", bal, held)
	}
	other := reqFor(7, "another-task")
	other.Targets = request.Targets
	if _, err := NewWorkflowSettler(s).Reserve(ctx, other); err == nil {
		t.Fatal("durable task admitted from local-only allowance")
	}
	proxy.restore()
	if err := s.Replay(ctx); err != nil {
		t.Fatal(err)
	}
	if bal, held := hotBalance(t, direct, 7); bal != 950 || held != 0 {
		t.Fatalf("replayed money=%d/%d", bal, held)
	}
	actual, err := NewWorkflowSettler(s).Finalize(ctx, request, reservation, result)
	if err != nil || actual != 50 {
		t.Fatalf("confirmed actual=%d err=%v", actual, err)
	}
	if len(events(t, direct)) != 1 {
		t.Fatal("retry duplicated WAL charge")
	}
}
