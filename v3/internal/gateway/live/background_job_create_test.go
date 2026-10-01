package live

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestBackgroundJobsCreateFailureRefundsAndUnavailableFailsClosed(t *testing.T) {
	h, _, wrapped, repo, billing := backgroundJobsFixture(t, "http://unused.invalid", "openai")
	repo.createErr = errors.New("store unavailable")
	r := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"gpt-test","background":true}`))
	r.Header.Set("Authorization", "Bearer owner")
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, r)
	if w.Code != 503 || billing.reserves != 1 || len(billing.outcomes) != 1 {
		t.Fatalf("create rollback status=%d reserves=%d outcomes=%d", w.Code, billing.reserves, len(billing.outcomes))
	}
	for _, out := range billing.outcomes {
		if out.Charge {
			t.Fatal("uncreated job charged")
		}
	}
	h.cfg.BackgroundBilling = nil
	r = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"gpt-test","background":true}`))
	r.Header.Set("Authorization", "Bearer owner")
	w = httptest.NewRecorder()
	wrapped.ServeHTTP(w, r)
	if w.Code != 503 || billing.reserves != 1 {
		t.Fatal("missing durable billing fell back to free execution")
	}
}

func TestBackgroundJobsPassthroughBoundsAndPricingSecrets(t *testing.T) {
	h, _, wrapped, repo, _ := backgroundJobsFixture(t, "http://unused.invalid", "openai")
	for _, body := range []string{"{ invalid JSON", "  {\"model\":\"m\",\"background\":false} \n", `{"model":"m","input":"ordinary"}`} {
		r := httptest.NewRequest("POST", "/responses", strings.NewReader(body))
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, r)
		if w.Body.String() != body {
			t.Fatalf("ordinary body changed: %q -> %q", body, w.Body.String())
		}
	}
	h.cfg.MaxBodyBytes = 10
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, httptest.NewRequest("POST", "/responses", strings.NewReader(strings.Repeat("x", 11))))
	if w.Code != 413 {
		t.Fatal("oversized body was forwarded")
	}
	h.cfg.MaxBodyBytes = 1024
	r := httptest.NewRequest("POST", "/responses", strings.NewReader(`{"model":"gpt-test","background":true}`))
	r.Header.Set("Authorization", "Bearer owner")
	r.Header.Set("X-Api-Key", "private-client-key")
	r.Header.Set("Cookie", "private-cookie")
	r.Header.Set("Proxy-Authorization", "private-proxy-key")
	r.Header.Set("X-Price-Test", "premium")
	w = httptest.NewRecorder()
	wrapped.ServeHTTP(w, r)
	id := gjson.GetBytes(w.Body.Bytes(), "id").Str
	job, _ := repo.GetOwned(context.Background(), id, 1, 11)
	data, _ := json.Marshal(job)
	for _, secret := range []string{"Bearer owner", "private-client-key", "private-cookie", "private-proxy-key", "upstream-secret"} {
		if strings.Contains(string(data), secret) {
			t.Errorf("secret persisted: %s", secret)
		}
	}
	if job.PricingHeaders["X-Price-Test"] != "premium" {
		t.Fatal("non-secret pricing header was lost")
	}
}

func TestBackgroundJobsPolicyChecksBeforeReserveAndAfterRestart(t *testing.T) {
	var calls atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer up.Close()
	h, _, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, "openai")
	h.cfg.ValidateRequest = func(_ gateway.Principal, model string, _ *http.Request) error {
		if model == "forbidden" {
			return errors.New("model denied")
		}
		return nil
	}
	r := httptest.NewRequest("POST", "/responses", strings.NewReader(`{"model":"forbidden","background":true}`))
	r.Header.Set("Authorization", "Bearer owner")
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, r)
	if w.Code != 403 || billing.reserves != 0 {
		t.Fatal("model denial happened after reservation")
	}
	id := createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","background":true}`)
	h.cfg.ValidateRequest = nil
	h.cfg.ResolvePrincipal = func(_ context.Context, user, key int64) (gateway.Principal, error) {
		return gateway.Principal{UserID: user, KeyID: key, AllowedModels: []string{}}, nil
	}
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	job, _ := repo.GetOwned(context.Background(), id, 1, 11)
	out, _, _ := billing.result(id)
	if calls.Load() != 0 || out.Charge || job.Status != "failed" || !job.Billed {
		t.Fatalf("revoked policy not enforced job=%+v out=%+v calls=%d", job, out, calls.Load())
	}
}

type backgroundJobsManyPlanner struct{ target gateway.Target }

func (planner backgroundJobsManyPlanner) Plan(context.Context, *gateway.Request) ([]gateway.Target, error) {
	second := planner.target
	second.ChannelID = 99
	return []gateway.Target{planner.target, second}, nil
}

func (backgroundJobsManyPlanner) Report(gateway.Target, gateway.AttemptResult) {}

func TestBackgroundJobsReservationFreezesOneTargetAndLegacyAliases(t *testing.T) {
	for _, alias := range []string{"openaimax", "openai_max"} {
		t.Run(alias, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if !gjson.GetBytes(body, "background").Bool() {
					t.Error("legacy alias lost native background capability")
				}
				_, _ = io.WriteString(w, backgroundCompletedSnapshot)
			}))
			defer up.Close()
			h, _, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, alias)
			target, _ := h.cfg.Resolve(context.Background(), 7, 9)
			h.cfg.Planner = backgroundJobsManyPlanner{target: target}
			id := createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","background":true}`)
			if err := h.Reconcile(context.Background(), 10); err != nil {
				t.Fatal(err)
			}
			job, _ := repo.GetOwned(context.Background(), id, 1, 11)
			out, count, _ := billing.result(id)
			if !job.Native || job.ChannelID != 7 || !out.Charge || count != 1 {
				t.Fatalf("alias/single-route job=%+v out=%+v", job, out)
			}
		})
	}
}

type backgroundJobsAuthFunc func(context.Context, string) (gateway.Principal, error)

func (fn backgroundJobsAuthFunc) Authorize(ctx context.Context, key string) (gateway.Principal, error) {
	return fn(ctx, key)
}

func TestBackgroundJobsNativeSubmitCannotBypassFileOwnership(t *testing.T) {
	var calls atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer up.Close()
	h, _, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, "openai")
	store, err := NewDiskFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	file, err := store.Create(context.Background(), 2, "private.txt", "assistants", "text/plain", strings.NewReader("private"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	h.cfg.Files = store
	body, _ := json.Marshal(map[string]any{"model": "gpt-test", "background": true, "input": []any{map[string]any{"role": "user", "content": []any{map[string]string{"type": "input_file", "file_id": file.ID}}}}})
	id := createBackgroundForTest(t, wrapped, "/responses", string(body))
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	job, _ := repo.GetOwned(context.Background(), id, 1, 11)
	out, count, _ := billing.result(id)
	if calls.Load() != 0 || job.Status != "failed" || !job.Billed || out.Charge || count != 1 {
		t.Fatalf("foreign file reached native upstream job=%+v out=%+v calls=%d", job, out, calls.Load())
	}
}
