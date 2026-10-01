package live

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type backgroundOverrideTransport func(*http.Request) (*http.Response, error)

func (fn backgroundOverrideTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestBackgroundJobsConfiguredClientFailureCannotFallBack(t *testing.T) {
	for _, unavailable := range []error{nil, errors.New("credential client unavailable")} {
		h, _, wrapped, repo, billing := backgroundJobsFixture(t, "https://unused.invalid", "openai")
		var forwarded atomic.Int64
		h.cfg.Client = &http.Client{Transport: backgroundOverrideTransport(func(req *http.Request) (*http.Response, error) {
			forwarded.Add(1)
			return backgroundOverrideResponse(req, 200, backgroundCompletedSnapshot), nil
		})}
		h.cfg.Clients = func(context.Context, gateway.Target) (*http.Client, error) { return nil, unavailable }
		id := createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","background":true}`)
		if err := h.Reconcile(context.Background(), 10); err != nil {
			t.Fatal(err)
		}
		job, _ := repo.GetOwned(context.Background(), id, 1, 11)
		out, count, _ := billing.result(id)
		if forwarded.Load() != 0 || job.Status != "failed" || !job.Billed || out.Charge || count != 1 {
			t.Fatalf("configured client failure used fallback job=%+v out=%+v forwarded=%d", job, out, forwarded.Load())
		}
	}
}

func backgroundOverrideResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

func TestBackgroundJobsNativeOverridesAndConfiguredCredentialClient(t *testing.T) {
	h, _, wrapped, repo, billing := backgroundJobsFixture(t, "https://unused.invalid", "openai")
	target, _ := h.cfg.Resolve(context.Background(), 7, 9)
	target.ProxyURL = "http://credential-proxy.invalid"
	target.Fingerprint = gateway.CredentialFingerprint{UserAgent: "stable-client", TLSProfile: "chrome"}
	target.HeaderOverride = map[string]string{"X-Route": "configured"}
	target.ParamOverride = map[string]any{"temperature": 0.25}
	target.StatusCodeMapping = map[string]int{"503": 200}
	h.cfg.Planner = backgroundJobsPlanner{target: target}
	h.cfg.Resolve = func(context.Context, int64, int64) (gateway.Target, error) { return target, nil }
	var clients, posts, gets atomic.Int64
	transport := backgroundOverrideTransport(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("X-Route") != "configured" {
			t.Error("channel header override was not dispatched")
		}
		if req.Method == http.MethodPost {
			posts.Add(1)
			body, _ := io.ReadAll(req.Body)
			if gjson.GetBytes(body, "temperature").Float() != 0.25 || !gjson.GetBytes(body, "background").Bool() || !gjson.GetBytes(body, "stream").Bool() {
				t.Errorf("converted native bytes did not consume JSON overrides: %s", body)
			}
			return backgroundOverrideResponse(req, 503, `{"id":"resp_up","status":"queued","output":[]}`), nil
		}
		gets.Add(1)
		if req.Body != nil || req.URL.Path != "/v1/responses/resp_up" {
			t.Error("generation override mutated the bodyless metadata request")
		}
		return backgroundOverrideResponse(req, 503, backgroundCompletedSnapshot), nil
	})
	configured := &http.Client{Transport: transport}
	h.cfg.Clients = func(_ context.Context, selected gateway.Target) (*http.Client, error) {
		clients.Add(1)
		if selected.ChannelID != 7 || selected.CredentialID != 9 || selected.ProxyURL != target.ProxyURL || selected.Fingerprint != target.Fingerprint {
			t.Errorf("configured client resolved wrong credential identity: %d/%d", selected.ChannelID, selected.CredentialID)
		}
		return configured, nil
	}
	const original = `{"model":"gpt-test","background":true,"temperature":1,"input":"hello"}`
	id := createBackgroundForTest(t, wrapped, "/responses", original)
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	job, _ := repo.GetOwned(context.Background(), id, 1, 11)
	if job.Status != "queued" || job.UpstreamID != "resp_up" || job.Billed || string(job.Body) != original {
		t.Fatalf("mapped acceptance or original body incorrect: %+v", job)
	}
	repo.expire(id)
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	job, _ = repo.GetOwned(context.Background(), id, 1, 11)
	out, count, _ := billing.result(id)
	if job.Status != "completed" || !job.Billed || !out.Charge || count != 1 || posts.Load() != 1 || gets.Load() != 1 || clients.Load() != 2 || string(job.Body) != original {
		t.Fatalf("mapped poll/client/body mismatch job=%+v out=%+v client calls=%d", job, out, clients.Load())
	}
	if configured.CheckRedirect != nil {
		t.Fatal("shared client was mutated instead of copied")
	}
}

