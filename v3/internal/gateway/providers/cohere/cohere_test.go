package cohere

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

func TestRequestPreservesEveryHistoricalUserMessage(t *testing.T) {
	req := fixtureRequest(`{"messages":[{"role":"system","content":"rules"},{"role":"user","content":"first"},{"role":"assistant","content":"answer"},{"role":"user","content":[{"type":"text","text":"last"}]}],"max_completion_tokens":23,"temperature":0,"top_p":0.8,"stop":["END"]}`, true)
	out, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{BaseURL: "https://example.invalid/v1/", Secret: "test-key", UpstreamModel: "command-r"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatal(err)
	}
	if out.URL.String() != "https://example.invalid/v2/chat" || out.Header.Get("Authorization") != "Bearer test-key" || out.Header.Get("Accept") != "text/event-stream" {
		t.Fatalf("wrong request destination or auth: %s", out.URL)
	}
	root := gjson.ParseBytes(data)
	if root.Get("model").Str != "command-r" || root.Get("messages.3.content").Str != "last" || root.Get("messages.1.content").Str != "first" || root.Get("messages.2.role").Str != "assistant" || root.Get("max_tokens").Int() != 23 || root.Get("temperature").Float() != 0 || root.Get("p").Float() != 0.8 || root.Get("stop_sequences.0").Str != "END" || !root.Get("stream").Bool() {
		t.Fatalf("lost request semantics: %s", data)
	}
}

func TestUnsupportedRequestsFailBeforeNetwork(t *testing.T) {
	for _, body := range []string{
		`{"messages":[]}`,
		`{"messages":[{"role":"invalid","content":"continue"}]}`,
		`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"x"}}]}]}`,
		`{"messages":[{"role":"user","content":"x"}],"tools":[{"type":"function"}]}`,
		`{"messages":[{"role":"user","content":"x"}],"max_tokens":0}`,
		`{"messages":[{"role":"user","content":"x"}],"n":2}`,
		`{"messages":[{"role":"user","content":"x"}],"logprobs":true}`,
		`{"messages":[{"role":"user","content":"x"}],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`,
		`{"messages":[{"role":"tool","content":"x","tool_call_id":"missing"}]}`,
	} {
		_, err := (Provider{}).BuildRequest(context.Background(), fixtureRequest(body, false), gateway.Target{})
		var typed *gateway.UpstreamError
		if !errors.As(err, &typed) || typed.Status != http.StatusBadRequest {
			t.Fatalf("request accepted or wrong error: %s %v", body, err)
		}
	}
}

func TestSingleResponseUsageAndLengthReason(t *testing.T) {
	s := fixtureStream(false, `{"id":"r1","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]},"finish_reason":"MAX_TOKENS","usage":{"billed_units":{"input_tokens":4,"output_tokens":2}}}`, "application/json", false)
	defer func() { _ = s.Close() }()
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventData || ev.TextBytes != 5 || ev.Usage == nil || ev.Usage.PromptTokens != 4 || ev.Usage.CompletionTokens != 2 || gjson.GetBytes(ev.Payload, "choices.0.finish_reason").Str != "length" || gjson.GetBytes(ev.Payload, "model").Str != "alias" {
		t.Fatalf("wrong converted response: %+v %s %v", ev, ev.Payload, err)
	}
	if _, err := s.Next(); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}

func TestStreamUsageIsCollectedAndOnlyForwardedOnRequest(t *testing.T) {
	data := event("message-start", `{"type":"message-start","id":"r1"}`) +
		event("content-delta", `{"type":"content-delta","index":0,"delta":{"message":{"content":{"text":"hello"}}}}`) +
		event("message-end", `{"type":"message-end","delta":{"finish_reason":"COMPLETE","usage":{"billed_units":{"input_tokens":8,"output_tokens":0}}}}`)
	for _, wantUsage := range []bool{false, true} {
		s := fixtureStream(true, data, "text/event-stream", wantUsage)
		ev, err := s.Next()
		if err != nil || ev.Kind != gateway.EventData || ev.TextBytes != 5 || gjson.GetBytes(ev.Payload, "choices.0.delta.role").Str != "assistant" {
			t.Fatalf("bad text chunk: %+v %v", ev, err)
		}
		ev, err = s.Next()
		if err != nil || ev.Kind != gateway.EventData || gjson.GetBytes(ev.Payload, "choices.0.finish_reason").Str != "stop" {
			t.Fatalf("bad finish chunk: %+v %v", ev, err)
		}
		ev, err = s.Next()
		expected := gateway.EventUsage
		if wantUsage {
			expected = gateway.EventData
		}
		if err != nil || ev.Kind != expected || ev.Usage == nil || ev.Usage.PromptTokens != 8 || ev.Usage.CompletionTokens != 0 {
			t.Fatalf("bad usage event: %+v %v", ev, err)
		}
		if wantUsage && gjson.GetBytes(ev.Payload, "choices.#").Int() != 0 {
			t.Fatalf("usage chunk contains choices: %s", ev.Payload)
		}
		ev, err = s.Next()
		if err != nil || ev.Kind != gateway.EventDone {
			t.Fatalf("missing clean done: %+v %v", ev, err)
		}
		if _, err := s.Next(); !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
		_ = s.Close()
	}
}

