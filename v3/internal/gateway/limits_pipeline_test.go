package gateway_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/bench/mockupstream"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/limits"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
)

type recordingLeases struct {
	mu        sync.Mutex
	err       error
	busyFirst bool
	acquired  []int64
	released  []int64
}

func (l *recordingLeases) Acquire(_ context.Context, _ *gateway.Request, target gateway.Target) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.acquired = append(l.acquired, target.ChannelID)
	if l.busyFirst && target.ChannelID == 1 {
		return gateway.ErrTargetBusy
	}
	return l.err
}

func (l *recordingLeases) Release(ctx context.Context, _ *gateway.Request, target gateway.Target) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	l.released = append(l.released, target.ChannelID)
	return nil
}

func limitHarness(t *testing.T, lease gateway.LeaseController, auth gateway.Authorizer, failures gateway.AuthFailureController) (*harness, http.Handler) {
	t.Helper()
	up := httptest.NewServer(mockupstream.Handler())
	t.Cleanup(up.Close)
	p := &fakePlanner{targets: []gateway.Target{
		{ChannelID: 1, Provider: openai.ID, BaseURL: up.URL + "/m/error_before"},
		{ChannelID: 2, Provider: openai.ID, BaseURL: up.URL + "/m/complete"},
	}}
	s := newSettler()
	g, err := gateway.New(gateway.Deps{Authorizer: auth, Planner: p, Settler: s,
		Limits: lease, AuthFailures: failures, Providers: map[string]gateway.Provider{openai.ID: openai.Provider{}}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &harness{t: t, gw: server, planner: p, settler: s}, mux
}

// Regression: busy credentials must fail over without cooling them down, and
// every successfully acquired attempt releases even when the upstream fails.
func TestConcurrencyLeaseFailoverAndRelease(t *testing.T) {
	for _, busy := range []bool{true, false} {
		l := &recordingLeases{busyFirst: busy}
		h, _ := limitHarness(t, l, fakeAuth{}, nil)
		if v := h.do(streamBody); v.status != http.StatusOK {
			t.Fatalf("status=%d body=%s", v.status, v.body)
		}
		if out := h.outcome(); out.Terminal != gateway.TerminalCompleted {
			t.Fatalf("terminal=%s", out.Terminal)
		}
		l.mu.Lock()
		if len(l.acquired) != 2 {
			t.Errorf("acquired attempts = %v", l.acquired)
		}
		want := 2
		if busy {
			want = 1
		}
		if len(l.released) != want {
			t.Errorf("released attempts = %v", l.released)
		}
		l.mu.Unlock()
		if busy && h.planner.results()[0].Scope != gateway.ScopeNone {
			t.Fatal("busy target was cooled down")
		}
	}
}

func TestUserLimitAndUnavailableStopBeforeUpstream(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{gateway.ErrRateLimited, http.StatusTooManyRequests},
		{gateway.ErrLimitsUnavailable, http.StatusServiceUnavailable},
	} {
		l := &recordingLeases{err: tc.err}
		h, _ := limitHarness(t, l, fakeAuth{}, nil)
		if got := h.do(streamBody).status; got != tc.status {
			t.Fatalf("status %d, want %d", got, tc.status)
		}
		if out := h.outcome(); out.Delivered {
			t.Fatal("limited request delivered upstream content")
		}
		l.mu.Lock()
		if len(l.acquired) != 1 || len(l.released) != 0 {
			t.Errorf("acquired %v released %v", l.acquired, l.released)
		}
		l.mu.Unlock()
	}
}

type countingAuth struct{ calls atomic.Int32 }

func (a *countingAuth) Authorize(ctx context.Context, key string) (gateway.Principal, error) {
	a.calls.Add(1)
	return fakeAuth{}.Authorize(ctx, key)
}

func TestFailedAuthenticationStopsRepeatedLookup(t *testing.T) {
	a := &countingAuth{}
	_, handler := limitHarness(t, nil, a, limits.NewAuthFailures(limits.FailureConfig{MaxFailures: 2}))
	for i, want := range []int{401, 401, 429} {
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(streamBody))
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("Authorization", "Bearer bad")
		// Spoofed proxy headers must not make the blocked address usable.
		r.Header.Set("X-Forwarded-For", "192.0.2."+string(rune('2'+i)))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		body, _ := io.ReadAll(w.Result().Body)
		if w.Code != want {
			t.Fatalf("request %d status %d want %d: %s", i, w.Code, want, body)
		}
	}
	if a.calls.Load() != 2 {
		t.Fatalf("blocked requests still call authorizer: %d", a.calls.Load())
	}
}
