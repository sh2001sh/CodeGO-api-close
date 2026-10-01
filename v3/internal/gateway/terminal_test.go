package gateway_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

var fast = gateway.Config{HeaderTimeout: 2 * time.Second, DrainTimeout: 5 * time.Second}

// Each case is one row of the plan §4 decision table, observed end to end.
func TestTerminalStates(t *testing.T) {
	cases := []struct {
		name       string
		scenarios  []string
		status     int
		deltas     int
		lastData   string
		terminal   gateway.Terminal
		charge     bool
		estimated  bool
		completion int64
		channel    int64
	}{
		{"completed", []string{"complete/c4/i0/t0"}, 200, 4, "[DONE]", gateway.TerminalCompleted, true, false, 4, 1},
		{"completed without usage", []string{"complete_no_usage/c4/i0/t0"}, 200, 4, "[DONE]", gateway.TerminalCompletedNoUsage, true, true, 0, 1},
		{"error after output", []string{"error_after/c4/i0/t0", "complete/c4/i0/t0"}, 200, 2, "error", gateway.TerminalUpstreamErrorAfterOutput, true, true, 0, 1},
		{"truncated after output", []string{"truncated/c4/i0/t0", "complete/c4/i0/t0"}, 200, 2, "error", gateway.TerminalUpstreamErrorAfterOutput, true, true, 0, 1},
		{"all candidates fail first", []string{"error_before/t0", "error_before/t0"}, 429, 0, "", gateway.TerminalUpstreamErrorBeforeOutput, false, false, 0, 2},
		{"empty stream everywhere", []string{"empty/t0", "empty/t0"}, 502, 0, "", gateway.TerminalEmptyStream, false, false, 0, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, fast, tc.scenarios...)
			view := h.do(streamBody)
			out := h.outcome()

			if view.status != tc.status {
				t.Fatalf("status = %d; want %d (body %q)", view.status, tc.status, view.body)
			}
			if got := contentEvents(view.data); got != tc.deltas {
				t.Errorf("client saw %d content events; want %d", got, tc.deltas)
			}
			if tc.lastData != "" && (len(view.data) == 0 || !strings.Contains(view.data[len(view.data)-1], tc.lastData)) {
				t.Errorf("last event = %v; want it to contain %q", view.data, tc.lastData)
			}
			if out.Terminal != tc.terminal {
				t.Errorf("terminal = %s; want %s", out.Terminal, tc.terminal)
			}
			if out.Charge != tc.charge || out.Usage.Estimated != tc.estimated {
				t.Errorf("charge=%v estimated=%v; want %v %v", out.Charge, out.Usage.Estimated, tc.charge, tc.estimated)
			}
			if tc.completion > 0 && out.Usage.CompletionTokens != tc.completion {
				t.Errorf("completion tokens = %d; want %d", out.Usage.CompletionTokens, tc.completion)
			}
			if out.Target == nil || out.Target.ChannelID != tc.channel {
				t.Errorf("final channel = %+v; want %d", out.Target, tc.channel)
			}
		})
	}
}

// Failures before the first content event must fail over without the client
// noticing: same stream, no error event, billed against the healthy channel.
func TestInvisibleFailover(t *testing.T) {
	for _, first := range []string{"error_before/t0", "error_in_stream/t0", "empty/t0", "slow_headers/t3000"} {
		t.Run(first, func(t *testing.T) {
			cfg := fast
			cfg.HeaderTimeout = 200 * time.Millisecond
			h := newHarness(t, cfg, first, "complete/c3/i0/t0")
			view := h.do(streamBody)
			out := h.outcome()

			if view.status != http.StatusOK || contentEvents(view.data) != 3 || view.data[len(view.data)-1] != "[DONE]" {
				t.Fatalf("client view = %d %v", view.status, view.data)
			}
			for _, d := range view.data {
				if strings.Contains(d, `"error"`) {
					t.Fatalf("client saw the failed attempt: %s", d)
				}
			}
			if out.Terminal != gateway.TerminalCompleted || out.Target.ChannelID != 2 {
				t.Fatalf("outcome = %s on channel %d", out.Terminal, out.Target.ChannelID)
			}
			reports := h.planner.results()
			if len(reports) != 2 || reports[0].OK || !reports[0].Retryable || reports[0].Scope != gateway.ScopeCredential || !reports[1].OK {
				t.Fatalf("planner feedback = %+v", reports)
			}
			if first == "error_before/t0" && reports[0].RetryAfter != time.Second {
				t.Fatalf("Retry-After not propagated: %v", reports[0].RetryAfter)
			}
		})
	}
}

