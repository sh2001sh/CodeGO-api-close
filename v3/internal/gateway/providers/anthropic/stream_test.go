package anthropic

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func decodeFixture(protocol gateway.Protocol, body, request string, stream bool) gateway.EventStream {
	contentType := "application/json"
	if stream {
		contentType = "text/event-stream"
	}
	req := &gateway.Request{Protocol: protocol, Model: "client-model", Body: []byte(request), Received: time.Unix(123, 0)}
	return (Provider{}).Decode(req, &http.Response{Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))})
}

func sseFixture(events ...string) string {
	var body strings.Builder
	for _, data := range events {
		body.WriteString("event: ")
		body.WriteString(gjson.Get(data, "type").Str)
		body.WriteString("\ndata: ")
		body.WriteString(data)
		body.WriteString("\n\n")
	}
	return body.String()
}

func TestNativeStreamAccumulatesUsageAndPreservesEvents(t *testing.T) {
	fixture := sseFixture(
		`{"type":"ping"}`,
		`{"type":"message_start","message":{"id":"msg1","usage":{"input_tokens":10,"output_tokens":0,"cache_read_input_tokens":20,"cache_creation_input_tokens":8,"cache_creation":{"ephemeral_1h_input_tokens":3}}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
		`{"type":"message_stop"}`)
	stream := decodeFixture(gateway.ProtocolAnthropic, fixture, "{}", true)
	defer func() { _ = stream.Close() }()
	for _, name := range []string{"message_start", "content_block_delta", "message_delta", "message_stop"} {
		ev, err := stream.Next()
		if err != nil {
			t.Fatal(err)
		}
		if ev.Kind != gateway.EventData || ev.Name != name || gjson.GetBytes(ev.Payload, "type").Str != name {
			t.Fatalf("lost native event: %+v", ev)
		}
		if ev.Usage == nil || ev.Usage.PromptTokens != 38 || ev.Usage.CachedTokens != 20 || ev.Usage.CacheWriteTokens != 5 || ev.Usage.CacheWrite1hTokens != 3 {
			t.Fatalf("incorrect accumulated usage: %+v", ev.Usage)
		}
		if name == "content_block_delta" && ev.TextBytes != 5 {
			t.Fatalf("wrong text bytes: %d", ev.TextBytes)
		}
		if name == "message_delta" && ev.Usage.CompletionTokens != 7 {
			t.Fatal("delta lost input usage")
		}
	}
	ev, err := stream.Next()
	if err != nil || ev.Kind != gateway.EventDone {
		t.Fatalf("expected done after native stop: %+v %v", ev, err)
	}
	if _, err := stream.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after done, got %v", err)
	}
}

func TestChatStreamToolsReasoningFinishAndUsage(t *testing.T) {
	fixture := sseFixture(
		`{"type":"message_start","message":{"id":"msg1","model":"upstream","usage":{"input_tokens":3,"cache_read_input_tokens":2,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"reason"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hi"}}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"call1","name":"weather","input":{}}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"city\":\"Paris\"}"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
		`{"type":"message_stop"}`)
	stream := decodeFixture(gateway.ProtocolOpenAIChat, fixture, `{"stream_options":{"include_usage":true}}`, true)
	defer func() { _ = stream.Close() }()
	var chunks [][]byte
	var finalUsage *gateway.Usage
	for {
		ev, err := stream.Next()
		if err != nil {
			t.Fatal(err)
		}
		if ev.Kind == gateway.EventDone {
			break
		}
		if ev.Kind != gateway.EventData {
			t.Fatalf("unexpected event %+v", ev)
		}
		chunks = append(chunks, append([]byte(nil), ev.Payload...))
		finalUsage = ev.Usage
		if gjson.GetBytes(ev.Payload, "model").Str != "client-model" || gjson.GetBytes(ev.Payload, "created").Int() != 123 || ev.Name != "" {
			t.Fatalf("incorrect Chat envelope: %s", ev.Payload)
		}
	}
	if len(chunks) != 7 {
		t.Fatalf("expected role, reasoning, text, tool, args, finish, usage; got %d", len(chunks))
	}
	for i, check := range []struct{ path, want string }{{"choices.0.delta.role", "assistant"}, {"choices.0.delta.reasoning_content", "reason"}, {"choices.0.delta.content", "hi"}, {"choices.0.delta.tool_calls.0.id", "call1"}, {"choices.0.delta.tool_calls.0.function.arguments", `{"city":"Paris"}`}, {"choices.0.finish_reason", "tool_calls"}} {
		if got := gjson.GetBytes(chunks[i], check.path).Str; got != check.want {
			t.Errorf("chunk %d %s=%q want %q", i, check.path, got, check.want)
		}
	}
	if gjson.GetBytes(chunks[3], "choices.0.delta.tool_calls.0.index").Int() != 0 || gjson.GetBytes(chunks[4], "choices.0.delta.tool_calls.0.id").Exists() {
		t.Fatal("tool identity/index repeated incorrectly")
	}
	if gjson.GetBytes(chunks[6], "choices.#").Int() != 0 || gjson.GetBytes(chunks[6], "usage.prompt_tokens").Int() != 5 || finalUsage == nil || finalUsage.CompletionTokens != 9 {
		t.Fatalf("wrong final usage chunk: %s %+v", chunks[6], finalUsage)
	}
}

func TestStreamFailureBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		kind       gateway.EventKind
		readError  bool
	}{
		{"in-band error", sseFixture(`{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`), gateway.EventError, false},
		{"invalid JSON", "data: nope\n\n", gateway.EventError, false},
		{"negative usage", sseFixture(`{"type":"message_start","message":{"usage":{"input_tokens":-1}}}`), gateway.EventError, false},
		{"overflow", sseFixture(`{"type":"message_start","message":{"usage":{"input_tokens":9223372036854775807,"cache_read_input_tokens":1}}}`), gateway.EventError, false},
		{"tool without start", sseFixture(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}}`), gateway.EventError, false},
		{"truncated SSE", "event: message_start\ndata: {}", 0, true},
		{"no terminal marker", "", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := decodeFixture(gateway.ProtocolOpenAIChat, tc.data, "{}", true)
			defer func() { _ = stream.Close() }()
			ev, err := stream.Next()
			if tc.readError {
				if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatalf("want truncation error, got %+v %v", ev, err)
				}
				return
			}
			if err != nil || ev.Kind != tc.kind || ev.Err == nil {
				t.Fatalf("missing typed failure: %+v %v", ev, err)
			}
		})
	}
}
