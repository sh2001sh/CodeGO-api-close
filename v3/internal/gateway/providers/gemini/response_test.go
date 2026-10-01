package gemini_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/gemini"
	"github.com/tidwall/gjson"
)

func decode(req *gateway.Request, body, contentType string) gateway.EventStream {
	return (gemini.Provider{}).Decode(req, &http.Response{Body: io.NopCloser(strings.NewReader(body)),
		Header: http.Header{"Content-Type": {contentType}}})
}

func TestNativeStreamPreservesPayloadAndUsageWithoutDoneMarker(t *testing.T) {
	const body = `{"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":40,"candidatesTokenCount":5,"thoughtsTokenCount":7,"cachedContentTokenCount":10,"toolUsePromptTokenCount":3}}`
	s := decode(&gateway.Request{Protocol: gateway.ProtocolGemini, Stream: true}, "data: "+body+"\n\n", "text/event-stream")
	defer func() { _ = s.Close() }()
	event, err := s.Next()
	if err != nil || event.Kind != gateway.EventData || string(event.Payload) != body || event.TextBytes != 5 {
		t.Fatalf("native payload changed: %+v %v", event, err)
	}
	if event.Usage == nil || event.Usage.PromptTokens != 43 || event.Usage.CompletionTokens != 12 || event.Usage.CachedTokens != 10 {
		t.Fatalf("incorrect Gemini usage: %+v", event.Usage)
	}
	if _, err := s.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("native end must be clean EOF, got %v", err)
	}
}

func TestChatSingleResponseIncludesReasoningToolsAndUsage(t *testing.T) {
	body := `{"responseId":"response-1","candidates":[{"content":{"parts":[{"text":"thinking","thought":true},{"text":"answer"},{"functionCall":{"name":"weather","args":{"city":"Shanghai"}},"thoughtSignature":"signed"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":3,"thoughtsTokenCount":2,"cachedContentTokenCount":4}}`
	s := decode(&gateway.Request{ID: "test", Protocol: gateway.ProtocolOpenAIChat, Model: "public-model", Received: time.Unix(123, 0)}, body, "application/json")
	defer func() { _ = s.Close() }()
	event, err := s.Next()
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"object": "chat.completion", "model": "public-model", "created": "123", "id": "response-1",
		"choices.0.message.content": "answer", "choices.0.message.reasoning_content": "thinking",
		"choices.0.message.tool_calls.0.function.name": "weather", "choices.0.finish_reason": "tool_calls",
		"choices.0.message.tool_calls.0.function.arguments":                     `{"city":"Shanghai"}`,
		"choices.0.message.tool_calls.0.extra_content.google.thought_signature": "signed",
		"usage.prompt_tokens": "12", "usage.completion_tokens": "5", "usage.total_tokens": "17",
		"usage.prompt_tokens_details.cached_tokens": "4", "usage.completion_tokens_details.reasoning_tokens": "2",
	} {
		if got := gjson.GetBytes(event.Payload, path).String(); got != want {
			t.Errorf("%s = %q, want %q; body %s", path, got, want, event.Payload)
		}
	}
}

func TestChatStreamKeepsUsageAfterFinishAndIndexesToolCalls(t *testing.T) {
	body := "data: " + `{"responseId":"r","candidates":[{"content":{"parts":[{"functionCall":{"name":"a","args":{"x":1}}},{"functionCall":{"name":"b","args":{}}}]}}]}` + "\n\n" +
		"data: " + `{"candidates":[{"finishReason":"STOP"}]}` + "\n\n" +
		"data: " + `{"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":4}}` + "\n\n"
	for _, includeUsage := range []bool{false, true} {
		req := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Stream: true, Body: []byte(`{}`)}
		if includeUsage {
			req.Body = []byte(`{"stream_options":{"include_usage":true}}`)
		}
		s := decode(req, body, "text/event-stream")
		first, err := s.Next()
		if err != nil || gjson.GetBytes(first.Payload, "choices.0.delta.tool_calls.1.index").Int() != 1 {
			t.Fatalf("tool indexes incorrect: %s %v", first.Payload, err)
		}
		finish, err := s.Next()
		if err != nil || gjson.GetBytes(finish.Payload, "choices.0.finish_reason").Str != "tool_calls" {
			t.Fatalf("missing tool finish: %s %v", finish.Payload, err)
		}
		usage, err := s.Next()
		wantKind := gateway.EventUsage
		if includeUsage {
			wantKind = gateway.EventData
		}
		if err != nil || usage.Kind != wantKind || usage.Usage == nil || usage.Usage.CompletionTokens != 4 {
			t.Fatalf("late usage lost: %+v %v", usage, err)
		}
		done, err := s.Next()
		if err != nil || done.Kind != gateway.EventDone {
			t.Fatalf("chat stream must finish after late usage: %+v %v", done, err)
		}
		if _, err := s.Next(); !errors.Is(err, io.EOF) {
			t.Fatalf("want EOF after done, got %v", err)
		}
		_ = s.Close()
	}
}

func TestNativeErrorAndCorruptOrInterruptedStreams(t *testing.T) {
	s := decode(&gateway.Request{Protocol: gateway.ProtocolGemini}, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"try later"}}`, "application/json")
	event, err := s.Next()
	if err != nil || event.Kind != gateway.EventError || event.Err.Status != 429 || event.Err.Code != "RESOURCE_EXHAUSTED" {
		t.Fatalf("lost native error: %+v %v", event, err)
	}
	_ = s.Close()
	for name, body := range map[string]string{"corrupt": "data: not-json\n\n", "interrupted": "data: {}\n"} {
		t.Run(name, func(t *testing.T) {
			s := decode(&gateway.Request{Protocol: gateway.ProtocolGemini, Stream: true}, body, "text/event-stream")
			defer func() { _ = s.Close() }()
			if _, err := s.Next(); err == nil || errors.Is(err, io.EOF) {
				t.Fatalf("broken upstream stream accepted: %v", err)
			}
		})
	}
}

func TestChatPromptBlockedReturnsRequestError(t *testing.T) {
	s := decode(&gateway.Request{Protocol: gateway.ProtocolOpenAIChat}, `{"promptFeedback":{"blockReason":"SAFETY"}}`, "application/json")
	defer func() { _ = s.Close() }()
	event, err := s.Next()
	if err != nil || event.Kind != gateway.EventError || event.Err.Status != 400 || event.Err.Code != "content_filter" {
		t.Fatalf("blocked prompt was not classified: %+v %v", event, err)
	}
}

func TestChatRejectsLossyGeneratedMediaAndInvalidJSONObjects(t *testing.T) {
	for name, body := range map[string]string{
		"code":             `{"candidates":[{"content":{"parts":[{"executableCode":{"language":"PYTHON","code":"print(1)"}}]}}]}`,
		"image":            `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}]}}]}`,
		"null":             "null",
		"invalid function": `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"x","args":[]}}]}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			s := decode(&gateway.Request{Protocol: gateway.ProtocolOpenAIChat}, body, "application/json")
			defer func() { _ = s.Close() }()
			if _, err := s.Next(); err == nil {
				t.Fatal("lossy or invalid conversion accepted")
			}
		})
	}
}
