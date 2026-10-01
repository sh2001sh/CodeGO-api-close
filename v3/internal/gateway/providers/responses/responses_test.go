package responses_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
)

func decode(body, contentType string) gateway.EventStream {
	return (responses.Provider{}).Decode(&gateway.Request{Protocol: gateway.ProtocolResponses, Stream: true}, &http.Response{
		Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {contentType}},
	})
}

func event(name, data string) string { return "event: " + name + "\ndata: " + data + "\n\n" }

func TestRequestPreservesResponsesFields(t *testing.T) {
	body := []byte(`{"model":"client","input":"hello","stream":true,"previous_response_id":"resp_previous","tools":[{"type":"web_search"}]}`)
	req, err := (responses.Provider{}).BuildRequest(context.Background(), &gateway.Request{
		Protocol: gateway.ProtocolResponses, Body: body, Model: "client", Stream: true,
	}, gateway.Target{BaseURL: "https://upstream.example/v1/", Secret: "test", UpstreamModel: "actual"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.String() != "https://upstream.example/v1/responses" || req.Header.Get("Authorization") != "Bearer test" {
		t.Fatalf("request = %s %v", req.URL, req.Header)
	}
	if !strings.Contains(string(got), `"model":"actual"`) || !strings.Contains(string(got), `"previous_response_id":"resp_previous"`) || strings.Contains(string(got), "stream_options") {
		t.Fatalf("body = %s", got)
	}
	if strings.Contains(string(body), `"model":"actual"`) {
		t.Fatal("original request body was changed")
	}
}

func TestCompletedStreamPreservesOrderAndExactUsage(t *testing.T) {
	created := event("response.created", `{"type":"response.created","sequence_number":0,"response":{"id":"resp_test"}}`)
	delta := event("response.output_text.delta", `{"type":"response.output_text.delta","sequence_number":1,"delta":"hello"}`)
	completed := event("response.completed", `{"type":"response.completed","sequence_number":2,"response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":2,"input_tokens_details":{"cached_tokens":4}}}}`)
	s := decode(created+delta+completed, "text/event-stream; charset=utf-8")
	defer func() { _ = s.Close() }()
	for i, name := range []string{"response.created", "response.output_text.delta", "response.completed"} {
		ev, err := s.Next()
		if err != nil || ev.Kind != gateway.EventData || ev.Name != name {
			t.Fatalf("event %d = %+v, %v", i, ev, err)
		}
		if i == 1 && ev.TextBytes != 5 {
			t.Fatalf("text bytes = %d", ev.TextBytes)
		}
		if i == 2 && (ev.Usage == nil || ev.Usage.PromptTokens != 10 || ev.Usage.CompletionTokens != 2 || ev.Usage.CachedTokens != 4) {
			t.Fatalf("usage = %+v", ev.Usage)
		}
	}
	if ev, err := s.Next(); err != nil || ev.Kind != gateway.EventDone {
		t.Fatalf("done = %+v %v", ev, err)
	}
	if _, err := s.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("end = %v", err)
	}
}

// Regression: a lifecycle-only prefix must not commit the first attempt.
func TestFailureBeforeOutputDiscardsLifecycle(t *testing.T) {
	s := decode(event("response.created", `{"type":"response.created"}`)+event("response.failed", `{"type":"response.failed","response":{"error":{"code":"server_error","message":"failed"}}}`), "text/event-stream")
	defer func() { _ = s.Close() }()
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventError || ev.Err == nil || ev.Err.Code != "server_error" {
		t.Fatalf("first visible event = %+v %v", ev, err)
	}
}

func TestFailureAfterOutputCarriesNativeEventAndUsage(t *testing.T) {
	s := decode(event("response.output_text.delta", `{"type":"response.output_text.delta","delta":"hello"}`)+event("response.failed", `{"type":"response.failed","response":{"error":{"code":"server_error","message":"failed"},"usage":{"input_tokens":10,"output_tokens":2}}}`), "text/event-stream")
	defer func() { _ = s.Close() }()
	if _, err := s.Next(); err != nil {
		t.Fatal(err)
	}
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventError || ev.Name != "response.failed" || ev.Usage == nil || ev.Usage.CompletionTokens != 2 || len(ev.Payload) == 0 {
		t.Fatalf("failure = %+v %v", ev, err)
	}
}

func TestIncompleteIsCleanAndMissingTerminalIsCut(t *testing.T) {
	for _, terminal := range []string{"response.completed", "response.incomplete"} {
		t.Run(terminal, func(t *testing.T) {
			s := decode(event("response.output_text.delta", `{"type":"response.output_text.delta","delta":"hello"}`)+event(terminal, `{"type":"`+terminal+`","response":{"usage":{"input_tokens":1,"output_tokens":2}}}`), "text/event-stream")
			defer func() { _ = s.Close() }()
			for i := 0; i < 2; i++ {
				if _, err := s.Next(); err != nil {
					t.Fatal(err)
				}
			}
			if ev, err := s.Next(); err != nil || ev.Kind != gateway.EventDone {
				t.Fatalf("terminal = %+v %v", ev, err)
			}
		})
	}
	s := decode(event("response.output_text.delta", `{"type":"response.output_text.delta","delta":"hello"}`), "text/event-stream")
	defer func() { _ = s.Close() }()
	if _, err := s.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Next(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("cut = %v", err)
	}
}

func TestSingleResponseAndInvalidBodies(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		kind       gateway.EventKind
		tokens     int64
	}{
		{"complete", `{"id":"resp_test","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":1,"output_tokens":2}}`, gateway.EventData, 2},
		{"tool", `{"status":"completed","output":[{"type":"function_call","name":"lookup","arguments":"{}"}]}`, gateway.EventData, 0},
		{"failed", `{"status":"failed","error":{"code":"server_error","message":"failed"}}`, gateway.EventError, 0},
		{"string error", `{"error":"temporarily unavailable"}`, gateway.EventError, 0},
		{"empty", `{"status":"completed","output":[]}`, gateway.EventError, 0},
		{"invalid", `<html>upstream unavailable</html>`, gateway.EventError, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := decode(tc.body, "application/json")
			defer func() { _ = s.Close() }()
			ev, err := s.Next()
			if err != nil || ev.Kind != tc.kind {
				t.Fatalf("event = %+v %v", ev, err)
			}
			if tc.tokens > 0 && (ev.Usage == nil || ev.Usage.CompletionTokens != tc.tokens) {
				t.Fatalf("usage = %+v", ev.Usage)
			}
		})
	}
}
