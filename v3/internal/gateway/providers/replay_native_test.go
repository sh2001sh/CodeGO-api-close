package providers_test

import (
	"net/http"
	"testing"
)

func nativeReplayFixtures() []replayFixture {
	fixtures := []replayFixture{anthropicReplayFixture("anthropic"), anthropicReplayFixture("claude"), geminiReplayFixture("gemini"), responsesReplayFixture("responses"), ollamaReplayFixture(), cohereReplayFixture()}
	azure := openAIReplayFixture("azure", "/openai/deployments/upstream-model/chat/completions")
	azure.check = func(t *testing.T, r *http.Request, b []byte) {
		assertHeader(t, r, "Api-Key", "test-token")
		assertHeader(t, r, "Authorization", "")
		if r.URL.Query().Get("api-version") != "2024-10-21" {
			t.Errorf("Azure API version lost: %s", r.URL)
		}
		assertJSON(t, b, "model", "upstream-model")
		assertJSON(t, b, "messages.0.content", "hello")
	}
	fixtures = append(fixtures, azure)
	codex := responsesReplayFixture("codex")
	codex.secret, codex.path = `{"access_token":"test-token","account_id":"account-1"}`, "/backend-api/codex/responses"
	codex.check = func(t *testing.T, r *http.Request, b []byte) {
		assertHeader(t, r, "Authorization", "Bearer test-token")
		assertHeader(t, r, "Chatgpt-Account-Id", "account-1")
		assertHeader(t, r, "Openai-Beta", "responses=experimental")
		assertJSON(t, b, "store", "false")
		assertJSON(t, b, "input.0.content.0.text", "hello")
		assertJSON(t, b, "model", "upstream-model")
	}
	fixtures = append(fixtures, codex, bedrockReplayFixture("bedrock"), bedrockReplayFixture("aws"))
	return append(fixtures, otherNativeReplayFixtures()...)
}

func anthropicReplayFixture(id string) replayFixture {
	start := namedEvent("message_start", `{"type":"message_start","message":{"id":"m1","role":"assistant","content":[]}}`)
	startUsage := namedEvent("message_start", `{"type":"message_start","message":{"id":"m1","role":"assistant","content":[],"usage":{"input_tokens":10}}}`)
	finish := namedEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`)
	finishUsage := namedEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`)
	stop := namedEvent("message_stop", `{"type":"message_stop"}`)
	return replayFixture{id: id, name: id, model: "claude-sonnet-4-20250514", secret: "test-token", path: "/v1/messages", contentType: "text/event-stream",
		preamble: start, preambleUsage: startUsage,
		content: namedEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`),
		finish:  finish + stop, finishUsage: finishUsage + stop, empty: start + finish + stop,
		check: func(t *testing.T, r *http.Request, b []byte) {
			assertHeader(t, r, "X-Api-Key", "test-token")
			assertHeader(t, r, "Anthropic-Version", "2023-06-01")
			assertJSON(t, b, "model", "claude-sonnet-4-20250514")
			assertJSON(t, b, "messages.0.content.0.text", "hello")
			forbidJSON(t, b, "stream_options")
		},
	}
}

func geminiReplayFixture(id string) replayFixture {
	finish := dataEvent(`{"candidates":[{"index":0,"finishReason":"STOP"}]}`)
	return replayFixture{id: id, name: id, model: "gemini-2.5-flash", secret: "test-token", path: "/v1beta/models/gemini-2.5-flash:streamGenerateContent", contentType: "text/event-stream",
		content: dataEvent(`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"hello"}]}}]}`),
		finish:  finish, finishUsage: finish + dataEvent(`{"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3}}`), empty: finish,
		check: func(t *testing.T, r *http.Request, b []byte) {
			assertHeader(t, r, "X-Goog-Api-Key", "test-token")
			if r.URL.Query().Get("alt") != "sse" {
				t.Errorf("Gemini SSE query lost")
			}
			assertJSON(t, b, "contents.0.parts.0.text", "hello")
			forbidJSON(t, b, "model", "messages", "stream_options")
		},
	}
}

func responsesReplayFixture(id string) replayFixture {
	start := namedEvent("response.created", `{"type":"response.created","response":{"id":"r1","status":"in_progress"}}`)
	finish := namedEvent("response.completed", `{"type":"response.completed","response":{"id":"r1","status":"completed","output":[]}}`)
	usage := namedEvent("response.completed", `{"type":"response.completed","response":{"id":"r1","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":3}}}`)
	return replayFixture{id: id, name: id, model: "upstream-model", secret: "test-token", path: "/v1/responses", contentType: "text/event-stream", preamble: start, preambleUsage: start,
		content: namedEvent("response.output_text.delta", `{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"hello"}`),
		finish:  finish, finishUsage: usage, empty: start + finish,
		check: func(t *testing.T, r *http.Request, b []byte) {
			assertHeader(t, r, "Authorization", "Bearer test-token")
			assertJSON(t, b, "model", "upstream-model")
			assertJSON(t, b, "input.0.content.0.text", "hello")
			forbidJSON(t, b, "messages", "stream_options")
		},
	}
}

func ollamaReplayFixture() replayFixture {
	finish := `{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop"}` + "\n"
	return replayFixture{id: "ollama", name: "ollama", model: "upstream-model", secret: "test-token", path: "/api/chat", contentType: "application/x-ndjson",
		content: `{"message":{"role":"assistant","content":"hello"},"done":false}` + "\n", finish: finish,
		finishUsage: `{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":10,"eval_count":3}` + "\n", empty: finish,
		check: func(t *testing.T, r *http.Request, b []byte) {
			assertHeader(t, r, "Authorization", "Bearer test-token")
			assertHeader(t, r, "Accept", "application/x-ndjson")
			assertJSON(t, b, "model", "upstream-model")
			assertJSON(t, b, "messages.0.content", "hello")
			forbidJSON(t, b, "stream_options")
		},
	}
}

func cohereReplayFixture() replayFixture {
	start := namedEvent("message-start", `{"type":"message-start","id":"c1"}`)
	finish := namedEvent("message-end", `{"type":"message-end","delta":{"finish_reason":"COMPLETE"}}`)
	return replayFixture{id: "cohere", name: "cohere", model: "command-r", secret: "test-token", path: "/v2/chat", contentType: "text/event-stream", preamble: start, preambleUsage: start,
		content: namedEvent("content-delta", `{"type":"content-delta","index":0,"delta":{"message":{"content":{"text":"hello"}}}}`),
		finish:  finish, finishUsage: namedEvent("message-end", `{"type":"message-end","delta":{"finish_reason":"COMPLETE","usage":{"billed_units":{"input_tokens":10,"output_tokens":3}}}}`), empty: start + finish,
		check: func(t *testing.T, r *http.Request, b []byte) {
			assertHeader(t, r, "Authorization", "Bearer test-token")
			assertJSON(t, b, "model", "command-r")
			assertJSON(t, b, "messages.0.content", "hello")
			forbidJSON(t, b, "stream_options")
		},
	}
}
