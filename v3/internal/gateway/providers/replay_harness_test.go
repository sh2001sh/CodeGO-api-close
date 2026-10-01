package providers_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type replayAuth struct{}

func (replayAuth) Authorize(_ context.Context, key string) (gateway.Principal, error) {
	if key != "replay-key" {
		return gateway.Principal{}, gateway.ErrInvalidKey
	}
	return gateway.Principal{UserID: 7, KeyID: 70, Group: "default"}, nil
}

type replayPlanner struct {
	targets []gateway.Target
	mu      sync.Mutex
	reports []gateway.AttemptResult
}

func (p *replayPlanner) Plan(context.Context, *gateway.Request) ([]gateway.Target, error) {
	return p.targets, nil
}
func (p *replayPlanner) Report(_ gateway.Target, result gateway.AttemptResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reports = append(p.reports, result)
}
func (p *replayPlanner) results() []gateway.AttemptResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]gateway.AttemptResult(nil), p.reports...)
}

type replaySettler struct{ outcomes chan gateway.Outcome }

func (*replaySettler) Reserve(context.Context, *gateway.Request) error { return nil }
func (s *replaySettler) Finalize(_ context.Context, _ *gateway.Request, out gateway.Outcome) error {
	s.outcomes <- out
	return nil
}

type replayHarness struct {
	url        string
	planner    *replayPlanner
	settler    *replaySettler
	calls      atomic.Int32
	clientLeft chan struct{}
}

func newReplayHarness(t *testing.T, registry map[string]gateway.Provider, f replayFixture, mode string) *replayHarness {
	t.Helper()
	h := &replayHarness{clientLeft: make(chan struct{}), settler: &replaySettler{outcomes: make(chan gateway.Outcome, 2)}}
	handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		h.calls.Add(1)
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		validateReplayRequest(t, f, request, body)
		if mode == "rate_limited" {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			_, _ = io.WriteString(w, `{"error":{"message":"try later","code":"rate_limit"}}`)
			return
		}
		w.Header().Set("Content-Type", f.contentType)
		prefix := f.preamble + f.content
		if mode == "completed" || mode == "client_canceled" {
			prefix = f.preambleUsage + f.content
		}
		if mode == "empty" {
			_, _ = io.WriteString(w, f.empty)
			return
		}
		_, _ = io.WriteString(w, prefix)
		w.(http.Flusher).Flush()
		switch mode {
		case "truncated_after_output":
			return // EOF before the native terminal is a cut.
		case "timeout_after_output":
			<-request.Context().Done()
			return
		case "client_canceled":
			select {
			case <-h.clientLeft:
			case <-request.Context().Done():
				return
			}
			// Allow the detached upstream to drain after client cancellation is observed.
			select {
			case <-time.After(50 * time.Millisecond):
			case <-request.Context().Done():
				return
			}
		}
		tail := f.finish
		if mode == "completed" || mode == "client_canceled" {
			tail = f.finishUsage
		}
		_, _ = io.WriteString(w, tail)
	})
	var upstream *httptest.Server
	if f.websocket {
		upstream = replayWebsocketServer(t, h, f, mode)
	} else {
		upstream = httptest.NewServer(handler)
	}
	t.Cleanup(upstream.Close)
	h.planner = &replayPlanner{}
	for i := int64(1); i <= 2; i++ {
		h.planner.targets = append(h.planner.targets, gateway.Target{ChannelID: i, Provider: f.id, BaseURL: upstream.URL, Secret: f.secret, UpstreamModel: f.model})
	}
	cfg := gateway.Config{HeaderTimeout: time.Second, RelayTimeout: 2 * time.Second, DrainTimeout: time.Second}
	if mode == "timeout_after_output" {
		cfg.RelayTimeout = 100 * time.Millisecond
	}
	g, err := gateway.New(gateway.Deps{Config: cfg, Authorizer: replayAuth{}, Planner: h.planner, Settler: h.settler, Providers: registry})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	h.url = server.URL
	return h
}

func validateReplayRequest(t *testing.T, f replayFixture, request *http.Request, body []byte) {
	t.Helper()
	if request.Method != http.MethodPost || request.URL.Path != f.path {
		t.Errorf("native destination=%s %s; want POST %s", request.Method, request.URL.Path, f.path)
	}
	if request.Header.Get("Content-Type") != "application/json" {
		t.Errorf("request content type=%q", request.Header.Get("Content-Type"))
	}
	if f.check != nil {
		f.check(t, request, body)
	}
}

func dataEvent(data string) string        { return "data: " + data + "\n\n" }
func namedEvent(name, data string) string { return "event: " + name + "\n" + dataEvent(data) }
