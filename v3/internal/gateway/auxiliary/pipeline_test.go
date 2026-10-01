package auxiliary

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type testAuth struct{ err error }

func (a testAuth) Authorize(context.Context, string) (gateway.Principal, error) {
	return gateway.Principal{UserID: 1, KeyID: 2, Group: "default"}, a.err
}

type testPlan struct {
	targets []gateway.Target
	model   string
	reports []gateway.AttemptResult
}

func (p *testPlan) Plan(_ context.Context, r *gateway.Request) ([]gateway.Target, error) {
	p.model = r.Model
	return p.targets, nil
}
func (p *testPlan) Report(_ gateway.Target, result gateway.AttemptResult) {
	p.reports = append(p.reports, result)
}

type testSettle struct {
	err       error
	reserves  int
	model     string
	out       gateway.Outcome
	req       *gateway.Request
	finalized bool
}

func (s *testSettle) Reserve(_ context.Context, r *gateway.Request) error {
	s.reserves++
	s.model = r.Model
	return s.err
}
func (s *testSettle) Finalize(ctx context.Context, r *gateway.Request, out gateway.Outcome) error {
	s.req, s.out, s.finalized = r, out, ctx.Err() == nil
	return nil
}

type testLimits struct {
	acquired, released int
	err                error
}

func (l *testLimits) Acquire(context.Context, *gateway.Request, gateway.Target) error {
	l.acquired++
	return l.err
}
func (l *testLimits) Release(context.Context, *gateway.Request, gateway.Target) error {
	l.released++
	return nil
}

func testHandler(t *testing.T, urls ...string) (*Handler, *testPlan, *testSettle, *testLimits) {
	t.Helper()
	plan := &testPlan{}
	for i, address := range urls {
		plan.targets = append(plan.targets, gateway.Target{ChannelID: int64(i + 1), Provider: "openai", BaseURL: address, Secret: "upstream-secret", UpstreamModel: "mapped"})
	}
	settle, limits := &testSettle{}, &testLimits{}
	h, err := New(Config{Authorizer: testAuth{}, Planner: plan, Settler: settle, Limits: limits, RelayTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return h, plan, settle, limits
}

func invoke(h *Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer client-key")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestAdmissionRefusesBeforeHTTP(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	for _, scenario := range []struct {
		name          string
		auth, billing error
		maximum       int64
		status        int
		reserves      int
	}{
		{"invalid key", gateway.ErrInvalidKey, nil, 0, 401, 0},
		{"auth outage", gateway.ErrAuthUnavailable, nil, 0, 503, 0},
		{"billing outage", nil, gateway.ErrBillingUnavailable, 0, 503, 1},
		{"missing price", nil, errors.New("no price"), 0, 503, 1},
		{"insufficient funds", nil, gateway.ErrInsufficientCredits, 0, 402, 1},
		{"body limit", nil, nil, 4, 413, 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			h, _, settle, _ := testHandler(t, server.URL)
			h.cfg.Authorizer, settle.err = testAuth{scenario.auth}, scenario.billing
			if scenario.maximum > 0 {
				h.cfg.MaxBodyBytes = scenario.maximum
			}
			response := invoke(h, "/v1/embeddings", `{"model":"test","input":"hello"}`)
			if response.Code != scenario.status || settle.reserves != scenario.reserves || settle.finalized {
				t.Fatalf("status=%d reserves=%d finalized=%v", response.Code, settle.reserves, settle.finalized)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("denied requests reached upstream")
	}
}

func TestActualUsageMappingAndHeaderBoundary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v1/images/generations" || !strings.Contains(string(body), `"model":"mapped"`) || r.Header.Get("Authorization") != "Bearer upstream-secret" || r.Header.Get("Cookie") != "" {
			t.Errorf("bad upstream request path=%s body=%s", r.URL.Path, body)
		}
		w.Header().Set("Set-Cookie", "private=value")
		w.Header().Set("Authorization", "Bearer secret")
		w.Header().Set("X-Request-Id", "upstream-private")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"aW1hZ2U="}],"usage":{"input_tokens":10,"output_tokens":20,"input_tokens_details":{"cached_tokens":3,"image_tokens":4},"output_tokens_details":{"image_tokens":20}}}`)
	}))
	defer server.Close()
	h, plan, settle, limits := testHandler(t, server.URL)
	w := invoke(h, "/v1/images/generations", `{"model":"alias","prompt":"cat"}`)
	if w.Code != 200 || w.Header().Get("Set-Cookie") != "" || w.Header().Get("Authorization") != "" || w.Header().Get("X-Request-Id") == "upstream-private" {
		t.Fatalf("bad response %d %v", w.Code, w.Header())
	}
	if !settle.finalized || !settle.out.Charge || settle.out.Terminal != gateway.TerminalCompleted || settle.out.Usage.PromptTokens != 10 || settle.out.Usage.ImageOutputTokens != 20 || settle.out.Usage.CachedTokens != 3 {
		t.Fatalf("bad outcome %+v", settle.out)
	}
	if plan.model != "alias" || limits.acquired != 1 || limits.released != 1 {
		t.Fatalf("admission model=%s limits=%+v", plan.model, limits)
	}
}

func TestRefundAndFailover(t *testing.T) {
	for _, status := range []int{400, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"message":"secret"}}`)
			}))
			defer first.Close()
			var calls atomic.Int64
			second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.WriteString(w, `{"data":[{"embedding":[1]}],"usage":{"prompt_tokens":3,"total_tokens":3}}`)
			}))
			defer second.Close()
			h, plan, settle, limits := testHandler(t, first.URL, second.URL)
			w := invoke(h, "/v1/embeddings", `{"model":"alias","input":"hello"}`)
			if status == 400 {
				if w.Code != 400 || settle.out.Charge || calls.Load() != 0 || len(plan.reports) != 1 {
					t.Fatalf("unexpected failure status=%d outcome=%+v reports=%v", w.Code, settle.out, plan.reports)
				}
			} else if w.Code != 200 || !settle.out.Charge || calls.Load() != 1 || len(plan.reports) != 2 {
				t.Fatalf("failover status=%d outcome=%+v", w.Code, settle.out)
			}
			if limits.acquired != limits.released {
				t.Fatal("lease leaked")
			}
			if strings.Contains(w.Body.String(), "secret") {
				t.Fatal("upstream details leaked")
			}
		})
	}
}

func TestBadEndpointInputRefundsBeforeHTTP(t *testing.T) {
	var called atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called.Add(1) }))
	defer server.Close()
	h, _, settle, _ := testHandler(t, server.URL)
	w := invoke(h, "/v1/audio/speech", `{"model":"tts"}`)
	if w.Code != 400 || called.Load() != 0 || settle.out.Charge || !settle.finalized || settle.reserves != 1 {
		t.Fatalf("status=%d outcome=%+v calls=%d", w.Code, settle.out, called.Load())
	}
}

func TestTimeoutRefundsAndFinalizesDetached(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(200 * time.Millisecond):
		}
	}))
	defer server.Close()
	h, _, settle, limits := testHandler(t, server.URL)
	h.cfg.RelayTimeout = 20 * time.Millisecond
	w := invoke(h, "/v1/embeddings", `{"model":"alias","input":"hello"}`)
	if w.Code != 504 || settle.out.Terminal != gateway.TerminalTimeout || settle.out.Charge || !settle.finalized || limits.released != 1 {
		t.Fatalf("status=%d outcome=%+v finalized=%v", w.Code, settle.out, settle.finalized)
	}
}
