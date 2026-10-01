package auxiliary

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestAuxiliaryRequestGuardRefusesBeforePlanReserveOrUpstream(t *testing.T) {
	var upstream atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { upstream.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable, 0} {
		h, plan, settle, limits := testHandler(t, server.URL)
		spy := &policyAdapterSpy{Adapter: h.adapters["openai"]}
		h.adapters["openai"] = spy
		calls := 0
		h.cfg.RequestGuard = func(ctx context.Context, req *gateway.Request) error {
			calls++
			if ctx.Err() != nil || req.Principal.UserID != 1 || req.Principal.KeyID != 2 || req.ID == "" || req.Path != "/v1/embeddings" || req.Model != "alias" || len(req.Targets) != 0 || string(req.Body) != `{"model":"alias","input":"original"}` {
				t.Error("guard did not receive current authorized request before planning")
			}
			if status == 0 {
				return errors.New("private dependency failure")
			}
			return fmt.Errorf("wrapped: %w", &gateway.UpstreamError{Status: status, Type: "permission_error", Code: "request_refused", Message: "request refused"})
		}
		w := invoke(h, "/v1/embeddings", `{"model":"alias","input":"original"}`)
		want := status
		if want == 0 {
			want = http.StatusServiceUnavailable
		}
		if w.Code != want || calls != 1 || plan.model != "" || settle.reserves != 0 || settle.finalized || spy.builds.Load() != 0 || upstream.Load() != 0 || limits.acquired != 0 || strings.Contains(w.Body.String(), "private dependency failure") {
			t.Fatalf("status=%d calls=%d planned=%q reserves=%d builds=%d upstream=%d", w.Code, calls, plan.model, settle.reserves, spy.builds.Load(), upstream.Load())
		}
	}
}

func TestAuxiliaryRequestGuardRunsOnceAcrossCredentialRetry(t *testing.T) {
	var upstream atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if upstream.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"embedding":[1]}],"usage":{"prompt_tokens":1}}`)
	}))
	defer server.Close()
	h, _, settle, limits := testHandler(t, server.URL, server.URL)
	calls := 0
	var guarded *gateway.Request
	h.cfg.RequestGuard = func(_ context.Context, req *gateway.Request) error {
		calls++
		guarded = req
		return nil
	}
	w := invoke(h, "/v1/embeddings", `{"model":"alias","input":"original"}`)
	if w.Code != http.StatusOK || calls != 1 || upstream.Load() != 2 || settle.req != guarded || guarded.ID != w.Header().Get("X-Request-Id") || settle.reserves != 1 || !settle.out.Charge || limits.acquired != 2 || limits.released != 2 {
		t.Fatalf("status=%d guard=%d upstream=%d outcome=%+v", w.Code, calls, upstream.Load(), settle.out)
	}
}

func TestAuxiliaryRequestGuardDoesNotRunBeforeAuthenticationOrKeyPolicy(t *testing.T) {
	for _, rejectAuth := range []bool{false, true} {
		h, plan, settle, _ := testHandler(t, "http://unused.invalid")
		if rejectAuth {
			h.cfg.Authorizer = testAuth{gateway.ErrInvalidKey}
		} else {
			h.cfg.ValidateRequest = func(gateway.Principal, string, *http.Request) error {
				return &gateway.UpstreamError{Status: http.StatusForbidden, Message: "key policy denied"}
			}
		}
		calls := 0
		h.cfg.RequestGuard = func(context.Context, *gateway.Request) error { calls++; return nil }
		w := invoke(h, "/v1/embeddings", `{"model":"alias","input":"original"}`)
		if calls != 0 || plan.model != "" || settle.reserves != 0 || w.Code == http.StatusOK {
			t.Fatalf("guard consumed unauthorized request: status=%d calls=%d", w.Code, calls)
		}
	}
}
