//go:build pgintegration

package billing

import (
	"fmt"
	"sync"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestWorkflowConcurrentReconciliationCommitsExactlyOnce(t *testing.T) {
	s, rdb, _, _ := setup(t, 1000)
	request := newReq("concurrent-async")
	request.Targets = []gateway.Target{{ChannelID: 3, CredentialID: 300}}
	adapter := NewWorkflowSettler(s)
	reservation, err := adapter.Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	result := native.Result{Status: "completed", Usage: gateway.Usage{PromptTokens: 10, CompletionTokens: 120}}
	start := make(chan struct{})
	failures := make(chan error, 24)
	var wait sync.WaitGroup
	for range 24 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			actual, err := adapter.Finalize(ctx, request, reservation, result)
			if err != nil || actual != 250 {
				failures <- fmt.Errorf("actual=%d error=%v", actual, err)
			}
		}()
	}
	close(start)
	wait.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	if bal, held := balance(t, rdb); bal != 750 || held != 0 || len(events(t, rdb)) != 1 {
		t.Fatalf("money=%d/%d events=%d", bal, held, len(events(t, rdb)))
	}
	// Repeated provider observations must return the money already committed,
	// even if a later poll's token accounting differs.
	result.Usage.CompletionTokens = 999
	if actual, err := adapter.Finalize(ctx, request, reservation, result); err != nil || actual != 250 {
		t.Fatalf("retry actual=%d error=%v", actual, err)
	}
	if _, err := adapter.Finalize(ctx, request, reservation, native.Result{Status: "failed"}); err == nil {
		t.Fatal("charged task relabeled refunded")
	}
}
