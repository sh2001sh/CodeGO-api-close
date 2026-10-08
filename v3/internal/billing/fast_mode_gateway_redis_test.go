//go:build pgintegration

package billing

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/codex"
	"github.com/tidwall/gjson"
)

type fastGatewayAuth struct{}

func (fastGatewayAuth) Authorize(context.Context, string) (gateway.Principal, error) {
	return gateway.Principal{UserID: 7, KeyID: 70, Group: "default"}, nil
}

type fastGatewayPlanner struct{ target gateway.Target }

func (p fastGatewayPlanner) Plan(context.Context, *gateway.Request) ([]gateway.Target, error) {
	return []gateway.Target{p.target}, nil
}
func (fastGatewayPlanner) Report(gateway.Target, gateway.AttemptResult) {}

func TestCodexFastEndToEndWithRedis(t *testing.T) {
	for _, tc := range []struct {
		name, path, tier string
		stream           bool
		funds, want      int64
		status           int
		called           bool
	}{
		{"responses fast JSON", "/v1/responses", "fast", false, 1000, 96, 200, true},
		{"responses priority SSE", "/v1/responses", "priority", true, 1000, 96, 200, true},
		{"responses downgrade SSE", "/v1/responses", "default", true, 1000, 48, 200, true},
		{"chat fast JSON", "/v1/chat/completions", "fast", false, 1000, 96, 200, true},
		{"chat downgrade SSE", "/v1/chat/completions", "default", true, 1000, 48, 200, true},
		{"missing response tier", "/v1/responses", "", true, 1000, 96, 200, true},
		{"insufficient before upstream", "/v1/responses", "fast", false, 300, 0, 402, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, rdb, _, _ := setup(t, tc.funds)
			var called atomic.Bool
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called.Store(true)
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				if r.URL.Path != "/backend-api/codex/responses" || gjson.GetBytes(data, "service_tier").Str != "fast" {
					t.Errorf("request path=%s body=%s", r.URL.Path, data)
				}
				tier := ""
				if tc.tier != "" {
					tier = `,"service_tier":"` + tc.tier + `"`
				}
				response := `{"id":"resp_test","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}],"usage":{"input_tokens":10,"output_tokens":20,"input_tokens_details":{"cached_tokens":2}}` + tier + `}`
				if tc.stream {
					w.Header().Set("Content-Type", "text/event-stream")
					initial := `{"service_tier":"fast"}`
					if tc.tier == "" {
						initial = `{}`
					}
					_, _ = fmt.Fprintf(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":%s}\n\nevent: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":%s}\n\n", initial, response)
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, response)
				}
			}))
			defer upstream.Close()
			h, err := gateway.New(gateway.Deps{Authorizer: fastGatewayAuth{}, Planner: fastGatewayPlanner{gateway.Target{ChannelID: 3, CredentialID: 300, Provider: "codex", BaseURL: upstream.URL, Secret: `{"access_token":"fixture","account_id":"fixture"}`}}, Settler: s, Providers: map[string]gateway.Provider{"codex": codex.Provider{}}})
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			h.Register(mux)
			input := `"input":"hi"`
			maxField := "max_output_tokens"
			if tc.path == "/v1/chat/completions" {
				input = `"messages":[{"role":"user","content":"hi"}]`
				maxField = "max_tokens"
			}
			body := fmt.Sprintf(`{"model":"gpt",%s,"%s":100,"stream":%t,"service_tier":"fast"}`, input, maxField, tc.stream)
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer fixture")
			req.Header.Set("Content-Type", "application/json")
			view := httptest.NewRecorder()
			mux.ServeHTTP(view, req)
			if view.Code != tc.status || called.Load() != tc.called {
				t.Fatalf("status=%d called=%v body=%s", view.Code, called.Load(), view.Body.String())
			}
			if b, r := balance(t, rdb); b != tc.funds-tc.want || r != 0 {
				t.Fatalf("balance=%d hold=%d; want %d/0", b, r, tc.funds-tc.want)
			}
			ev := events(t, rdb)
			if !tc.called {
				if len(ev) != 0 {
					t.Fatal("denied request charged")
				}
				return
			}
			if len(ev) != 1 || ev[0][FieldAmount] != fmt.Sprint(tc.want) {
				t.Fatalf("billing events=%v", ev)
			}
			if tc.tier != "" && ev[0]["service_tier"] != tc.tier {
				t.Fatalf("actual tier was lost: %v", ev)
			}
			if tc.path == "/v1/chat/completions" && tc.tier != "" && !strings.Contains(view.Body.String(), `"service_tier":"`+tc.tier+`"`) {
				t.Fatalf("client cannot observe actual tier: %s", view.Body.String())
			}
		})
	}
}
