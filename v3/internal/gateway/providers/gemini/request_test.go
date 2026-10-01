package gemini_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/gemini"
	"github.com/tidwall/gjson"
)

func TestNativeRequestPreservesBodyAndSelectsMappedModel(t *testing.T) {
	body := []byte(`{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"temperature":0}}`)
	for _, stream := range []bool{false, true} {
		req := &gateway.Request{Protocol: gateway.ProtocolGemini, Model: "alias", Body: body, Stream: stream}
		up, err := (gemini.Provider{}).BuildRequest(context.Background(), req, gateway.Target{
			BaseURL: "https://example.test/v1beta/", UpstreamModel: "models/gemini-2.5-pro", Secret: "test-secret"})
		if err != nil {
			t.Fatal(err)
		}
		wantURL := "https://example.test/v1beta/models/gemini-2.5-pro:generateContent"
		if stream {
			wantURL = "https://example.test/v1beta/models/gemini-2.5-pro:streamGenerateContent?alt=sse"
		}
		got, err := io.ReadAll(up.Body)
		if err != nil || string(got) != string(body) || up.URL.String() != wantURL {
			t.Fatalf("request URL/body mismatch: %s %s %v", up.URL, got, err)
		}
		if up.Header.Get("X-Goog-Api-Key") != "test-secret" || up.URL.Query().Get("key") != "" {
			t.Fatal("API key header was not set or key was placed in URL")
		}
	}
}

func TestChatRequestConvertsSystemImagesToolsAndResults(t *testing.T) {
	body := `{"model":"gemini-2.5-pro","messages":[
		{"role":"system","content":"policy"},{"role":"developer","content":"more policy"},
		{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]},
		{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"weather","arguments":"{\"city\":\"Shanghai\"}"},"extra_content":{"google":{"thought_signature":"signature"}}}]},
		{"role":"tool","tool_call_id":"call_1","content":"{\"temperature\":22}"}],
		"tools":[{"type":"function","function":{"name":"weather","description":"Weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}],
		"tool_choice":{"type":"function","function":{"name":"weather"}},"temperature":0,"top_p":0.8,
		"max_completion_tokens":800,"stop":["STOP"],"response_format":{"type":"json_schema","json_schema":{"schema":{"type":"object"}}},"reasoning_effort":"low"}`
	up, err := (gemini.Provider{}).BuildRequest(context.Background(), &gateway.Request{
		Protocol: gateway.ProtocolOpenAIChat, Body: []byte(body), Model: "gemini-2.5-pro"}, gateway.Target{BaseURL: "https://example.test"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(up.Body)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"systemInstruction.parts.0.text": "policy", "systemInstruction.parts.1.text": "more policy",
		"contents.0.role": "user", "contents.0.parts.1.inlineData.data": "aGVsbG8=",
		"contents.1.role": "model", "contents.1.parts.0.functionCall.args.city": "Shanghai",
		"contents.1.parts.0.thoughtSignature": "signature", "contents.2.parts.0.functionResponse.name": "weather",
		"contents.2.parts.0.functionResponse.response.temperature": "22", "generationConfig.temperature": "0",
		"generationConfig.maxOutputTokens": "800", "generationConfig.responseMimeType": "application/json",
		"generationConfig.thinkingConfig.thinkingBudget": "1024", "toolConfig.functionCallingConfig.mode": "ANY",
		"toolConfig.functionCallingConfig.allowedFunctionNames.0":                  "weather",
		"tools.0.functionDeclarations.0.parametersJsonSchema.properties.city.type": "string",
	} {
		if value := gjson.GetBytes(got, path).String(); value != want {
			t.Errorf("%s = %q, want %q; body %s", path, value, want, got)
		}
	}
}

func TestChatUnsupportedFeaturesAndInvalidToolsFailExplicitly(t *testing.T) {
	for name, body := range map[string]string{
		"logprobs":       `{"messages":[{"role":"user","content":"hi"}],"logprobs":true}`,
		"unsafe image":   `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://127.0.0.1/image.png"}}]}]}`,
		"bad image":      `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,not-base64!"}}]}]}`,
		"orphan tool":    `{"messages":[{"role":"tool","tool_call_id":"absent","content":"result"}]}`,
		"bad args":       `{"messages":[{"role":"assistant","tool_calls":[{"id":"1","type":"function","function":{"name":"tool","arguments":"[]"}}]}]}`,
		"unknown choice": `{"messages":[{"role":"user","content":"hi"}],"tool_choice":{"type":"function","function":{"name":"absent"}}}`,
		"strict tool":    `{"messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"tool","strict":true}}]}`,
		"bad number":     `{"messages":[{"role":"user","content":"hi"}],"temperature":"hot"}`,
		"no content":     `{"messages":[{"role":"system","content":"policy"}]}`,
		"empty text":     `{"messages":[{"role":"user","content":""}]}`,
		"invalid text":   `{"messages":[{"role":"user","content":[{"type":"text","text":42}]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := (gemini.Provider{}).BuildRequest(context.Background(), &gateway.Request{
				Protocol: gateway.ProtocolOpenAIChat, Body: []byte(body), Model: "gemini"}, gateway.Target{BaseURL: "https://example.test"})
			if err == nil || !strings.Contains(err.Error(), "gemini:") {
				t.Fatalf("want explicit conversion error, got %v", err)
			}
		})
	}
}
