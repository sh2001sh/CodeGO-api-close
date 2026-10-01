package routing

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
)

type scoredGatewayAuth struct{}

func (scoredGatewayAuth) Authorize(context.Context, string) (gateway.Principal, error) {
	return gateway.Principal{UserID: 1, KeyID: 9, Group: "default"}, nil
}

type scoredGatewayCapture struct{ attempts []gateway.Attempt }

func (*scoredGatewayCapture) Reserve(context.Context, *gateway.Request) error { return nil }
func (s *scoredGatewayCapture) Finalize(_ context.Context, req *gateway.Request, _ gateway.Outcome) error {
	s.attempts = append([]gateway.Attempt(nil), req.Attempts...)
	return nil
}

func TestRealGatewayFirstEventAndUsageFeedOfficialScoredHealth(t *testing.T) {
	var successes atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/first/") {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		realUsage := successes.Add(1) == 1
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(20 * time.Millisecond)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"actual upstream content\"}}]}\n\n")
		w.(http.Flusher).Flush()
		// Keep completion well after the first event. Measuring the full relay
		// duration instead of actual TTFT must fail the timing assertion below.
		time.Sleep(100 * time.Millisecond)
		if realUsage {
			_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":7,\"prompt_tokens_details\":{\"cached_tokens\":30}}}\n\n")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()
	snap := scoredPlanFixture()
	pool := snap.OfficialPools["default"]
	pool.Members = pool.Members[:2]
	pool.Members[0].CostMultiplier, pool.Members[1].CostMultiplier = "0.1", "0.2"
	snap.OfficialPools["default"] = pool
	snap.Channels[1].BaseURL, snap.Channels[2].BaseURL = upstream.URL+"/first", upstream.URL+"/next"
	e := newEnv(snap, Config{})
	settler := &scoredGatewayCapture{}
	relay, err := gateway.New(gateway.Deps{Authorizer: scoredGatewayAuth{}, Planner: e.planner, Settler: settler,
		Providers: map[string]gateway.Provider{openai.ID: openai.Provider{}}, Config: gateway.Config{HeaderTimeout: time.Second, RelayTimeout: 5 * time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	relay.Register(mux)
	call := func() {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
		req.Header.Set("Authorization", "Bearer fixture-api-key")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "actual upstream content") {
			t.Fatalf("actual HTTP/SSE pipeline failed: %d %s", w.Code, w.Body.String())
		}
	}
	call()
	if len(settler.attempts) != 2 {
		t.Fatalf("failed first scored target did not retry frozen plan: %d attempts", len(settler.attempts))
	}
	failure, success := settler.attempts[0], settler.attempts[1]
	if failure.Result.TTFT != 0 || failure.Result.PromptTokens != 0 || failure.Result.CachedTokens != 0 {
		t.Fatal("failed upstream invented first-event or usage observations")
	}
	if success.Target.RoutePoolID != 17 || success.Target.ProcurementCostMultiplier != "0.2" {
		t.Fatal("actual retry lost frozen procurement source")
	}
	result := success.Result
	if !result.OK || result.PromptTokens != 100 || result.CachedTokens != 30 || result.TTFT < 10*time.Millisecond || success.Duration-result.TTFT < 50*time.Millisecond {
		t.Fatalf("actual first-event/usage fields not preserved: result=%+v duration=%s", result, success.Duration)
	}
	observed := e.planner.official.read(2, "gpt", e.clock.now())
	if observed.cacheRate != 30 || observed.cacheSamples != 1 || observed.p95(e.clock.now()) != float64(result.TTFT)/float64(time.Millisecond) {
		t.Fatal("real gateway Report did not feed measured TTFT/cache into scored planner")
	}
	call() // The second completion has content but no authoritative usage.
	last := settler.attempts[len(settler.attempts)-1].Result
	if !last.OK || last.TTFT <= 0 || last.PromptTokens != 0 || last.CachedTokens != 0 {
		t.Fatal("local estimated billing usage entered official procurement health")
	}
	if state := e.planner.official.read(2, "gpt", e.clock.now()); state.cacheSamples != 1 {
		t.Fatal("missing usage fabricated a zero-cache success sample")
	}
}
