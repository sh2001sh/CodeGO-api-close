package providers_test

import (
	"net/http"
	"testing"
)

func difyReplayFixture() replayFixture {
	finish := dataEvent(`{"event":"message_end"}`)
	return replayFixture{id: "dify", name: "dify", model: "app", secret: "test-token", path: "/v1/chat-messages", contentType: "text/event-stream",
		content: dataEvent(`{"event":"message","message_id":"d1","answer":"hello"}`), finish: finish, empty: finish,
		finishUsage: dataEvent(`{"event":"message_end","metadata":{"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}}`),
		check: func(t *testing.T, r *http.Request, b []byte) {
			assertHeader(t, r, "Authorization", "Bearer test-token")
			assertJSON(t, b, "query", "USER: \nhello\n")
			assertJSON(t, b, "response_mode", "streaming")
			forbidJSON(t, b, "messages", "model", "stream_options")
		},
	}
}

func cozeReplayFixture() replayFixture {
	finish := namedEvent("conversation.chat.completed", `{"status":"completed"}`)
	return replayFixture{id: "coze", name: "coze", model: "bot-model", secret: "test-bot|test-token", path: "/v3/chat", contentType: "text/event-stream",
		content: namedEvent("conversation.message.delta", `{"id":"c1","role":"assistant","type":"answer","content_type":"text","content":"hello"}`), finish: finish, empty: finish,
		finishUsage: namedEvent("conversation.chat.completed", `{"status":"completed","usage":{"input_count":10,"output_count":3,"token_count":13}}`),
		check: func(t *testing.T, r *http.Request, b []byte) {
			assertHeader(t, r, "Authorization", "Bearer test-token")
			assertJSON(t, b, "bot_id", "test-bot")
			assertJSON(t, b, "additional_messages.0.content", "hello")
			assertJSON(t, b, "stream", "true")
			forbidJSON(t, b, "messages", "model", "stream_options")
		},
	}
}

func cloudflareReplayFixture() replayFixture {
	f := openAIReplayFixture("cloudflare", "/client/v4/accounts/test-account/ai/v1/chat/completions")
	f.model, f.secret = "@cf/meta/llama-3.1-8b-instruct", "test-account|test-token"
	f.check = func(t *testing.T, r *http.Request, b []byte) {
		assertHeader(t, r, "Authorization", "Bearer test-token")
		assertJSON(t, b, "model", f.model)
		assertJSON(t, b, "messages.0.content", "hello")
		assertJSON(t, b, "stream", "true")
		assertJSON(t, b, "stream_options.include_usage", "true")
	}
	return f
}

func cloudflareResponsesReplayFixture() replayFixture {
	f := responsesReplayFixture("cloudflare")
	f.name, f.model, f.secret = "cloudflare-responses", "@cf/meta/llama-3.1-8b-instruct", "test-account|test-token"
	f.path = "/client/v4/accounts/test-account/ai/v1/responses"
	f.clientPath, f.clientBody, f.doneMarker = "/v1/responses", `{"model":"public-alias","stream":true,"input":"hello"}`, "response.completed"
	f.check = func(t *testing.T, r *http.Request, b []byte) {
		assertHeader(t, r, "Authorization", "Bearer test-token")
		assertJSON(t, b, "model", f.model)
		assertJSON(t, b, "input", "hello")
		assertJSON(t, b, "stream", "true")
		forbidJSON(t, b, "messages", "stream_options")
	}
	return f
}
