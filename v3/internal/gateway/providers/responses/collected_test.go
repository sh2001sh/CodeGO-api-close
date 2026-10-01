package responses_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
)

func nonStreaming(body string) gateway.EventStream {
	return (responses.Provider{}).Decode(&gateway.Request{Protocol: gateway.ProtocolResponses}, &http.Response{
		Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {"text/event-stream"}},
	})
}

func TestStreamingUpstreamForNonStreamingClientYieldsOneResponse(t *testing.T) {
	response := `{"id":"resp_test","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":2}}`
	s := nonStreaming(event("response.created", `{"type":"response.created"}`) +
		event("response.output_text.delta", `{"type":"response.output_text.delta","delta":"hello"}`) +
		event("response.completed", `{"type":"response.completed","response":`+response+`}`))
	defer func() { _ = s.Close() }()
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventData || string(ev.Payload) != response || ev.Name != "" || ev.TextBytes != 5 || ev.Usage == nil || ev.Usage.CompletionTokens != 2 {
		t.Fatalf("response = %+v %v", ev, err)
	}
	if _, err = s.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("second event = %v", err)
	}
}

func TestStreamingFailureIsNotPartialJSONForNonStreamingClient(t *testing.T) {
	s := nonStreaming(event("response.output_text.delta", `{"type":"response.output_text.delta","delta":"hello"}`) +
		event("response.failed", `{"type":"response.failed","response":{"error":{"code":"server_error","message":"failed"},"usage":{"input_tokens":10,"output_tokens":2}}}`))
	defer func() { _ = s.Close() }()
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventError || len(ev.Payload) != 0 || ev.Err.Code != "server_error" || ev.Usage == nil || ev.Usage.CompletionTokens != 2 {
		t.Fatalf("failure = %+v %v", ev, err)
	}
}
