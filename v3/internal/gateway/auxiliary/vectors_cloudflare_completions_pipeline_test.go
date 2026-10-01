package auxiliary

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestCloudflareCompletionsGatewayConvertsAndSettles(t *testing.T) {
	for _, tc := range []struct {
		name, body                string
		stream, charge, estimated bool
		status                    int
		terminal                  gateway.Terminal
		outputTokens              int64
	}{
		{"json actual", `{"result":{"response":"Hello world","usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}}`, false, true, false, 200, gateway.TerminalCompleted, 2},
		{"json estimated text", `{"result":{"response":"Hello world"}}`, false, true, true, 200, gateway.TerminalCompletedNoUsage, 3},
		{"stream actual", "data: {\"response\":\"Hello world\"}\n\ndata: {\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\ndata: [DONE]\n\n", true, true, false, 200, gateway.TerminalCompleted, 2},
		{"stream estimated text", "data: {\"response\":\"Hello world\"}\n\ndata: [DONE]\n\n", true, true, true, 200, gateway.TerminalCompletedNoUsage, 3},
		{"native error before text", "data: {\"error\":{\"message\":\"private-error\"}}\n\n", true, false, false, 502, gateway.TerminalUpstreamErrorBeforeOutput, 0},
		{"native error after text", "data: {\"response\":\"Hello world\"}\n\ndata: {\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\ndata: {\"error\":{\"message\":\"private-error\"}}\n\n", true, true, false, 200, gateway.TerminalUpstreamErrorAfterOutput, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = io.Copy(io.Discard, r.Body)
				if tc.stream {
					w.Header().Set("Content-Type", "text/event-stream")
				} else {
					w.Header().Set("Content-Type", "application/json")
				}
				w.Header().Set("Authorization", "Bearer private-header")
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			h, plan, settle, limits := testHandler(t, server.URL)
			plan.targets[0].Provider = "cloudflare"
			plan.targets[0].Secret = "account|provider-key"
			plan.targets[0].UpstreamModel = "@cf/meta/llama"
			body := `{"model":"alias","prompt":"Hello"}`
			if tc.stream {
				body = `{"model":"alias","prompt":"Hello","stream":true,"stream_options":{"include_usage":true}}`
			}
			w := invoke(h, "/v1/completions", body)
			if w.Code != tc.status || calls != 1 || !settle.finalized || settle.reserves != 1 || settle.out.Charge != tc.charge || settle.out.Terminal != tc.terminal || limits.acquired != limits.released {
				t.Fatalf("status=%d calls=%d settlement=%+v limits=%+v", w.Code, calls, settle, limits)
			}
			if tc.charge && (settle.out.Usage.Estimated != tc.estimated || settle.out.Usage.CompletionTokens != tc.outputTokens) {
				t.Fatalf("usage=%+v want estimated=%v output=%d", settle.out.Usage, tc.estimated, tc.outputTokens)
			}
			if tc.charge && (!strings.Contains(w.Body.String(), `"text":"Hello world"`) || strings.Contains(w.Body.String(), `"response":`) || strings.Contains(w.Body.String(), `"delta":`)) {
				t.Fatalf("native stream shape leaked: %s", w.Body.String())
			}
			if w.Header().Get("Authorization") != "" || strings.Contains(w.Body.String(), "private-error") {
				t.Fatal("private upstream details leaked")
			}
		})
	}
}

func TestCloudflareCompletionsDoesNotRetryAfterPartialOutput(t *testing.T) {
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"response\":\"Hello world\"}\n\n")
	}))
	defer broken.Close()
	var retries atomic.Int64
	retry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retries.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"response\":\"retried\"}\n\ndata: [DONE]\n\n")
	}))
	defer retry.Close()
	h, plan, settle, limits := testHandler(t, broken.URL, retry.URL)
	for index := range plan.targets {
		plan.targets[index].Provider = "cloudflare"
		plan.targets[index].Secret = "account|key"
		plan.targets[index].UpstreamModel = "@cf/meta/llama"
	}
	w := invoke(h, "/v1/completions", `{"model":"alias","prompt":"Hello","stream":true}`)
	if w.Code != 200 || retries.Load() != 0 || strings.Contains(w.Body.String(), "[DONE]") || !strings.Contains(w.Body.String(), `"text":"Hello world"`) {
		t.Fatalf("partial output status=%d retries=%d body=%s", w.Code, retries.Load(), w.Body.String())
	}
	if !settle.out.Charge || settle.out.Terminal != gateway.TerminalUpstreamErrorAfterOutput || !settle.out.Usage.Estimated || settle.out.Usage.CompletionTokens != 3 || settle.reserves != 1 || !settle.finalized || limits.acquired != 1 || limits.released != 1 {
		t.Fatalf("partial-output settlement=%+v limits=%+v", settle, limits)
	}
}
