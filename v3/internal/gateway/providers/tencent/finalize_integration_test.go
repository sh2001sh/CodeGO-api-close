package tencent_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers"
)

type signingAuth struct{}

func (signingAuth) Authorize(context.Context, string) (gateway.Principal, error) {
	return gateway.Principal{UserID: 1, KeyID: 1, Group: "default"}, nil
}

type signingPlanner struct{ target gateway.Target }

func (p signingPlanner) Plan(context.Context, *gateway.Request) ([]gateway.Target, error) {
	return []gateway.Target{p.target}, nil
}
func (signingPlanner) Report(gateway.Target, gateway.AttemptResult) {}

type signingSettler struct{ outcomes chan gateway.Outcome }

func (signingSettler) Reserve(context.Context, *gateway.Request) error { return nil }
func (s signingSettler) Finalize(_ context.Context, _ *gateway.Request, out gateway.Outcome) error {
	s.outcomes <- out
	return nil
}

func TestActualBridgedRegistryFinalizesTencentAfterOverrides(t *testing.T) {
	const auth = "TC3-HMAC-SHA256 Credential=test-id/2023-11-14/hunyuan/tc3_request, SignedHeaders=content-type;host;x-tc-action, Signature=d6b033648d2abe0dc83908ca2a6769e7a1100ce66c945e9114c1d6c5202f80cf"
	const body = `{"Model":"hunyuan-lite","Messages":[{"Role":"user","Content":"hello"}],"Stream":false,"Temperature":0.25}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual, err := io.ReadAll(r.Body)
		if err != nil || string(actual) != body || r.Header.Get("Authorization") != auth || r.Host != "signing.example.test" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"Response":{"Choices":[{"Message":{"Role":"assistant","Content":"signed reply"},"FinishReason":"stop"}],"Usage":{"PromptTokens":3,"CompletionTokens":2}}}`)
	}))
	defer upstream.Close()
	target := gateway.Target{Provider: "tencent", BaseURL: upstream.URL, Secret: "123|test-id|test-secret", UpstreamModel: "hunyuan-lite", ParamOverride: map[string]any{"Temperature": 0.25}, HeaderOverride: map[string]string{"Content-Type": "application/json; charset=utf-8", "X-TC-Action": "OtherAction", "X-TC-Timestamp": "1700000000", "Host": "signing.example.test"}}
	outcomes := make(chan gateway.Outcome, 1)
	relay, err := gateway.New(gateway.Deps{Authorizer: signingAuth{}, Planner: signingPlanner{target}, Settler: signingSettler{outcomes}, Providers: providers.Registry()})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	relay.Register(mux)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"alias","messages":[{"role":"user","content":"hello"}],"temperature":0.5}`))
	request.Header.Set("Authorization", "Bearer test-client")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "signed reply") {
		t.Fatalf("real gateway bridge did not sign final override bytes: %d %s", response.Code, response.Body.String())
	}
	outcome := <-outcomes
	if outcome.Terminal != gateway.TerminalCompleted || outcome.Usage.CompletionTokens != 2 || !outcome.Charge {
		t.Fatalf("signed native result not settled: %+v", outcome)
	}
}
