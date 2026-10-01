package providers_test

import (
	"net/http"
	"testing"

	"github.com/tidwall/gjson"
)

func compatibleReplayFixtures() []replayFixture {
	paths := map[string]string{
		"openai": "/v1/chat/completions", "custom": "/v1/chat/completions", "openai_max": "/v1/chat/completions", "openaimax": "/v1/chat/completions",
		"ohmygpt": "/v1/chat/completions", "ails": "/v1/chat/completions", "aiproxy": "/v1/chat/completions", "api2gpt": "/v1/chat/completions",
		"aigc2d": "/v1/chat/completions", "360": "/v1/chat/completions", "openrouter": "/v1/chat/completions", "aiproxy_library": "/v1/chat/completions",
		"fastgpt": "/v1/chat/completions", "lingyiwanwu": "/v1/chat/completions", "xinference": "/v1/chat/completions", "xai": "/v1/chat/completions",
		"moonshot": "/v1/chat/completions", "deepseek": "/v1/chat/completions", "mistral": "/v1/chat/completions",
		"submodel": "/v1/chat/completions", "siliconflow": "/v1/chat/completions", "perplexity": "/chat/completions",
		"ali": "/compatible-mode/v1/chat/completions", "zhipu_4v": "/api/paas/v4/chat/completions", "zhipu_v4": "/api/paas/v4/chat/completions",
		"baidu_v2": "/v2/chat/completions", "volcengine": "/api/v3/chat/completions", "minimax": "/v1/chat/completions",
	}
	var fixtures []replayFixture
	for id, path := range paths {
		f := openAIReplayFixture(id, path)
		fixtures = append(fixtures, f)
	}
	return fixtures
}

func openAIReplayFixture(id, path string) replayFixture {
	finish := dataEvent(`{"id":"o1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
	usage := dataEvent(`{"id":"o1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`)
	done := dataEvent("[DONE]")
	return replayFixture{id: id, name: id, model: "upstream-model", secret: "test-token", path: path, contentType: "text/event-stream",
		preamble:      dataEvent(`{"id":"o1","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`),
		preambleUsage: dataEvent(`{"id":"o1","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`),
		content:       dataEvent(`{"id":"o1","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`),
		finish:        finish + done, finishUsage: finish + usage + done, empty: finish + usage + done,
		check: func(t *testing.T, request *http.Request, body []byte) {
			assertHeader(t, request, "Authorization", "Bearer test-token")
			assertJSON(t, body, "model", "upstream-model")
			assertJSON(t, body, "messages.0.content", "hello")
			assertJSON(t, body, "stream", "true")
			if id != "perplexity" {
				assertJSON(t, body, "stream_options.include_usage", "true")
			}
		},
	}
}

func assertHeader(t *testing.T, request *http.Request, key, want string) {
	t.Helper()
	if got := request.Header.Get(key); got != want {
		t.Errorf("native %s=%q; want %q", key, got, want)
	}
}

func assertJSON(t *testing.T, body []byte, path, want string) {
	t.Helper()
	if got := gjson.GetBytes(body, path).String(); got != want {
		t.Errorf("native %s=%q; want %q; body=%s", path, got, want, body)
	}
}

func forbidJSON(t *testing.T, body []byte, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if gjson.GetBytes(body, path).Exists() {
			t.Errorf("client-only field %s leaked into native body: %s", path, body)
		}
	}
}
