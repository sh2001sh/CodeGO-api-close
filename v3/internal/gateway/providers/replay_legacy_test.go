package providers_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func otherNativeReplayFixtures() []replayFixture {
	fixtures := []replayFixture{baiduReplayFixture(), zhipuReplayFixture(), tencentReplayFixture(), difyReplayFixture(), cozeReplayFixture(), cloudflareReplayFixture(), cloudflareResponsesReplayFixture(), sparkReplayFixture(), bedrockNovaReplayFixture()}
	for _, family := range []string{"google", "anthropic", "open-source"} {
		fixtures = append(fixtures, vertexReplayFixture(family))
	}
	return fixtures
}

func baiduReplayFixture() replayFixture {
	finish := dataEvent(`{"id":"b1","result":"","is_end":true}`)
	return replayFixture{id: "baidu", name: "baidu", model: "ERNIE-4.0", secret: "test-token", path: "/rpc/2.0/ai_custom/v1/wenxinworkshop/chat/completions_pro", contentType: "text/event-stream",
		content: dataEvent(`{"id":"b1","result":"hello","is_end":false}`), finish: finish, empty: finish,
		finishUsage: dataEvent(`{"id":"b1","result":"","is_end":true,"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`),
		check: func(t *testing.T, r *http.Request, b []byte) {
			if r.URL.Query().Get("access_token") != "test-token" {
				t.Errorf("Baidu OAuth access token lost")
			}
			assertJSON(t, b, "messages.0.content", "hello")
			assertJSON(t, b, "stream", "true")
			forbidJSON(t, b, "model", "stream_options")
		},
	}
}

func zhipuReplayFixture() replayFixture {
	finish := "event: finish\ndata: \n\n"
	return replayFixture{id: "zhipu", name: "zhipu", model: "chatglm_turbo", secret: "test-id.test-signing-secret", path: "/api/paas/v3/model-api/chatglm_turbo/sse-invoke", contentType: "text/event-stream",
		content: "event: add\ndata: hello\n\n", finish: finish, empty: finish,
		finishUsage: "event: finish\ndata: \nmeta: {\"task_id\":\"z1\",\"task_status\":\"SUCCESS\",\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":3}}\n\n",
		check: func(t *testing.T, r *http.Request, b []byte) {
			parts := strings.Split(r.Header.Get("Authorization"), ".")
			if len(parts) != 3 {
				t.Error("Zhipu JWT missing")
				return
			}
			mac := hmac.New(sha256.New, []byte("test-signing-secret"))
			_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
			if parts[2] != base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) {
				t.Error("Zhipu JWT signature invalid")
			}
			claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
			assertJSON(t, claims, "api_key", "test-id")
			assertJSON(t, b, "prompt.0.content", "hello")
			forbidJSON(t, b, "model", "messages", "stream_options")
		},
	}
}

func tencentReplayFixture() replayFixture {
	finish := dataEvent(`{"Choices":[{"Delta":{"Content":""},"FinishReason":"stop"}]}`)
	return replayFixture{id: "tencent", name: "tencent", model: "hunyuan-lite", secret: "123|test-id|test-signing-secret", path: "/", contentType: "text/event-stream",
		content: dataEvent(`{"Id":"t1","Choices":[{"Delta":{"Role":"assistant","Content":"hello"}}]}`), finish: finish, empty: finish,
		finishUsage: finish + dataEvent(`{"Usage":{"PromptTokens":10,"CompletionTokens":3,"TotalTokens":13}}`),
		check: func(t *testing.T, r *http.Request, b []byte) {
			if !strings.HasPrefix(r.Header.Get("Authorization"), "TC3-HMAC-SHA256 Credential=test-id/") {
				t.Error("Tencent TC3 credential missing")
			}
			assertHeader(t, r, "X-TC-Action", "ChatCompletions")
			assertHeader(t, r, "X-TC-Version", "2023-09-01")
			assertJSON(t, b, "Model", "hunyuan-lite")
			assertJSON(t, b, "Messages.0.Content", "hello")
			assertJSON(t, b, "Stream", "true")
			forbidJSON(t, b, "model", "messages", "stream_options")
		},
	}
}

func vertexReplayFixture(family string) replayFixture {
	f := geminiReplayFixture("vertex")
	publisher, model, action := "google", f.model, "streamGenerateContent"
	if family == "anthropic" {
		f = anthropicReplayFixture("vertex")
		publisher, model, action = "anthropic", "claude-sonnet-4@20250514", "streamRawPredict"
	}
	if family == "open-source" {
		f = openAIReplayFixture("vertex", "/v1beta1/projects/test-project/locations/global/endpoints/openapi/chat/completions")
		f.model = "llama-3.3"
	}
	f.name, f.secret = "vertex-"+family, `{"project_id":"test-project","access_token":"test-token"}`
	if family != "open-source" {
		f.path = "/v1/projects/test-project/locations/global/publishers/" + publisher + "/models/" + model + ":" + action
	}
	f.check = func(t *testing.T, r *http.Request, b []byte) {
		assertHeader(t, r, "Authorization", "Bearer test-token")
		assertHeader(t, r, "X-Goog-User-Project", "test-project")
		assertHeader(t, r, "X-Goog-Api-Key", "")
		assertHeader(t, r, "X-Api-Key", "")
		switch family {
		case "google":
			assertJSON(t, b, "contents.0.parts.0.text", "hello")
		case "anthropic":
			assertJSON(t, b, "anthropic_version", "vertex-2023-10-16")
			assertJSON(t, b, "messages.0.content.0.text", "hello")
		case "open-source":
			assertJSON(t, b, "model", "llama-3.3")
			assertJSON(t, b, "messages.0.content", "hello")
		}
		if gjson.GetBytes(b, "messages").Exists() && family == "google" {
			t.Error("Vertex native conversion absent")
		}
	}
	return f
}
