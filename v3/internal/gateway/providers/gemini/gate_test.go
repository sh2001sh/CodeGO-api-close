package gemini_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestLifecycleOnlyGeminiCompletionProducesNoClientData(t *testing.T) {
	for _, protocol := range []gateway.Protocol{gateway.ProtocolGemini, gateway.ProtocolOpenAIChat} {
		for _, stream := range []bool{false, true} {
			body := `{"candidates":[{"index":0,"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":12}}`
			contentType := "application/json"
			if stream {
				body, contentType = "data: "+body+"\n\n", "text/event-stream"
			}
			s := decode(&gateway.Request{Protocol: protocol, Stream: stream, Body: []byte(`{"stream_options":{"include_usage":true}}`)}, body, contentType)
			for {
				ev, err := s.Next()
				if errors.Is(err, io.EOF) || ev.Kind == gateway.EventDone {
					break
				}
				if err != nil || ev.Kind == gateway.EventData {
					t.Fatalf("lifecycle-only %s stream=%v committed output: %+v %v", protocolName(protocol), stream, ev, err)
				}
			}
			_ = s.Close()
		}
	}
}

func TestGeminiLifecycleThenErrorNeverCommitsOutput(t *testing.T) {
	for _, protocol := range []gateway.Protocol{gateway.ProtocolGemini, gateway.ProtocolOpenAIChat} {
		body := "data: " + `{"candidates":[{"index":0,"content":{"parts":[]}}]}` + "\n\n" +
			"data: " + `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"retry later"}}` + "\n\n"
		s := decode(&gateway.Request{Protocol: protocol, Stream: true}, body, "text/event-stream")
		ev, err := s.Next()
		_ = s.Close()
		if err != nil || ev.Kind != gateway.EventError || ev.Err.Status != 429 {
			t.Fatalf("lifecycle exposed before request can fail over: %+v %v", ev, err)
		}
	}
}

func TestGeminiGateReplaysLifecycleInOrderBeforeActualOutput(t *testing.T) {
	for _, protocol := range []gateway.Protocol{gateway.ProtocolGemini, gateway.ProtocolOpenAIChat} {
		first := `{"responseId":"first","candidates":[{"index":0,"content":{"parts":[]}}]}`
		second := `{"responseId":"second","candidates":[{"index":0,"content":{"parts":[{"text":"answer"}]},"finishReason":"STOP"}]}`
		body := "data: " + first + "\n\n" + "data: " + second + "\n\n" +
			"data: " + `{"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":2}}` + "\n\n"
		s := decode(&gateway.Request{Protocol: protocol, Stream: true}, body, "text/event-stream")
		preamble, err := s.Next()
		if err != nil || !strings.Contains(string(preamble.Payload), "first") || strings.Contains(string(preamble.Payload), "answer") {
			t.Fatalf("original lifecycle payload/order lost: %s %v", preamble.Payload, err)
		}
		content, err := s.Next()
		if err != nil || !strings.Contains(string(content.Payload), "answer") {
			t.Fatalf("semantic payload/order lost: %s %v", content.Payload, err)
		}
		usage, err := s.Next()
		if err != nil || usage.Usage == nil || usage.Usage.CompletionTokens != 2 {
			t.Fatalf("late usage lost after gate: %+v %v", usage, err)
		}
		_ = s.Close()
	}
}

func TestGeminiGateBoundsLifecycleWithoutOutput(t *testing.T) {
	for name, body := range map[string]string{
		"count": strings.Repeat("data: "+`{"candidates":[{"index":0,"content":{"parts":[]}}]}`+"\n\n", 257),
		"bytes": "data: " + `{"responseId":"` + strings.Repeat("x", 1<<20) + `","candidates":[{"finishReason":"STOP"}]}` + "\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			s := decode(&gateway.Request{Protocol: gateway.ProtocolGemini, Stream: true}, body, "text/event-stream")
			defer func() { _ = s.Close() }()
			ev, err := s.Next()
			if err == nil && (ev.Kind != gateway.EventError || ev.Err == nil) {
				t.Fatalf("unbounded lifecycle frames were exposed: %+v %v", ev, err)
			}
		})
	}
}
