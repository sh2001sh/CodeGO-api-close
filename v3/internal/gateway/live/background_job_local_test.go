package live

import (
	"bytes"
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
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
	"github.com/tidwall/gjson"
)

func TestBackgroundJobsLocalCodexAndPersistedStreamResume(t *testing.T) {
	var posts atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/backend-api/codex/responses" || gjson.GetBytes(body, "background").Exists() || gjson.GetBytes(body, "store").Bool() || !gjson.GetBytes(body, "stream").Bool() {
			t.Errorf("incorrect local Codex request %s %s", r.URL.Path, body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, backgroundLocalEvents)
	}))
	defer up.Close()
	h, mux, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, "codex")
	completed := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		r := httptest.NewRequest("POST", "/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-test","background":true,"stream":true,"input":"hello"}`))
		r.Header.Set("Authorization", "Bearer owner")
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, r)
		completed <- w
	}()
	var id string
	select {
	case id = <-repo.created:
	case <-time.After(time.Second):
		t.Fatal("stream creation was not persisted")
	}
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	var initial *httptest.ResponseRecorder
	select {
	case initial = <-completed:
	case <-time.After(time.Second):
		t.Fatal("completed persisted stream did not close")
	}
	reader := sse.NewReader(bytes.NewReader(initial.Body.Bytes()), 0)
	seen := int64(0)
	for {
		event, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if gjson.GetBytes(event.Data, "sequence_number").Int() != seen {
			t.Fatalf("non-monotonic local sequence %s", event.Data)
		}
		if strings.Contains(string(event.Data), "resp_up") {
			t.Fatalf("upstream ID exposed instead of local job ID: %s", event.Data)
		}
		seen++
	}
	if seen != 3 {
		t.Fatalf("expected three persisted events, got %d: %s", seen, initial.Body.String())
	}
	resumed := backgroundCall(mux, "GET", "/v1/responses/"+id+"?stream=true&starting_after=1", "owner")
	if strings.Contains(resumed.Body.String(), "response.output_text.delta") || !strings.Contains(resumed.Body.String(), "response.completed") || !strings.Contains(resumed.Body.String(), `"sequence_number":2`) {
		t.Fatalf("resume replayed wrong events %s", resumed.Body.String())
	}
	out, count, _ := billing.result(id)
	if !out.Charge || out.Usage.CompletionTokens != 3 || count != 1 || posts.Load() != 1 {
		t.Fatalf("local billing out=%+v count=%d posts=%d", out, count, posts.Load())
	}
}

func TestBackgroundJobsUnsupportedNativeFallsBackAfterExplicitRejection(t *testing.T) {
	var calls atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if gjson.GetBytes(body, "background").Exists() {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":{"param":"background","message":"background is not supported"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, backgroundLocalEvents)
	}))
	defer up.Close()
	h, _, wrapped, repo, billing := backgroundJobsFixture(t, up.URL, "openai")
	id := createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","background":true}`)
	if err := h.Reconcile(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	job, _ := repo.GetOwned(context.Background(), id, 1, 11)
	out, count, _ := billing.result(id)
	if job.Native || !job.Billed || !out.Charge || count != 1 || calls.Load() != 2 {
		t.Fatalf("fallback job=%+v out=%+v calls=%d", job, out, calls.Load())
	}
}

type backgroundJobsNativeTransport struct{ calls *atomic.Int64 }

func (provider backgroundJobsNativeTransport) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	return (responses.Provider{}).BuildRequest(ctx, req, target)
}

func (backgroundJobsNativeTransport) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	return (responses.Provider{}).Decode(req, resp)
}

func (provider backgroundJobsNativeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	provider.calls.Add(1)
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(backgroundLocalEvents)), Request: req}, nil
}

type backgroundJobsTransportSelector struct {
	backgroundJobsNativeTransport
	selected *atomic.Bool
}

func (provider backgroundJobsTransportSelector) UpstreamTransport(_ *gateway.Request, _ http.RoundTripper) http.RoundTripper {
	provider.selected.Store(true)
	return provider.backgroundJobsNativeTransport
}

func TestBackgroundJobsLocalOptionalTransportsArePreserved(t *testing.T) {
	for _, selector := range []bool{false, true} {
		name := "roundtripper"
		if selector {
			name = "selector"
		}
		t.Run(name, func(t *testing.T) {
			h, _, wrapped, _, billing := backgroundJobsFixture(t, "http://network-must-not-be-used.invalid", "custom")
			var calls atomic.Int64
			var selected atomic.Bool
			native := backgroundJobsNativeTransport{calls: &calls}
			if selector {
				h.cfg.Providers["custom"] = backgroundJobsTransportSelector{backgroundJobsNativeTransport: native, selected: &selected}
			} else {
				h.cfg.Providers["custom"] = native
			}
			id := createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","background":true}`)
			if err := h.Reconcile(context.Background(), 10); err != nil {
				t.Fatal(err)
			}
			out, count, _ := billing.result(id)
			if calls.Load() != 1 || !out.Charge || count != 1 || (selector && !selected.Load()) {
				t.Fatalf("optional transport lost calls=%d selected=%v out=%+v", calls.Load(), selected.Load(), out)
			}
		})
	}
}
