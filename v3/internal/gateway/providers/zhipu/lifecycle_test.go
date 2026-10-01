package zhipu

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

type testAuth struct{}

func (testAuth) Authorize(context.Context, string) (gateway.Principal, error) {
	return gateway.Principal{UserID: 1, KeyID: 1}, nil
}

type testPlanner struct{ targets []gateway.Target }

func (p testPlanner) Plan(context.Context, *gateway.Request) ([]gateway.Target, error) {
	return p.targets, nil
}

func (testPlanner) Report(gateway.Target, gateway.AttemptResult) {}

type testSettler struct{ result chan gateway.Outcome }

func (testSettler) Reserve(context.Context, *gateway.Request) error { return nil }
func (s testSettler) Finalize(_ context.Context, _ *gateway.Request, out gateway.Outcome) error {
	s.result <- out
	return nil
}

func gatewayFixture(t *testing.T, handler http.HandlerFunc, config gateway.Config) (string, <-chan gateway.Outcome) {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	result := make(chan gateway.Outcome, 1)
	g, err := gateway.New(gateway.Deps{Config: config, Authorizer: testAuth{},
		Planner: testPlanner{targets: []gateway.Target{{ChannelID: 1, CredentialID: 1, Provider: ID, BaseURL: upstream.URL, Secret: "test-id.test-secret"}}},
		Settler: testSettler{result: result}, Providers: map[string]gateway.Provider{ID: Provider{}}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL, result
}

func awaitOutcome(t *testing.T, result <-chan gateway.Outcome) gateway.Outcome {
	t.Helper()
	select {
	case out := <-result:
		return out
	case <-time.After(3 * time.Second):
		t.Fatal("gateway did not settle")
		return gateway.Outcome{}
	}
}

func postStream(t *testing.T, ctx context.Context, base string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/chat/completions", strings.NewReader(`{"model":"chatglm_turbo","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer sk-test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestGatewayNativeTerminalAndBillingMatrix(t *testing.T) {
	add := "event: add\ndata: hello\n\n"
	meta := "event: finish\nmeta: {\"task_status\":\"SUCCESS\",\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":3}}\n\n"
	accounting := "event: meta\ndata: {\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":3}}\n\n"
	errorEvent := "event: error\ndata: {\"code\":500,\"msg\":\"busy\"}\n\n"
	for _, tc := range []struct {
		name, wire string
		terminal   gateway.Terminal
		charge     bool
		estimated  bool
	}{
		{"completed", add + meta, gateway.TerminalCompleted, true, false},
		{"no usage", add + "event: finish\n\n", gateway.TerminalCompletedNoUsage, true, true},
		{"error before", errorEvent, gateway.TerminalUpstreamErrorBeforeOutput, false, false},
		{"error after", add + errorEvent, gateway.TerminalUpstreamErrorAfterOutput, true, true},
		{"cut before", "", gateway.TerminalUpstreamErrorBeforeOutput, false, false},
		{"cut after", add, gateway.TerminalUpstreamErrorAfterOutput, true, true},
		{"accounted error after", add + accounting + errorEvent, gateway.TerminalUpstreamErrorAfterOutput, true, false},
		{"accounted cut after", add + accounting, gateway.TerminalUpstreamErrorAfterOutput, true, false},
		{"empty", meta, gateway.TerminalEmptyStream, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, result := gatewayFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/paas/v3/model-api/chatglm_turbo/sse-invoke" {
					t.Errorf("upstream path = %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, tc.wire)
			}, gateway.Config{})
			resp := postStream(t, context.Background(), base)
			body, _ := io.ReadAll(resp.Body)
			out := awaitOutcome(t, result)
			if out.Terminal != tc.terminal || out.Charge != tc.charge || out.Usage.Estimated != tc.estimated {
				t.Fatalf("body=%s outcome=%+v", body, out)
			}
			if tc.charge && !tc.estimated && (out.Usage.PromptTokens != 9 || out.Usage.CompletionTokens != 3) {
				t.Fatalf("billing usage = %+v", out.Usage)
			}
			if (tc.terminal == gateway.TerminalCompleted || tc.terminal == gateway.TerminalCompletedNoUsage) && !strings.Contains(string(body), "[DONE]") {
				t.Fatalf("missing client completion: %s", body)
			}
			if tc.terminal == gateway.TerminalUpstreamErrorAfterOutput && strings.Contains(string(body), "[DONE]") {
				t.Fatalf("false completion after failure: %s", body)
			}
		})
	}
}

func TestGatewayNativeTimeoutBeforeAndAfterOutput(t *testing.T) {
	for _, output := range []bool{false, true} {
		base, result := gatewayFixture(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			if output {
				_, _ = io.WriteString(w, "event: add\ndata: hello\n\n")
			}
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}, gateway.Config{RelayTimeout: 50 * time.Millisecond, HeaderTimeout: time.Second})
		resp := postStream(t, context.Background(), base)
		_, _ = io.ReadAll(resp.Body)
		out := awaitOutcome(t, result)
		if out.Terminal != gateway.TerminalTimeout || out.Delivered != output || out.Charge != output {
			t.Fatalf("output=%v outcome=%+v", output, out)
		}
	}
}

func TestGatewayNativeClientCancelDrainsExactUsage(t *testing.T) {
	base, result := gatewayFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 30; i++ {
			_, _ = io.WriteString(w, "event: add\ndata: hello\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(time.Millisecond)
		}
		_, _ = io.WriteString(w, "event: finish\nmeta: {\"task_status\":\"SUCCESS\",\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":30}}\n\n")
	}, gateway.Config{DrainTimeout: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp := postStream(t, ctx, base)
	if _, err := sse.NewReader(resp.Body, 0).Next(); err != nil {
		t.Fatal(err)
	}
	cancel()
	_ = resp.Body.Close()
	out := awaitOutcome(t, result)
	if out.Terminal != gateway.TerminalClientCanceled || !out.Delivered || !out.Charge || out.Usage.Estimated || out.Usage.PromptTokens != 9 || out.Usage.CompletionTokens != 30 {
		t.Fatalf("canceled outcome = %+v", out)
	}
}
