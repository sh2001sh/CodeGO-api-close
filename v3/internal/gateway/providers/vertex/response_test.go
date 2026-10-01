package vertex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func decodeFixture(t *testing.T, protocol gateway.Protocol, model, response string, stream bool) gateway.EventStream {
	t.Helper()
	req := &gateway.Request{ID: "r1", Model: "public-alias", Protocol: protocol, Stream: stream, Received: time.Unix(123, 0),
		Body: []byte(`{"model":"public-alias","messages":[{"role":"user","content":"hello"}],"max_tokens":32,"stream_options":{"include_usage":true}}`)}
	if protocol == gateway.ProtocolGemini {
		req.Body = []byte(`{"contents":[{"parts":[{"text":"hello"}]}]}`)
	}
	secret := "key"
	if modelFamily(model) == openSource {
		secret = encoded(t, Credentials{ProjectID: "project", APIKey: "key"})
	}
	out, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{Secret: secret, UpstreamModel: model})
	if err != nil {
		t.Fatal(err)
	}
	_ = out.Body.Close()
	contentType := "application/json"
	if stream {
		contentType = "text/event-stream"
	}
	return (Provider{}).Decode(req, &http.Response{Request: out, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(response))})
}

func TestSingleResponsesPreserveNativeAndChatUsage(t *testing.T) {
	for _, tc := range []struct {
		model, data, textPath string
		native                gateway.Protocol
		prompt, output, cache int64
	}{
		{"claude-sonnet-4", `{"id":"m1","type":"message","content":[{"type":"text","text":"answer"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":2}}`, "choices.0.message.content", gateway.ProtocolAnthropic, 12, 3, 2},
		{"gemini-2.5-pro", `{"candidates":[{"content":{"parts":[{"text":"answer"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3,"cachedContentTokenCount":2}}`, "choices.0.message.content", gateway.ProtocolGemini, 10, 3, 2},
	} {
		for _, protocol := range []gateway.Protocol{tc.native, gateway.ProtocolOpenAIChat} {
			s := decodeFixture(t, protocol, tc.model, tc.data, false)
			event, err := s.Next()
			if err != nil || event.Kind != gateway.EventData || event.Usage == nil || event.Usage.PromptTokens != tc.prompt || event.Usage.CompletionTokens != tc.output || event.Usage.CachedTokens != tc.cache {
				t.Fatalf("lost Vertex usage: %+v %v", event, err)
			}
			if protocol == tc.native && string(event.Payload) != tc.data {
				t.Fatalf("native response changed: %s", event.Payload)
			}
			if protocol == gateway.ProtocolOpenAIChat && (gjson.GetBytes(event.Payload, tc.textPath).Str != "answer" || gjson.GetBytes(event.Payload, "model").Str != "public-alias") {
				t.Fatalf("alias selected wrong decoder: %s", event.Payload)
			}
			if _, err := s.Next(); !errors.Is(err, io.EOF) {
				t.Fatalf("wrong single-response termination: %v", err)
			}
			_ = s.Close()
		}
	}
}

func TestStreamingNativeAndChatUsage(t *testing.T) {
	claudeStream := "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":8,"output_tokens":0}}}` + "\n\n" +
		"event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}` + "\n\n" +
		"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}` + "\n\n" +
		"event: message_stop\ndata: " + `{"type":"message_stop"}` + "\n\n"
	geminiStream := "data: " + `{"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":4}}` + "\n\n"
	for _, tc := range []struct {
		model, body string
		native      gateway.Protocol
	}{
		{"claude-sonnet-4", claudeStream, gateway.ProtocolAnthropic},
		{"gemini-2.5-pro", geminiStream, gateway.ProtocolGemini},
	} {
		for _, protocol := range []gateway.Protocol{tc.native, gateway.ProtocolOpenAIChat} {
			s := decodeFixture(t, protocol, tc.model, tc.body, true)
			var usage *gateway.Usage
			textBytes, events := 0, 0
			for {
				event, err := s.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if event.Kind == gateway.EventError {
					t.Fatalf("unexpected Vertex stream error: %+v", event.Err)
				}
				textBytes += event.TextBytes
				events++
				if event.Usage != nil {
					usage = event.Usage
				}
			}
			_ = s.Close()
			if usage == nil || usage.PromptTokens != 8 || usage.CompletionTokens != 4 || textBytes != 5 || events == 0 {
				t.Fatalf("lost streaming output/usage: %+v bytes=%d events=%d", usage, textBytes, events)
			}
		}
	}
}

func TestStreamingErrorAndTruncation(t *testing.T) {
	for _, tc := range []struct {
		model, body string
		native      gateway.Protocol
		errorEvent  bool
	}{
		{"claude-sonnet-4", "event: error\ndata: " + `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}` + "\n\n", gateway.ProtocolAnthropic, true},
		{"gemini", "data: " + `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"busy"}}` + "\n\n", gateway.ProtocolGemini, true},
		{"claude-sonnet-4", "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"m","usage":{"input_tokens":1}}}` + "\n\n" + "event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"x"}}` + "\n\n", gateway.ProtocolAnthropic, false},
		{"gemini", "data: " + `{"candidates":[{"content":{"parts":[{"text":"x"}]}}]}` + "\n\n", gateway.ProtocolGemini, false},
	} {
		for _, protocol := range []gateway.Protocol{tc.native, gateway.ProtocolOpenAIChat} {
			s := decodeFixture(t, protocol, tc.model, tc.body, true)
			found := false
			for i := 0; i < 10; i++ {
				event, err := s.Next()
				if tc.errorEvent && event.Kind == gateway.EventError && event.Err != nil {
					found = true
					break
				}
				if !tc.errorEvent && errors.Is(err, io.ErrUnexpectedEOF) {
					found = true
					break
				}
				if err != nil {
					break
				}
			}
			_ = s.Close()
			if !found {
				t.Fatalf("Vertex failure falsely completed: model=%s protocol=%d", tc.model, protocol)
			}
		}
	}
}

func TestOpenSourceAliasesUseOpenAIDecoder(t *testing.T) {
	s := decodeFixture(t, gateway.ProtocolOpenAIChat, "meta/llama-3.3-70b-instruct-maas",
		`{"id":"r","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":4,"total_tokens":11}}`, false)
	defer func() { _ = s.Close() }()
	event, err := s.Next()
	if err != nil || event.Kind != gateway.EventData || event.Usage == nil || event.Usage.PromptTokens != 7 || event.Usage.CompletionTokens != 4 || gjson.GetBytes(event.Payload, "choices.0.message.content").Str != "hello" {
		t.Fatalf("wrong OpenAPI decoder: %+v %v", event, err)
	}
}
