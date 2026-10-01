package providers_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers"
)

func palmReplayFixture() replayFixture {
	return replayFixture{id: "palm", name: "palm", model: "chat-bison-001", secret: "test-token", path: "/v1beta2/models/chat-bison-001:generateMessage", contentType: "application/json",
		content: `{"candidates":[{"author":"assistant","content":"hello"}]}`, empty: `{"candidates":[]}`,
		check: func(t *testing.T, r *http.Request, b []byte) {
			if r.URL.Query().Get("key") != "test-token" {
				t.Error("PaLM API key lost")
			}
			assertHeader(t, r, "Accept", "application/json")
			assertJSON(t, b, "prompt.messages.0.content", "hello")
			forbidJSON(t, b, "model", "stream", "stream_options", "messages")
		},
	}
}

// PaLM generateMessage returns one JSON response and no token counts. Actual
// usage completion and upstream failure after visible partial output are
// intrinsically unavailable; it must never fabricate either state.
func TestProviderReplayPaLMIntrinsicStates(t *testing.T) {
	registry := providers.Registry()
	if registry["palm"] == nil {
		t.Fatal("importable provider palm is absent")
	}
	f := palmReplayFixture()
	for _, tc := range []replayCase{replayCases[1], replayCases[2], replayCases[4]} {
		t.Run(tc.name, func(t *testing.T) { runProviderReplay(t, registry, f, tc) })
	}
	for _, mode := range []string{"client_canceled_before_output", "timeout_before_output"} {
		t.Run(mode, func(t *testing.T) { replayPaLMInterrupted(t, registry, f, mode) })
	}
}

func replayPaLMInterrupted(t *testing.T, registry map[string]gateway.Provider, f replayFixture, mode string) {
	started := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		validateReplayRequest(t, f, r, b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[`)
		w.(http.Flusher).Flush()
		started <- struct{}{}
		<-r.Context().Done()
	}))
	t.Cleanup(upstream.Close)
	planner := &replayPlanner{targets: []gateway.Target{{ChannelID: 1, Provider: "palm", BaseURL: upstream.URL, Secret: f.secret, UpstreamModel: f.model}}}
	settler := &replaySettler{outcomes: make(chan gateway.Outcome, 1)}
	cfg := gateway.Config{HeaderTimeout: time.Second, RelayTimeout: 2 * time.Second, DrainTimeout: 50 * time.Millisecond}
	if mode == "timeout_before_output" {
		cfg.RelayTimeout = 100 * time.Millisecond
	}
	g, err := gateway.New(gateway.Deps{Config: cfg, Authorizer: replayAuth{}, Planner: planner, Settler: settler, Providers: registry})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"public-alias","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	request.Header.Set("Authorization", "Bearer replay-key")
	clientDone := make(chan error, 1)
	go func() {
		response, err := http.DefaultClient.Do(request)
		if response != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		clientDone <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("PaLM native exchange did not start")
	}
	want := gateway.TerminalTimeout
	if mode == "client_canceled_before_output" {
		cancel()
		want = gateway.TerminalClientCanceled
	}
	select {
	case out := <-settler.outcomes:
		if out.Terminal != want || out.Delivered || out.Charge {
			t.Errorf("PaLM interruption=%s delivered=%v charge=%v; want %s refund", out.Terminal, out.Delivered, out.Charge, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("PaLM interrupted request did not finalize")
	}
	select {
	case <-clientDone:
	case <-time.After(time.Second):
		t.Fatal("PaLM client exchange leaked")
	}
}
