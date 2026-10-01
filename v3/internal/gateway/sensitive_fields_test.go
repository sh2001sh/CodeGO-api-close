package gateway_test

import (
	"testing"
)

func TestSensitiveProtocolPromptFields(t *testing.T) {
	for _, test := range []struct{ name, path, body string }{
		{"chat structured text", "/v1/chat/completions", `{"model":"test-model","messages":[{"role":"user","content":[{"type":"text","text":"reverse shell"}]}]}`},
		{"chat system text", "/v1/chat/completions", `{"model":"test-model","messages":[{"role":"system","content":"reverse shell"},{"role":"user","content":"hello"}]}`},
		{"responses input string", "/v1/responses", `{"model":"test-model","input":"reverse shell"}`},
		{"responses message parts", "/v1/responses", `{"model":"test-model","input":[{"role":"user","content":[{"type":"input_text","text":"reverse shell"}]}]}`},
		{"responses instructions", "/v1/responses", `{"model":"test-model","input":"hello","instructions":"reverse shell"}`},
		{"responses tool result", "/v1/responses", `{"model":"test-model","input":[{"type":"function_call_output","call_id":"c1","output":"reverse shell"}]}`},
		{"gemini text", "/v1beta/models/test-model:generateContent", `{"contents":[{"role":"user","parts":[{"text":"reverse shell"}]}]}`},
		{"gemini system camelcase", "/v1beta/models/test-model:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"systemInstruction":{"parts":[{"text":"reverse shell"}]}}`},
		{"gemini system snakecase", "/v1beta/models/test-model:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"system_instruction":{"parts":[{"text":"reverse shell"}]}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newSensitiveHTTPHarness(t, nil, nil)
			status, body, out := h.request(t, test.path, test.body)
			if status != 403 || out != nil || h.reserves() != 0 || h.calls.Load() != 0 {
				t.Fatalf("prompt field missed: %d %s", status, body)
			}
		})
	}
}

func TestSensitiveIgnoresSchemasURLsAndMetadata(t *testing.T) {
	for _, test := range []struct{ name, path, body string }{
		{"chat fields", "/v1/chat/completions", `{"model":"test-model","messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"https://example.test/reverse-shell","detail":"reverse shell"}}]}],"metadata":{"text":"reverse shell"},"tools":[{"type":"function","function":{"name":"f","description":"reverse shell","parameters":{"type":"object","properties":{"value":{"description":"reverse shell","type":"string"}}}}}]}`},
		{"responses fields", "/v1/responses", `{"model":"test-model","input":"hello","metadata":{"text":"reverse shell"},"tools":[{"type":"function","name":"f","description":"reverse shell","parameters":{"type":"object","properties":{"value":{"description":"reverse shell","type":"string"}}}}]}`},
		{"gemini fields", "/v1beta/models/test-model:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"tools":[{"functionDeclarations":[{"name":"f","description":"reverse shell","parameters":{"type":"object","properties":{"value":{"type":"string","description":"reverse shell"}}}}]}],"metadata":{"text":"reverse shell"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newSensitiveHTTPHarness(t, nil, nil)
			status, body, out := h.request(t, test.path, test.body)
			if status != 200 || out == nil || h.reserves() != 1 || h.calls.Load() != 1 {
				t.Fatalf("nonprompt metadata caused interception: %d %s", status, body)
			}
		})
	}
}
