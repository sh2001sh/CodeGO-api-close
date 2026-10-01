package auxiliary

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestCompatibleInvalidOutputRefunds(t *testing.T) {
	for _, test := range []struct{ name, path, request, response string }{
		{"empty image", "/v1/images/generations", `{"model":"image","prompt":"cat"}`, `{"data":[{}]}`},
		{"invalid vector", "/v1/embeddings", `{"model":"embedding","input":"hello"}`, `{"data":[{"embedding":["bad"]}]}`},
		{"negative accounting", "/v1/embeddings", `{"model":"embedding","input":"hello"}`, `{"data":[{"embedding":[1]}],"usage":{"prompt_tokens":-1}}`},
		{"empty completion", "/v1/completions", `{"model":"completion","prompt":"hello"}`, `{"choices":[{"text":"","finish_reason":"stop"}],"usage":{"prompt_tokens":3}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, test.response) }))
			defer server.Close()
			h, _, s, _ := testHandler(t, server.URL)
			w := invoke(h, test.path, test.request)
			if w.Code != 502 || s.out.Charge || s.out.Delivered {
				t.Fatalf("status=%d outcome=%+v", w.Code, s.out)
			}
		})
	}
}

func TestLegacyTextEditsUseInstructionAndTextUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/edits" {
			t.Errorf("path=%s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"choices":[{"text":"fixed text"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
	}))
	defer server.Close()
	h, _, s, _ := testHandler(t, server.URL)
	w := invoke(h, "/v1/edits", `{"model":"edit","input":"fixx text","instruction":"Fix spelling"}`)
	if w.Code != 200 || !s.out.Charge || s.out.Usage.CompletionTokens != 2 || s.out.Usage.ImageCount != 0 {
		t.Fatalf("status=%d outcome=%+v", w.Code, s.out)
	}
	w = invoke(h, "/v1/edits", `{"model":"edit","prompt":"wrong input"}`)
	if w.Code != 400 || s.out.Charge {
		t.Fatalf("status=%d outcome=%+v", w.Code, s.out)
	}
}

func TestFinishOnlySSERefundsAndTruncatedOutputChargesConsumed(t *testing.T) {
	for _, test := range []struct {
		name, response string
		terminal       gateway.Terminal
		charge         bool
		status         int
	}{
		{"finish only", "data: {\"choices\":[{\"text\":\"\",\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", gateway.TerminalEmptyStream, false, 502},
		{"negative initial usage", "data: {\"choices\":[{\"text\":\"hello\"}],\"usage\":{\"prompt_tokens\":-1}}\n\ndata: [DONE]\n\n", gateway.TerminalUpstreamErrorBeforeOutput, false, 502},
		{"truncated text", "data: {\"choices\":[{\"text\":\"hello\"}]}\n\n", gateway.TerminalUpstreamErrorAfterOutput, true, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, test.response)
			}))
			defer server.Close()
			h, _, s, _ := testHandler(t, server.URL)
			w := invoke(h, "/v1/completions", `{"model":"completion","prompt":"hi","stream":true}`)
			if w.Code != test.status || s.out.Terminal != test.terminal || s.out.Charge != test.charge {
				t.Fatalf("status=%d outcome=%+v", w.Code, s.out)
			}
		})
	}
}
