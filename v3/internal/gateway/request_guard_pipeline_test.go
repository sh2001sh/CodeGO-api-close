package gateway_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
)

type requestGuardPlanner struct {
	fakePlanner
	plans atomic.Int32
}

func (p *requestGuardPlanner) Plan(ctx context.Context, req *gateway.Request) ([]gateway.Target, error) {
	p.plans.Add(1)
	return p.fakePlanner.Plan(ctx, req)
}

func requestGuardGateway(t *testing.T, auth gateway.Authorizer, guard gateway.RequestGuard, upstream string) (http.Handler, *requestGuardPlanner, *fakeSettler) {
	t.Helper()
	plan := &requestGuardPlanner{fakePlanner: fakePlanner{targets: []gateway.Target{
		{ChannelID: 1, Provider: openai.ID, BaseURL: upstream},
		{ChannelID: 2, Provider: openai.ID, BaseURL: upstream},
	}}}
	settler := newSettler()
	g, err := gateway.New(gateway.Deps{Authorizer: auth, Planner: plan, Settler: settler,
		RequestGuard: guard, Providers: map[string]gateway.Provider{openai.ID: openai.Provider{}}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	return mux, plan, settler
}

func requestGuardPost(handler http.Handler, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(streamBody))
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestRequestGuardRejectsBeforePlanningOrReservation(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code int
	}{
		{"blocked", &gateway.UpstreamError{Status: 403, Type: "permission_error", Code: "account_request_disabled", Message: "account requests disabled"}, 403},
		{"rpm", &gateway.UpstreamError{Status: 429, Type: "rate_limit_error", Code: "account_request_rpm_reached", Message: "account request limit reached"}, 429},
		{"dependency", &gateway.UpstreamError{Status: 503, Type: "api_error", Code: "account_request_guard_unavailable", Message: "admission unavailable"}, 503},
		{"generic", errors.New("private dependency credentials"), 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			var checks atomic.Int32
			h, plan, settle := requestGuardGateway(t, fakeAuth{}, func(ctx context.Context, req *gateway.Request) error {
				checks.Add(1)
				if ctx.Err() != nil || req.ID == "" || req.Principal.UserID != 7 || req.Path != "/v1/chat/completions" || string(req.Body) != streamBody {
					t.Error("guard lost original authenticated request")
				}
				return test.err
			}, "http://unused.invalid")
			w := requestGuardPost(h, "sk-test")
			if w.Code != test.code || checks.Load() != 1 || plan.plans.Load() != 0 || settle.reserved != 0 || len(plan.results()) != 0 || strings.Contains(w.Body.String(), "private dependency") {
				t.Fatalf("status=%d checks=%d plans=%d reserve=%d body=%s", w.Code, checks.Load(), plan.plans.Load(), settle.reserved, w.Body.String())
			}
		})
	}
}

func TestRequestGuardCountsOnceAcrossActualUpstreamRetry(t *testing.T) {
	var upstreamCalls, checks atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if upstreamCalls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"okay\"}}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()
	h, plan, settle := requestGuardGateway(t, fakeAuth{}, func(context.Context, *gateway.Request) error {
		checks.Add(1)
		return nil
	}, upstream.URL)
	w := requestGuardPost(h, "sk-test")
	if w.Code != 200 || checks.Load() != 1 || upstreamCalls.Load() != 2 || plan.plans.Load() != 1 || settle.reserved != 1 || len(plan.results()) != 2 {
		t.Fatalf("status=%d checks=%d upstream=%d plans=%d reserve=%d", w.Code, checks.Load(), upstreamCalls.Load(), plan.plans.Load(), settle.reserved)
	}
	if out := <-settle.outcomes; !out.Charge || out.Usage.PromptTokens != 10 {
		t.Fatalf("allowed retry was not normally settled: %+v", out)
	}
}

func TestRequestGuardDoesNotCountUnauthorizedCalls(t *testing.T) {
	for _, test := range []struct {
		name string
		auth gateway.Authorizer
		key  string
		code int
	}{
		{"missing", fakeAuth{}, "", 401},
		{"invalid", fakeAuth{}, "bad-key", 401},
		{"model denied", restrictedAuth{gateway.Principal{UserID: 7, AllowedModels: []string{}}}, "sk-test", 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			var checks atomic.Int32
			h, plan, settle := requestGuardGateway(t, test.auth, func(context.Context, *gateway.Request) error {
				checks.Add(1)
				return nil
			}, "http://unused.invalid")
			w := requestGuardPost(h, test.key)
			if w.Code != test.code || checks.Load() != 0 || plan.plans.Load() != 0 || settle.reserved != 0 {
				t.Fatalf("status=%d checks=%d plans=%d reserve=%d", w.Code, checks.Load(), plan.plans.Load(), settle.reserved)
			}
		})
	}
}
