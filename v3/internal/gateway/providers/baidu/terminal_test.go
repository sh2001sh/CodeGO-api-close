package baidu

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

type testAuthorizer struct{}

func (testAuthorizer) Authorize(context.Context, string) (gateway.Principal, error) {
	return gateway.Principal{UserID: 1, KeyID: 1}, nil
}

type testPlanner struct{ target gateway.Target }

func (p testPlanner) Plan(context.Context, *gateway.Request) ([]gateway.Target, error) {
	return []gateway.Target{p.target}, nil
}
func (testPlanner) Report(gateway.Target, gateway.AttemptResult) {}

type testSettler struct{ result chan gateway.Outcome }

func (testSettler) Reserve(context.Context, *gateway.Request) error { return nil }
func (s testSettler) Finalize(_ context.Context, _ *gateway.Request, out gateway.Outcome) error {
	s.result <- out
	return nil
}

func newGatewayFixture(t *testing.T, handler http.HandlerFunc, timeout time.Duration) (*httptest.Server, chan gateway.Outcome) {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	outcomes := make(chan gateway.Outcome, 1)
	g, err := gateway.New(gateway.Deps{Config: gateway.Config{RelayTimeout: timeout, DrainTimeout: time.Second, HeaderTimeout: time.Second},
		Authorizer: testAuthorizer{}, Planner: testPlanner{gateway.Target{Provider: ID, BaseURL: upstream.URL, Secret: "token"}},
		Settler: testSettler{outcomes}, Providers: map[string]gateway.Provider{ID: Provider{}}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, outcomes
}

func postChat(t *testing.T, ctx context.Context, server *httptest.Server) *http.Response {
	t.Helper()
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"ERNIE-Bot","messages":[{"role":"user","content":"hi"}],"stream":true}`))
	request.Header.Set("Authorization", "Bearer sk-test")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func awaitOutcome(t *testing.T, outcomes <-chan gateway.Outcome) gateway.Outcome {
	t.Helper()
	select {
	case out := <-outcomes:
		return out
	case <-time.After(3 * time.Second):
		t.Fatal("gateway did not finalize")
		return gateway.Outcome{}
	}
}

func TestBaiduGatewayTerminalDecisionTable(t *testing.T) {
	partial := "data: " + `{"result":"hi"}` + "\n\n"
	errorFrame := "data: " + `{"error_code":18,"error_msg":"limit reached"}` + "\n\n"
	for _, tc := range []struct {
		name     string
		body     string
		terminal gateway.Terminal
		charge   bool
		estimate bool
	}{
		{"completed", partial + "data: " + `{"is_end":true,"usage":{"prompt_tokens":4,"total_tokens":7}}` + "\n\n", gateway.TerminalCompleted, true, false},
		{"completed_no_usage", partial + "data: " + `{"is_end":true}` + "\n\n", gateway.TerminalCompletedNoUsage, true, true},
		{"error_before_output", errorFrame, gateway.TerminalUpstreamErrorBeforeOutput, false, false},
		{"error_after_output", partial + errorFrame, gateway.TerminalUpstreamErrorAfterOutput, true, true},
		{"cut_after_output", partial, gateway.TerminalUpstreamErrorAfterOutput, true, true},
		{"empty", "data: " + `{"result":"","is_end":true,"usage":{"prompt_tokens":4,"total_tokens":4}}` + "\n\n", gateway.TerminalEmptyStream, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, outcomes := newGatewayFixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, tc.body)
			}, time.Second)
			response := postChat(t, context.Background(), server)
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			out := awaitOutcome(t, outcomes)
			if out.Terminal != tc.terminal || out.Charge != tc.charge || out.Usage.Estimated != tc.estimate {
				t.Fatalf("terminal/billing incorrect: %+v", out)
			}
			if tc.name == "completed" && (out.Usage.PromptTokens != 4 || out.Usage.CompletionTokens != 3) {
				t.Fatalf("reported usage lost: %+v", out)
			}
		})
	}
}

func TestBaiduGatewayTimeoutAfterOutput(t *testing.T) {
	server, outcomes := newGatewayFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: "+`{"result":"hi"}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}, 80*time.Millisecond)
	response := postChat(t, context.Background(), server)
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	out := awaitOutcome(t, outcomes)
	if out.Terminal != gateway.TerminalTimeout || !out.Delivered || !out.Charge || !out.Usage.Estimated {
		t.Fatalf("timeout misclassified: %+v", out)
	}
}

func TestBaiduGatewayClientCancellationDrainsReportedUsage(t *testing.T) {
	release := make(chan struct{})
	server, outcomes := newGatewayFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: "+`{"result":"hi"}`+"\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = fmt.Fprint(w, "data: "+`{"is_end":true,"usage":{"prompt_tokens":4,"total_tokens":7}}`+"\n\n")
	}, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	response := postChat(t, ctx, server)
	reader := sse.NewReader(response.Body, 0)
	if _, err := reader.Next(); err != nil {
		t.Fatal(err)
	}
	cancel()
	_ = response.Body.Close()
	// Allow the gateway's request cancellation observer to start draining.
	time.Sleep(25 * time.Millisecond)
	close(release)
	out := awaitOutcome(t, outcomes)
	if out.Terminal != gateway.TerminalClientCanceled || !out.Charge || out.Usage.Estimated || out.Usage.CompletionTokens != 3 {
		t.Fatalf("canceled stream lost drained usage: %+v", out)
	}
}