func TestSSEAndTruncatedStream(t *testing.T) {
	s := fixtureStream(true, event("content-delta", `{"type":"content-delta","index":0,"delta":{"message":{"content":{"text":"x"}}}}`), "text/event-stream", false)
	defer func() { _ = s.Close() }()
	if _, err := s.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Next(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncation treated as clean completion: %v", err)
	}
}

func TestErrorAndEmptyResponsesStayInvisible(t *testing.T) {
	for _, data := range []string{`{"message":"bad key"}`, `{"error":{"message":"unavailable"}}`, `{"message":{"content":[{"type":"text","text":"x"}]},"finish_reason":"ERROR"}`, `{"message":{"content":[]},"finish_reason":"ERROR"}`} {
		s := fixtureStream(false, data, "application/json", false)
		ev, err := s.Next()
		if err != nil || ev.Kind != gateway.EventError {
			t.Fatalf("in-band error became data: %+v %v", ev, err)
		}
		_ = s.Close()
	}
	s := fixtureStream(true, event("message-start", `{"type":"message-start"}`)+event("message-end", `{"type":"message-end","delta":{"finish_reason":"COMPLETE","usage":{"billed_units":{"input_tokens":0,"output_tokens":0}}}}`), "text/event-stream", true)
	defer func() { _ = s.Close() }()
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventError || ev.Err == nil || ev.Err.Code != "empty_response" || ev.Usage == nil {
		t.Fatalf("empty stream committed client headers: %+v %v", ev, err)
	}
	if _, err = s.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected empty terminal: %v", err)
	}
}

func TestMissingAndInvalidUsageCannotMasqueradeAsZero(t *testing.T) {
	s := fixtureStream(false, `{"message":{"content":[{"type":"text","text":"x"}]},"finish_reason":"COMPLETE","usage":{"billed_units":{"input_tokens":4}}}`, "application/json", false)
	ev, err := s.Next()
	_ = s.Close()
	if err != nil || ev.Usage != nil {
		t.Fatalf("partial usage accepted as complete: %+v %v", ev, err)
	}
	s = fixtureStream(false, `{"message":{"content":[{"type":"text","text":"x"}]},"finish_reason":"COMPLETE","usage":{"billed_units":{"input_tokens":-1,"output_tokens":2}}}`, "application/json", false)
	defer func() { _ = s.Close() }()
	if _, err := s.Next(); err == nil {
		t.Fatal("negative usage accepted")
	}
	for _, tokens := range []string{`1.5`, `-0.5`, `9223372036854775808`, `"4"`} {
		s := fixtureStream(false, `{"message":{"content":[{"type":"text","text":"x"}]},"finish_reason":"COMPLETE","usage":{"billed_units":{"input_tokens":`+tokens+`,"output_tokens":2}}}`, "application/json", false)
		_, err := s.Next()
		_ = s.Close()
		if err == nil {
			t.Fatalf("invalid token count %s accepted", tokens)
		}
	}
}

func event(name, body string) string { return "event: " + name + "\ndata: " + body + "\n\n" }

func fixtureRequest(body string, stream bool) *gateway.Request {
	return &gateway.Request{ID: "fixture", Received: time.Unix(123, 0), Protocol: gateway.ProtocolOpenAIChat, Body: []byte(body), Model: "alias", Stream: stream}
}

func fixtureStream(stream bool, data, contentType string, wantUsage bool) gateway.EventStream {
	body := `{}`
	if wantUsage {
		body = `{"stream_options":{"include_usage":true}}`
	}
	return (Provider{}).Decode(fixtureRequest(body, stream), &http.Response{Body: io.NopCloser(strings.NewReader(data)), Header: http.Header{"Content-Type": {contentType}}})
}
