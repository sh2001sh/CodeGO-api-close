package gateway_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/bench/mockupstream"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

type harness struct {
	t       *testing.T
	gw      *httptest.Server
	planner *fakePlanner
	settler *fakeSettler
}

// newHarness starts the mock upstream and a gateway whose RoutePlan is one
// channel per scenario path, e.g. "error_before", "complete/c4".
func newHarness(t *testing.T, cfg gateway.Config, scenarios ...string) *harness {
	t.Helper()
	upstream := httptest.NewServer(mockupstream.Handler())
	t.Cleanup(upstream.Close)

	planner := &fakePlanner{}
	for i, s := range scenarios {
		planner.targets = append(planner.targets, gateway.Target{
			ChannelID: int64(i + 1), Provider: openai.ID, Secret: "upstream-secret",
			BaseURL: upstream.URL + "/m/" + s,
		})
	}
	settler := newSettler()
	g, err := gateway.New(gateway.Deps{
		Config: cfg, Authorizer: fakeAuth{}, Planner: planner, Settler: settler,
		Providers: map[string]gateway.Provider{openai.ID: openai.Provider{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	gw := httptest.NewServer(mux)
	t.Cleanup(gw.Close)
	return &harness{t: t, gw: gw, planner: planner, settler: settler}
}

const streamBody = `{"model":"gpt-test","stream":true,"messages":[{"role":"user","content":"hi"}]}`

func (h *harness) post(ctx context.Context, body string) *http.Response {
	h.t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, h.gw.URL+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer sk-test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	return resp
}

type clientView struct {
	status int
	data   []string // SSE data payloads in order
	body   string   // raw body for non-SSE responses
}

func (h *harness) do(body string) clientView {
	h.t.Helper()
	resp := h.post(context.Background(), body)
	defer func() { _ = resp.Body.Close() }()
	view := clientView{status: resp.StatusCode}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		b, _ := io.ReadAll(resp.Body)
		view.body = string(b)
		return view
	}
	r := sse.NewReader(resp.Body, 0)
	for {
		ev, err := r.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				h.t.Fatalf("client stream error: %v", err)
			}
			return view
		}
		view.data = append(view.data, string(ev.Data))
	}
}

func (h *harness) outcome() gateway.Outcome {
	h.t.Helper()
	select {
	case out := <-h.settler.outcomes:
		return out
	case <-time.After(10 * time.Second):
		h.t.Fatal("no outcome finalized")
		return gateway.Outcome{}
	}
}

func contentEvents(data []string) int {
	n := 0
	for _, d := range data {
		if strings.Contains(d, `"delta":{"content"`) {
			n++
		}
	}
	return n
}
