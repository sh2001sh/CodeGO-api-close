package gateway_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

type metricAttemptSettler struct {
	*fakeSettler
	attempts chan []gateway.Attempt
}

func (s *metricAttemptSettler) Finalize(ctx context.Context, req *gateway.Request, out gateway.Outcome) error {
	s.attempts <- append([]gateway.Attempt(nil), req.Attempts...)
	return s.fakeSettler.Finalize(ctx, req, out)
}

func attemptMetricHarness(t *testing.T, upstream string, candidates int) (*harness, <-chan []gateway.Attempt) {
	t.Helper()
	planner := &fakePlanner{}
	for i := range candidates {
		planner.targets = append(planner.targets, gateway.Target{ChannelID: int64(i + 1), Provider: openai.ID, BaseURL: upstream})
	}
	settler := &metricAttemptSettler{fakeSettler: newSettler(), attempts: make(chan []gateway.Attempt, 1)}
	g, err := gateway.New(gateway.Deps{Authorizer: fakeAuth{}, Planner: planner, Settler: settler,
		Providers: map[string]gateway.Provider{openai.ID: openai.Provider{}}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &harness{t: t, gw: server, planner: planner, settler: settler.fakeSettler}, settler.attempts
}

func metricEvent(w http.ResponseWriter, data string) {
	_, _ = io.WriteString(w, "data: "+data+"\n\n")
	w.(http.Flusher).Flush()
}

func metricWait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func TestAttemptMetricsFirstOutputIsSeparateFromFullRelay(t *testing.T) {
	const firstDelay, trailingDelay = 35 * time.Millisecond, 90 * time.Millisecond
	for _, retry := range []bool{false, true} {
		t.Run(map[bool]string{false: "one_attempt", true: "after_http_503"}[retry], func(t *testing.T) {
			firstObserved := make(chan struct{})
			var calls atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 && retry {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				// Usage is visible to accounting immediately; lifecycle metadata
				// remains gated until the later genuine content event arrives.
				metricEvent(w, `{"choices":[],"usage":{"prompt_tokens":20,"completion_tokens":0,"prompt_tokens_details":{"cached_tokens":5}}}`)
				metricEvent(w, `{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`)
				if !metricWait(r.Context(), firstDelay) {
					return
				}
				metricEvent(w, `{"choices":[{"index":0,"delta":{"content":"delayed"}}]}`)
				select {
				case <-firstObserved:
				case <-r.Context().Done():
					return
				}
				// This tail starts only after the client has read an event. A
				// full-exchange duration cannot satisfy the TTFT separation.
				if !metricWait(r.Context(), trailingDelay) {
					return
				}
				metricEvent(w, `{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":75}}}`)
				metricEvent(w, `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
				metricEvent(w, "[DONE]")
			}))
			t.Cleanup(upstream.Close)
			candidates := 1
			if retry {
				candidates = 2
			}
			h, saved := attemptMetricHarness(t, upstream.URL, candidates)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			response := h.post(ctx, streamBody)
			defer func() { _ = response.Body.Close() }()
			reader := sse.NewReader(response.Body, 0)
			first, err := reader.Next()
			close(firstObserved) // also releases the fixture on an assertion failure
			if err != nil || response.StatusCode != 200 || string(first.Data) == "[DONE]" {
				t.Fatalf("no real first output: status=%d event=%+v err=%v", response.StatusCode, first, err)
			}
			for {
				_, err = reader.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			out := h.outcome()
			attempts, reports := <-saved, h.planner.results()
			if len(reports) != candidates || len(attempts) != candidates || calls.Load() != int64(candidates) {
				t.Fatalf("actual attempt reports lost: attempts=%+v reports=%+v calls=%d", attempts, reports, calls.Load())
			}
			last := reports[candidates-1]
			full := attempts[candidates-1].Duration
			if !last.OK || last.TTFT < firstDelay || full-last.TTFT < trailingDelay*3/4 || last.PromptTokens != 100 || last.CachedTokens != 75 {
				t.Fatalf("report used early metadata/full relay/fabricated tokens: report=%+v full=%s", last, full)
			}
			if out.Terminal != gateway.TerminalCompleted || !out.Charge || out.Usage.PromptTokens != 100 || out.Usage.CachedTokens != 75 {
				t.Fatalf("actual accounting observation mismatch: %+v", out)
			}
			if retry && (reports[0].TTFT != 0 || reports[0].PromptTokens != 0 || reports[0].CachedTokens != 0 || reports[0].Status != 503 || !reports[0].Retryable) {
				t.Fatalf("failed attempt inherited success metrics: %+v", reports[0])
			}
		})
	}
}

func TestAttemptMetricsEstimatedUsageIsNotARealRoutingObservation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if !metricWait(r.Context(), 15*time.Millisecond) {
			return
		}
		metricEvent(w, `{"choices":[{"index":0,"delta":{"content":"abcdefgh"},"finish_reason":"stop"}]}`)
		metricEvent(w, "[DONE]")
	}))
	t.Cleanup(upstream.Close)
	h, _ := attemptMetricHarness(t, upstream.URL, 1)
	view, out := h.do(streamBody), h.outcome()
	reports := h.planner.results()
	if view.status != 200 || out.Terminal != gateway.TerminalCompletedNoUsage || !out.Charge || !out.Usage.Estimated || out.Usage.PromptTokens <= 0 || out.Usage.CompletionTokens != 2 {
		t.Fatalf("missing usage did not take explicit estimated path: view=%+v out=%+v", view, out)
	}
	if len(reports) != 1 || !reports[0].OK || reports[0].TTFT < 15*time.Millisecond || reports[0].PromptTokens != 0 || reports[0].CachedTokens != 0 {
		t.Fatalf("local estimate became fake real token observation: %+v", reports)
	}
}

func TestAttemptMetricsNoOutputMeansNoFirstOutputTime(t *testing.T) {
	for _, scenario := range []string{"http_failure", "usage_only", "role_then_error"} {
		t.Run(scenario, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if scenario == "http_failure" {
					w.WriteHeader(503)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				metricEvent(w, `{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`)
				if scenario == "usage_only" {
					metricEvent(w, `{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":0,"prompt_tokens_details":{"cached_tokens":75}}}`)
					metricEvent(w, "[DONE]")
				} else {
					metricEvent(w, `{"error":{"message":"failed","code":"native_failure"}}`)
				}
			}))
			t.Cleanup(upstream.Close)
			h, _ := attemptMetricHarness(t, upstream.URL, 1)
			view, out := h.do(streamBody), h.outcome()
			reports := h.planner.results()
			if view.status == 200 || out.Delivered || out.Charge || len(reports) != 1 || reports[0].OK || reports[0].TTFT != 0 {
				t.Fatalf("invisible failure/metadata acquired output metrics: view=%+v out=%+v reports=%+v", view, out, reports)
			}
			if scenario == "usage_only" {
				if out.Terminal != gateway.TerminalEmptyStream || reports[0].PromptTokens != 100 || reports[0].CachedTokens != 75 {
					t.Fatalf("empty usage observation lost or charged: out=%+v reports=%+v", out, reports)
				}
			} else if reports[0].PromptTokens != 0 || reports[0].CachedTokens != 0 {
				t.Fatalf("failed attempt fabricated tokens: %+v", reports[0])
			}
		})
	}
}
