package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func runtimeLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestLoopExitRejectsRequestsAndReadiness(t *testing.T) {
	runtime := newProdRuntime(context.Background(), runtimeLogger())
	t.Cleanup(runtime.Close)
	runtime.ready.Store(true)
	handler := runtime.Wrap(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := func(path string) int {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w.Code
	}
	if got := request("/readyz"); got != 204 {
		t.Fatalf("ready runtime status=%d", got)
	}
	runtime.start("subscription", func(context.Context) error { return errors.New("lost subscription") })
	select {
	case <-runtime.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("early loop exit did not disable admission")
	}
	for _, path := range []string{"/readyz", "/v1/chat/completions", "/v1/files", "/v1/realtime"} {
		if got := request(path); got != 503 {
			t.Errorf("failed runtime path=%s status=%d", path, got)
		}
	}
	if got := request("/metrics"); got != 204 {
		t.Fatalf("failed runtime metrics status=%d", got)
	}
}

func TestSignalStopsAdmissionAndCleanupWaitsForSettlement(t *testing.T) {
	parent, signal := context.WithCancel(context.Background())
	runtime := newProdRuntime(parent, runtimeLogger())
	t.Cleanup(runtime.Close)
	runtime.ready.Store(true)
	entered, canceled, settled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var settleOnce sync.Once
	finishSettlement := func() { settleOnce.Do(func() { close(settled) }) }
	t.Cleanup(finishSettlement)
	closed := make(chan struct{})
	runtime.close = append(runtime.close, func() { close(closed) })
	handler := runtime.Wrap(parent, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(canceled)
		<-settled // simulates detached billing after a hijacked/client stream closes
		w.WriteHeader(http.StatusNoContent)
	}))
	finished := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/realtime", nil))
		close(finished)
	}()
	<-entered
	signal()
	if runtime.ctx.Err() != nil {
		t.Fatal("signal prematurely stopped settlement infrastructure")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/v1/chat/completions", nil))
	if w.Code != 503 {
		t.Fatalf("request admitted during shutdown: %d", w.Code)
	}
	cleanup := make(chan struct{})
	go func() { runtime.Close(); close(cleanup) }()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not cancel live request")
	}
	select {
	case <-closed:
		t.Fatal("dependencies closed before detached settlement")
	default:
	}
	finishSettlement()
	select {
	case <-cleanup:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not finish after settlement")
	}
	<-finished
	<-closed
}

func TestStartupCleanupJoinsLoopsBeforeResources(t *testing.T) {
	runtime := newProdRuntime(context.Background(), runtimeLogger())
	stopped := make(chan struct{})
	runtime.start("startup", func(ctx context.Context) error {
		<-ctx.Done()
		close(stopped)
		return nil
	})
	runtime.close = append(runtime.close, func() {
		select {
		case <-stopped:
		default:
			t.Error("dependency closed before background loop stopped")
		}
	})
	runtime.Close()
	runtime.Close()
}
