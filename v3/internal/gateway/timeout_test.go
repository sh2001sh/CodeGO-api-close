package gateway_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
)

type buildTimeoutProvider struct{ openai.Provider }

func (buildTimeoutProvider) BuildRequest(ctx context.Context, _ *gateway.Request, _ gateway.Target) (*http.Request, error) {
	<-ctx.Done()
	return nil, errors.New("OAuth exchange failed")
}

func TestRequestBuilderTimeoutReportsTimeoutAndRefund(t *testing.T) {
	planner := &fakePlanner{targets: []gateway.Target{{Provider: "oauth"}}}
	settler := newSettler()
	g, err := gateway.New(gateway.Deps{Config: gateway.Config{HeaderTimeout: 10 * time.Millisecond},
		Authorizer: fakeAuth{}, Planner: planner, Settler: settler,
		Providers: map[string]gateway.Provider{"oauth": buildTimeoutProvider{}}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(streamBody))
	req.Header.Set("Authorization", "Bearer sk-test")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	out := <-settler.outcomes
	if w.Code != http.StatusGatewayTimeout || out.Terminal != gateway.TerminalTimeout || out.Charge {
		t.Fatalf("status=%d outcome=%+v", w.Code, out)
	}
}

// Regression: HTTP.Do timeouts used to refund correctly but report the wrong
// terminal, preventing timeout accounting and seven-terminal replay coverage.
func TestTimeoutBeforeOutput(t *testing.T) {
	for _, stream := range []bool{true, false} {
		t.Run(map[bool]string{true: "stream", false: "json"}[stream], func(t *testing.T) {
			cfg := gateway.Config{HeaderTimeout: 20 * time.Millisecond, RelayTimeout: 20 * time.Millisecond}
			h := newHarness(t, cfg, "slow_headers/t1000")
			body := streamBody
			if !stream {
				body = `{"model":"gpt-test","messages":[]}`
			}
			view := h.do(body)
			out := h.outcome()
			if view.status != http.StatusGatewayTimeout || out.Terminal != gateway.TerminalTimeout || out.Charge {
				t.Fatalf("status=%d terminal=%s charge=%v", view.status, out.Terminal, out.Charge)
			}
		})
	}
}

func TestTimeoutAfterOutputDoesNotRetry(t *testing.T) {
	h := newHarness(t, gateway.Config{RelayTimeout: 100 * time.Millisecond}, "complete/c30/i1000/t0", "complete/c3/i0/t0")
	view := h.do(streamBody)
	out := h.outcome()
	if view.status != http.StatusOK || contentEvents(view.data) != 1 || out.Terminal != gateway.TerminalTimeout || !out.Charge || !out.Usage.Estimated {
		t.Fatalf("view=%+v outcome=%+v", view, out)
	}
	if len(h.planner.results()) != 1 || !strings.Contains(view.data[len(view.data)-1], "upstream_timeout") {
		t.Fatalf("timeout was retried or hidden: %v", view.data)
	}
}

// Regression: heartbeat used to commit a 200 before the first upstream event,
// so a delayed invalid request could only be reported as an SSE error.
func TestHeartbeatKeepsFirstEventGate(t *testing.T) {
	h := newHarness(t, gateway.Config{Heartbeat: time.Millisecond}, "invalid_request/t40")
	view := h.do(streamBody)
	if view.status != http.StatusBadRequest || !strings.Contains(view.body, "missing_messages") {
		t.Fatalf("heartbeat committed headers before failure: %+v", view)
	}
	_ = h.outcome()
}
