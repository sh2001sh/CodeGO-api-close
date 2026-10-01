package live

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestBackgroundJobsRunCapsWorkersBeforeClaimAndAdmitsAfterCompletion(t *testing.T) {
	h, _, wrapped, repo, _ := backgroundJobsFixture(t, "https://unused.invalid", "openai")
	var active, peak atomic.Int64
	started := make(chan struct{}, 64)
	release := make(chan struct{}, 1)
	h.cfg.Clients = func(context.Context, gateway.Target) (*http.Client, error) {
		return &http.Client{Transport: backgroundOverrideTransport(func(req *http.Request) (*http.Response, error) {
			current := active.Add(1)
			defer active.Add(-1)
			for observed := peak.Load(); current > observed; observed = peak.Load() {
				if peak.CompareAndSwap(observed, current) {
					break
				}
			}
			started <- struct{}{}
			select {
			case <-release:
				return backgroundOverrideResponse(req, 200, backgroundCompletedSnapshot), nil
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		})}, nil
	}
	for range backgroundConcurrency + 1 {
		createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","background":true}`)
		<-repo.created
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Run(ctx) }()
	awaitStarted := func() {
		t.Helper()
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("background worker was not admitted")
		}
	}
	for range backgroundConcurrency {
		awaitStarted()
	}
	// An overlapping dispatcher must return promptly without claiming the
	// waiting job while the existing batch owns all process slots.
	reconciled := make(chan error, 1)
	go func() { reconciled <- h.Reconcile(ctx, 1000) }()
	select {
	case err := <-reconciled:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("dispatcher waited for a slot with an acquired lease")
	}
	repo.mu.Lock()
	queued := 0
	for _, job := range repo.jobs {
		if job.Status == "queued" {
			queued++
			if job.LeaseID != "" {
				t.Error("waiting job acquired a durable lease before process admission")
			}
		}
	}
	repo.mu.Unlock()
	if queued != 1 || active.Load() != backgroundConcurrency || peak.Load() != backgroundConcurrency {
		t.Fatalf("concurrency bound queued=%d active=%d peak=%d", queued, active.Load(), peak.Load())
	}
	release <- struct{}{}
	awaitStarted()
	if peak.Load() > backgroundConcurrency {
		t.Fatalf("replacement exceeded worker bound: %d", peak.Load())
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run shutdown error=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run cancellation failed to stop active workers promptly")
	}
	if active.Load() != 0 {
		t.Fatalf("Run returned with active upstream workers: %d", active.Load())
	}
}
