package live

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type guardPlanner struct {
	livePlan
	plans atomic.Int32
}

func (p *guardPlanner) Plan(ctx context.Context, req *gateway.Request) ([]gateway.Target, error) {
	p.plans.Add(1)
	return p.livePlan.Plan(ctx, req)
}

type guardContextKey struct{}

func guardDenials() []struct {
	name string
	err  error
	want gateway.UpstreamError
} {
	return []struct {
		name string
		err  error
		want gateway.UpstreamError
	}{
		{"blocked", &gateway.UpstreamError{Status: 403, Type: "permission_error", Code: "account_request_disabled", Message: "account requests disabled"}, gateway.UpstreamError{Status: 403, Type: "permission_error", Code: "account_request_disabled", Message: "account requests disabled"}},
		{"rpm", &gateway.UpstreamError{Status: 429, Type: "rate_limit_error", Code: "account_request_rpm_reached", Message: "account request limit reached"}, gateway.UpstreamError{Status: 429, Type: "rate_limit_error", Code: "account_request_rpm_reached", Message: "account request limit reached"}},
		{"dependency", &gateway.UpstreamError{Status: 503, Type: "api_error", Code: "account_request_guard_unavailable", Message: "admission unavailable"}, gateway.UpstreamError{Status: 503, Type: "api_error", Code: "account_request_guard_unavailable", Message: "admission unavailable"}},
		{"generic", errors.New("private dependency credentials"), gateway.UpstreamError{Status: 503, Type: "api_error", Code: "request_guard_unavailable", Message: "request admission is temporarily unavailable"}},
	}
}

func TestLiveHTTPGuardRejectsBeforePlanReserveUpstreamOrJob(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses", "/v1/realtime"} {
		for _, denial := range guardDenials() {
			t.Run(path+"/"+denial.name, func(t *testing.T) {
				var checks, upstreamCalls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { upstreamCalls.Add(1) }))
				defer upstream.Close()
				h, _, wrapped, repo, billing := backgroundJobsFixture(t, upstream.URL, "openai")
				planner := &guardPlanner{}
				h.cfg.Planner = planner
				ledger := &liveLedger{finalized: make(chan gateway.Outcome, 1)}
				h.cfg.Settler = ledger
				h.cfg.RequestGuard = func(ctx context.Context, req *gateway.Request) error {
					checks.Add(1)
					if ctx.Value(guardContextKey{}) != "retained" || req.ID == "" || req.Model != "gpt-test" || req.Path != path || req.Principal.UserID != 1 || req.Principal.KeyID != 11 {
						t.Errorf("guard lost incoming identity/model/path/context: %+v", req)
					}
					return denial.err
				}
				r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-test","background":true}`))
				if path == "/v1/realtime" {
					r = httptest.NewRequest(http.MethodGet, path+"?model=gpt-test", nil)
					r.Header.Set("Upgrade", "websocket")
					r.Header.Set("Connection", "Upgrade")
				}
				r = r.WithContext(context.WithValue(r.Context(), guardContextKey{}, "retained"))
				r.Header.Set("Authorization", "Bearer owner")
				w := httptest.NewRecorder()
				if path == "/v1/realtime" {
					h.serveRealtime(w, r)
				} else {
					wrapped.ServeHTTP(w, r)
				}
				body := w.Body.Bytes()
				if w.Code != denial.want.Status || gjson.GetBytes(body, "error.type").Str != denial.want.Type || gjson.GetBytes(body, "error.code").Str != denial.want.Code || gjson.GetBytes(body, "error.message").Str != denial.want.Message {
					t.Fatalf("guard error changed status=%d body=%s", w.Code, body)
				}
				if checks.Load() != 1 || planner.plans.Load() != 0 || billing.reserves != 0 || ledger.reserves.Load() != 0 || upstreamCalls.Load() != 0 || len(repo.jobs) != 0 {
					t.Fatalf("denial admitted work checks=%d plans=%d reserves=%d/%d upstream=%d jobs=%d", checks.Load(), planner.plans.Load(), billing.reserves, ledger.reserves.Load(), upstreamCalls.Load(), len(repo.jobs))
				}
			})
		}
	}
}

