package live

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/codex"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
	"github.com/tidwall/gjson"
)

type backgroundJobsBilling struct {
	mu                                  sync.Mutex
	reserves, refreshes, finalizes      int
	outcomes                            map[string]gateway.Outcome
	reserveErr, refreshErr, finalizeErr error
}

func (billing *backgroundJobsBilling) Reserve(_ context.Context, req *gateway.Request) (json.RawMessage, error) {
	billing.mu.Lock()
	defer billing.mu.Unlock()
	billing.reserves++
	if len(req.Targets) != 1 {
		return nil, errors.New("durable hold must freeze exactly one route")
	}
	if billing.reserveErr != nil {
		return nil, billing.reserveErr
	}
	return json.Marshal(map[string]any{"request_id": req.ID, "user_id": req.Principal.UserID, "key_id": req.Principal.KeyID})
}

func (billing *backgroundJobsBilling) Refresh(_ context.Context, req *gateway.Request, hold json.RawMessage) error {
	billing.mu.Lock()
	defer billing.mu.Unlock()
	billing.refreshes++
	if gjson.GetBytes(hold, "request_id").Str != req.ID {
		return errors.New("durable reservation cannot be restored")
	}
	return billing.refreshErr
}

func (billing *backgroundJobsBilling) Finalize(_ context.Context, req *gateway.Request, hold json.RawMessage, out gateway.Outcome) error {
	billing.mu.Lock()
	defer billing.mu.Unlock()
	billing.finalizes++
	if billing.finalizeErr != nil {
		return billing.finalizeErr
	}
	if gjson.GetBytes(hold, "request_id").Str != req.ID {
		return errors.New("wrong durable hold")
	}
	if _, posted := billing.outcomes[req.ID]; !posted {
		billing.outcomes[req.ID] = out
	}
	return nil
}

func (billing *backgroundJobsBilling) result(id string) (gateway.Outcome, int, int) {
	billing.mu.Lock()
	defer billing.mu.Unlock()
	return billing.outcomes[id], len(billing.outcomes), billing.finalizes
}

type backgroundJobsPlanner struct{ target gateway.Target }

func (planner backgroundJobsPlanner) Plan(context.Context, *gateway.Request) ([]gateway.Target, error) {
	return []gateway.Target{planner.target}, nil
}

func (backgroundJobsPlanner) Report(gateway.Target, gateway.AttemptResult) {}

func backgroundJobsFixture(t *testing.T, base, provider string) (*Handler, *http.ServeMux, http.Handler, *backgroundJobsMemory, *backgroundJobsBilling) {
	t.Helper()
	if provider == "" {
		provider = "openai"
	}
	target := gateway.Target{ChannelID: 7, CredentialID: 9, Provider: provider, BaseURL: base, Secret: "upstream-secret"}
	adapters := map[string]gateway.Provider{"openai": responses.Provider{}, "responses": responses.Provider{}, "codex": codex.Provider{}}
	if provider == "codex" {
		target.Secret = `{"access_token":"access","account_id":"account"}`
	}
	repo := newBackgroundJobsMemory()
	billing := &backgroundJobsBilling{outcomes: make(map[string]gateway.Outcome)}
	h, err := New(Config{Auth: backgroundAuth{}, Planner: backgroundJobsPlanner{target: target}, Settler: backgroundSettler{},
		Repository: &backgroundRepository{}, BackgroundJobs: repo, BackgroundBilling: billing, Providers: adapters,
		Resolve: func(context.Context, int64, int64) (gateway.Target, error) { return target, nil },
		ResolvePrincipal: func(_ context.Context, user, key int64) (gateway.Principal, error) {
			return gateway.Principal{UserID: user, KeyID: key}, nil
		}, SessionTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.registerBackground(mux)
	for _, path := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses"} {
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) { body, _ := io.ReadAll(r.Body); _, _ = w.Write(body) })
	}
	return h, mux, h.Wrap(mux), repo, billing
}

func createBackgroundForTest(t *testing.T, wrapped http.Handler, path, body string) string {
	t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer owner")
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	id := gjson.GetBytes(w.Body.Bytes(), "id").Str
	if !strings.HasPrefix(id, "resp_bg_") || gjson.GetBytes(w.Body.Bytes(), "status").Str != "queued" {
		t.Fatalf("invalid queued response %s", w.Body.String())
	}
	return id
}

const backgroundCompletedSnapshot = `{"id":"resp_up","object":"response","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":5,"output_tokens":3,"input_tokens_details":{"cached_tokens":2}}}`

const backgroundLocalEvents = "event: response.created\ndata: {\"type\":\"response.created\",\"sequence_number\":0,\"response\":{\"id\":\"resp_up\",\"status\":\"in_progress\"}}\n\nevent: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"sequence_number\":1,\"delta\":\"hello\",\"response_id\":\"resp_up\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"sequence_number\":2,\"response\":" + backgroundCompletedSnapshot + "}\n\n"
