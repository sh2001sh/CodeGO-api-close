package responses_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

func TestErrorEventPreservesFlatErrorDetails(t *testing.T) {
	s := decode(event("error", `{"type":"error","code":"rate_limit_exceeded","message":"retry later"}`), "text/event-stream")
	defer func() { _ = s.Close() }()
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventError || ev.Err == nil || ev.Err.Code != "rate_limit_exceeded" || ev.Err.Message != "retry later" {
		t.Fatalf("error = %+v %v", ev, err)
	}
}

func TestCompletedSnapshotCanSupplyOutputWithoutDeltas(t *testing.T) {
	s := decode(event("response.created", `{"type":"response.created"}`)+event("response.completed", `{"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}]}}`), "text/event-stream")
	defer func() { _ = s.Close() }()
	if ev, err := s.Next(); err != nil || ev.Name != "response.created" {
		t.Fatalf("created = %+v %v", ev, err)
	}
	if ev, err := s.Next(); err != nil || ev.Name != "response.completed" || ev.TextBytes != 5 {
		t.Fatalf("completed = %+v %v", ev, err)
	}
}

func TestEmptyCompletedAndWrongDoneMarkerDoNotSucceed(t *testing.T) {
	for _, body := range []string{
		event("response.created", `{"type":"response.created"}`) + event("response.completed", `{"type":"response.completed","response":{"output":[]}}`),
		"data: [DONE]\n\n",
	} {
		s := decode(body, "text/event-stream")
		ev, err := s.Next()
		empty := ev.Kind == gateway.EventError && ev.Err != nil && ev.Err.Code == "empty_response"
		if !empty && !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("empty completed = %+v %v", ev, err)
		}
		_ = s.Close()
	}
}

func TestLifecycleBufferIsBounded(t *testing.T) {
	s := decode(strings.Repeat(event("response.in_progress", `{"type":"response.in_progress"}`), 257), "text/event-stream")
	defer func() { _ = s.Close() }()
	if _, err := s.Next(); !errors.Is(err, sse.ErrEventTooLarge) {
		t.Fatalf("overflow = %v", err)
	}
}

// Regression: lifecycle events after reasoning must precede the next text delta.
func TestReasoningThenTextLifecycleStaysInWireOrder(t *testing.T) {
	names := []string{"response.reasoning_summary_text.delta", "response.output_item.added", "response.content_part.added", "response.output_text.delta", "response.completed"}
	body := event(names[0], `{"type":"response.reasoning_summary_text.delta","delta":"think"}`) +
		event(names[1], `{"type":"response.output_item.added","item":{"type":"message","content":[]}}`) +
		event(names[2], `{"type":"response.content_part.added","part":{"type":"output_text","text":""}}`) +
		event(names[3], `{"type":"response.output_text.delta","delta":"answer"}`) +
		event(names[4], `{"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":2}}}`)
	s := decode(body, "text/event-stream")
	defer func() { _ = s.Close() }()
	for _, want := range names {
		ev, err := s.Next()
		if err != nil || ev.Name != want {
			t.Fatalf("event = %+v %v; want %s", ev, err, want)
		}
	}
}

func TestCacheWriteUsageAliases(t *testing.T) {
	for _, field := range []string{"cached_creation_tokens", "cache_creation_tokens", "cache_creation_input_tokens", "cache_write_tokens", "cache_write_input_tokens"} {
		s := decode(`{"status":"completed","output":[{"type":"function_call","name":"lookup"}],"usage":{"input_tokens":10,"output_tokens":1,"input_tokens_details":{"`+field+`":5}}}`, "application/json")
		ev, err := s.Next()
		if err != nil || ev.Usage == nil || ev.Usage.CacheWriteTokens != 5 {
			t.Fatalf("%s usage = %+v %v", field, ev.Usage, err)
		}
		_ = s.Close()
	}
}

func BenchmarkDecodeResponsesStream(b *testing.B) {
	body := event("response.created", `{"type":"response.created"}`) + strings.Repeat(event("response.output_text.delta", `{"type":"response.output_text.delta","delta":"hello"}`), 100) + event("response.completed", `{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":100}}}`)
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	for b.Loop() {
		s := decode(body, "text/event-stream")
		for {
			ev, err := s.Next()
			if err != nil {
				b.Fatal(err)
			}
			if ev.Kind == gateway.EventDone {
				break
			}
		}
		_ = s.Close()
	}
}