type guardAuth struct{ principal gateway.Principal }

func (a guardAuth) Authorize(context.Context, string) (gateway.Principal, error) {
	return a.principal, nil
}

func TestLiveGuardDoesNotCountAuthModelIPOrHandshakeRejection(t *testing.T) {
	for _, mode := range []string{"missing-auth", "model", "ip", "handshake"} {
		for _, realtime := range []bool{false, true} {
			if mode == "handshake" && !realtime {
				continue
			}
			t.Run(mode+map[bool]string{false: "-background", true: "-realtime"}[realtime], func(t *testing.T) {
				h, _, wrapped, _, billing := backgroundJobsFixture(t, "https://unused.invalid", "openai")
				var checks atomic.Int32
				planner := &guardPlanner{}
				h.cfg.Planner = planner
				h.cfg.RequestGuard = func(context.Context, *gateway.Request) error { checks.Add(1); return nil }
				principal := gateway.Principal{UserID: 1, KeyID: 11}
				if mode == "model" {
					principal.AllowedModels = []string{}
				}
				if mode == "ip" {
					principal.AllowedCIDRs = []netip.Prefix{}
				}
				h.cfg.Auth = guardAuth{principal: principal}
				r := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{"model":"gpt-test","background":true}`))
				if realtime {
					r = httptest.NewRequest(http.MethodGet, "/v1/realtime?model=gpt-test", nil)
					if mode != "handshake" {
						r.Header.Set("Upgrade", "websocket")
						r.Header.Set("Connection", "Upgrade")
					}
				}
				if mode != "missing-auth" {
					r.Header.Set("Authorization", "Bearer owner")
				}
				w := httptest.NewRecorder()
				if realtime {
					h.serveRealtime(w, r)
				} else {
					wrapped.ServeHTTP(w, r)
				}
				if w.Code < 400 || checks.Load() != 0 || planner.plans.Load() != 0 || billing.reserves != 0 {
					t.Fatalf("invalid admission consumed guard status=%d checks=%d plans=%d reserves=%d", w.Code, checks.Load(), planner.plans.Load(), billing.reserves)
				}
			})
		}
	}
}

func TestBackgroundFalseWrapCountsOnlyCoreGuard(t *testing.T) {
	h, _, _, _, _ := backgroundJobsFixture(t, "https://unused.invalid", "openai")
	var checks atomic.Int32
	h.cfg.RequestGuard = func(_ context.Context, req *gateway.Request) error {
		checks.Add(1)
		if req.Path != "/v1/responses" || gjson.GetBytes(req.Body, "background").Bool() {
			t.Error("core guard received incorrect fallback request")
		}
		return &gateway.UpstreamError{Status: 429, Type: "rate_limit_error", Code: "account_request_rpm_reached", Message: "limit reached"}
	}
	planner := &guardPlanner{}
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 1)}
	core, err := gateway.New(gateway.Deps{Authorizer: h.cfg.Auth, Planner: planner, Settler: ledger, Providers: h.cfg.Providers, RequestGuard: h.cfg.RequestGuard})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	core.Register(mux)
	w := filesTestRequest(h.Wrap(mux), http.MethodPost, "/v1/responses", "owner", strings.NewReader(`{"model":"gpt-test","background":false,"input":"hello"}`), "application/json")
	if w.Code != 429 || checks.Load() != 1 || planner.plans.Load() != 0 || ledger.reserves.Load() != 0 {
		t.Fatalf("fallback counted more than once status=%d checks=%d plans=%d reserves=%d", w.Code, checks.Load(), planner.plans.Load(), ledger.reserves.Load())
	}
}