// A request-scoped 400 is passed through verbatim and never retried.
func TestInvalidRequestIsNotRetried(t *testing.T) {
	h := newHarness(t, fast, "invalid_request/t0", "complete/t0")
	view := h.do(streamBody)
	out := h.outcome()
	if view.status != http.StatusBadRequest || !strings.Contains(view.body, "missing_messages") {
		t.Fatalf("client view = %d %q", view.status, view.body)
	}
	if n := len(h.planner.results()); n != 1 {
		t.Fatalf("attempts = %d; want 1", n)
	}
	if out.Terminal != gateway.TerminalUpstreamErrorBeforeOutput || out.Charge {
		t.Fatalf("outcome = %s charge=%v", out.Terminal, out.Charge)
	}
}

// When the client leaves mid-stream the gateway keeps draining the upstream
// and bills the usage the upstream reports, not an estimate.
func TestClientCancelDrainsForUsage(t *testing.T) {
	h := newHarness(t, fast, "complete/c30/i20/t0")
	ctx, cancel := context.WithCancel(context.Background())
	resp := h.post(ctx, streamBody)
	r := sse.NewReader(resp.Body, 0)
	for i := 0; i < 3; i++ {
		if _, err := r.Next(); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	_ = resp.Body.Close()

	out := h.outcome()
	if out.Terminal != gateway.TerminalClientCanceled || !out.Delivered {
		t.Fatalf("outcome = %s delivered=%v", out.Terminal, out.Delivered)
	}
	if !out.Charge || out.Usage.Estimated || out.Usage.CompletionTokens != 30 {
		t.Fatalf("usage = %+v charge=%v; want drained upstream usage of 30", out.Usage, out.Charge)
	}
}

func TestNonStreaming(t *testing.T) {
	h := newHarness(t, fast, "error_before/t0", "complete/c5/t0")
	view := h.do(`{"model":"gpt-test","messages":[]}`)
	out := h.outcome()
	if view.status != http.StatusOK || !strings.Contains(view.body, `"object":"chat.completion"`) {
		t.Fatalf("client view = %d %q", view.status, view.body)
	}
	if out.Terminal != gateway.TerminalCompleted || out.Usage.CompletionTokens != 5 || out.Target.ChannelID != 2 {
		t.Fatalf("outcome = %s usage=%+v", out.Terminal, out.Usage)
	}
}

func TestAdmissionErrors(t *testing.T) {
	h := newHarness(t, fast, "complete/t0")
	req, _ := http.NewRequest(http.MethodPost, h.gw.URL+"/v1/chat/completions", strings.NewReader(streamBody))
	req.Header.Set("Authorization", "Bearer sk-wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad key status = %d", resp.StatusCode)
	}

	if v := h.do(`{"stream":true}`); v.status != http.StatusBadRequest {
		t.Fatalf("missing model status = %d", v.status)
	}

	h.settler.insufficient = true
	if v := h.do(streamBody); v.status != http.StatusPaymentRequired {
		t.Fatalf("insufficient credits status = %d", v.status)
	}
	if n := len(h.planner.results()); n != 0 {
		t.Fatalf("upstream was called %d times on admission errors", n)
	}
}