func TestBackgroundJobsMappedFailureAndInvalidOverridesRefundWithoutFallback(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		h, _, wrapped, repo, billing := backgroundJobsFixture(t, "https://unused.invalid", "openai")
		target, _ := h.cfg.Resolve(context.Background(), 7, 9)
		target.StatusCodeMapping = map[string]int{"200": 429}
		if invalid {
			target.HeaderOverride = map[string]string{"X-Invalid": "contains\nnewline"}
		}
		h.cfg.Resolve = func(context.Context, int64, int64) (gateway.Target, error) { return target, nil }
		var calls atomic.Int64
		h.cfg.Clients = func(context.Context, gateway.Target) (*http.Client, error) {
			return &http.Client{Transport: backgroundOverrideTransport(func(req *http.Request) (*http.Response, error) {
				calls.Add(1)
				return backgroundOverrideResponse(req, 200, backgroundCompletedSnapshot), nil
			})}, nil
		}
		id := createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","background":true}`)
		if err := h.Reconcile(context.Background(), 10); err != nil {
			t.Fatal(err)
		}
		job, _ := repo.GetOwned(context.Background(), id, 1, 11)
		out, count, _ := billing.result(id)
		want := int64(1)
		if invalid {
			want = 0
		}
		if job.Status != "failed" || !job.Billed || out.Charge || count != 1 || calls.Load() != want {
			t.Fatalf("mapped failure invalid=%v job=%+v out=%+v calls=%d", invalid, job, out, calls.Load())
		}
	}
}

func TestBackgroundMetadataUsesConfiguredClientHeaderAndStatusPolicy(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		target := gateway.Target{Provider: "openai", BaseURL: "https://unused.invalid", ProxyURL: "http://credential-proxy.invalid",
			HeaderOverride: map[string]string{"X-Route": "metadata"}, ParamOverride: map[string]any{"temperature": 0}, StatusCodeMapping: map[string]int{"503": 200}}
		h, mux, _, _ := backgroundFixture(t, target)
		var calls atomic.Int64
		h.cfg.Clients = func(_ context.Context, selected gateway.Target) (*http.Client, error) {
			if selected.ChannelID != 7 || selected.CredentialID != 9 || selected.ProxyURL != target.ProxyURL {
				t.Error("metadata did not select original credential client")
			}
			return &http.Client{Transport: backgroundOverrideTransport(func(req *http.Request) (*http.Response, error) {
				calls.Add(1)
				if req.Header.Get("X-Route") != "metadata" || req.Body != nil || req.Method != method {
					t.Error("metadata headers/body/method changed")
				}
				return backgroundOverrideResponse(req, 503, backgroundCompletedSnapshot), nil
			})}, nil
		}
		path := "/responses/resp_123"
		if method == http.MethodPost {
			path += "/cancel"
		}
		w := backgroundCall(mux, method, path, "owner")
		if w.Code != http.StatusOK || calls.Load() != 1 || w.Body.String() != backgroundCompletedSnapshot {
			t.Fatalf("metadata mapped result status=%d calls=%d body=%s", w.Code, calls.Load(), w.Body.String())
		}
	}
}
