package gemini_test

import (
	"errors"
	"io"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestGeminiSSERejectsEOFBeforeEveryCandidateFinishes(t *testing.T) {
	for _, protocol := range []gateway.Protocol{gateway.ProtocolGemini, gateway.ProtocolOpenAIChat} {
		for name, frame := range map[string]string{
			"single unfinished": `{"candidates":[{"index":0,"content":{"parts":[{"text":"partial"}]}}]}`,
			"second unfinished": `{"candidates":[{"index":0,"content":{"parts":[{"text":"complete"}]},"finishReason":"STOP"},{"index":1,"content":{"parts":[{"text":"partial"}]}}]}`,
		} {
			t.Run(name+protocolName(protocol), func(t *testing.T) {
				s := decode(&gateway.Request{Protocol: protocol, Stream: true}, "data: "+frame+"\n\n", "text/event-stream")
				defer func() { _ = s.Close() }()
				if event, err := s.Next(); err != nil || event.Kind != gateway.EventData {
					t.Fatalf("want partial content event before truncation: %+v %v", event, err)
				}
				if _, err := s.Next(); !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatalf("unfinished candidate must fail as truncated, got %v", err)
				}
			})
		}
	}
}

func TestGeminiSSEWaitsForAllCandidatesAndLateUsage(t *testing.T) {
	body := "data: " + `{"candidates":[{"index":0,"content":{"parts":[{"text":"zero"}]},"finishReason":"STOP"},{"index":1,"content":{"parts":[{"text":"one"}]}}]}` + "\n\n" +
		"data: " + `{"candidates":[{"index":1,"finishReason":"STOP"}]}` + "\n\n" +
		"data: " + `{"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":9}}` + "\n\n"
	for _, protocol := range []gateway.Protocol{gateway.ProtocolGemini, gateway.ProtocolOpenAIChat} {
		t.Run(protocolName(protocol), func(t *testing.T) {
			s := decode(&gateway.Request{Protocol: protocol, Stream: true}, body, "text/event-stream")
			defer func() { _ = s.Close() }()
			for index := 0; index < 3; index++ {
				event, err := s.Next()
				if err != nil || event.Kind == gateway.EventDone {
					t.Fatalf("candidate/usage frame %d was lost or ended early: %+v %v", index, event, err)
				}
				if index == 2 && (event.Usage == nil || event.Usage.CompletionTokens != 9) {
					t.Fatalf("late usage was lost: %+v", event)
				}
			}
			if protocol == gateway.ProtocolOpenAIChat {
				if event, err := s.Next(); err != nil || event.Kind != gateway.EventDone {
					t.Fatalf("Chat completion marker missing: %+v %v", event, err)
				}
			}
			if _, err := s.Next(); !errors.Is(err, io.EOF) {
				t.Fatalf("all finished candidates must close cleanly, got %v", err)
			}
		})
	}
}

func TestGeminiNativeBlockedPromptReturnsErrorBeforeOutput(t *testing.T) {
	for _, stream := range []bool{false, true} {
		body, contentType := `{"promptFeedback":{"blockReason":"SAFETY"},"usageMetadata":{"promptTokenCount":12}}`, "application/json"
		if stream {
			body, contentType = "data: "+body+"\n\n", "text/event-stream"
		}
		s := decode(&gateway.Request{Protocol: gateway.ProtocolGemini, Stream: stream}, body, contentType)
		event, err := s.Next()
		_ = s.Close()
		if err != nil || event.Kind != gateway.EventError || event.Err.Status != 400 || event.Err.Code != "content_filter" {
			t.Fatalf("native blocked prompt must fail before any data: %+v %v", event, err)
		}
	}
}

func protocolName(protocol gateway.Protocol) string {
	if protocol == gateway.ProtocolGemini {
		return " native"
	}
	return " chat"
}
