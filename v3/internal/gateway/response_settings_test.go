package gateway

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

type settingsFixtureStream struct {
	events []Event
	err    error
	closed bool
}

func (s *settingsFixtureStream) Next() (Event, error) {
	if len(s.events) == 0 {
		if s.err != nil {
			return Event{}, s.err
		}
		return Event{}, io.EOF
	}
	event := s.events[0]
	s.events = s.events[1:]
	return event, nil
}

func (s *settingsFixtureStream) Close() error { s.closed = true; return nil }

func settingsData(payload string) Event { return Event{Kind: EventData, Payload: []byte(payload)} }

func settingsNext(t *testing.T, stream EventStream) Event {
	t.Helper()
	event, err := stream.Next()
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestResponseSettingsThinkingChoiceIsolationAndAccounting(t *testing.T) {
	usage := &Usage{CompletionTokens: 9}
	first := settingsData(`{"id":"chat","vendor":{"keep":true},"choices":[{"index":2,"delta":{"reasoning_content":"R2","content":"A2","vendor":"keep"}},{"index":0,"delta":{"reasoning":"R0"}}]}`)
	first.TextBytes, first.Usage, first.Name = 6, usage, "native-name"
	second := settingsData(`{"id":"chat","choices":[{"index":0,"delta":{"reasoning_content":"more","content":"answer"},"finish_reason":"stop"},{"index":2,"delta":{"content":"tail"},"finish_reason":"stop"}]}`)
	second.TextBytes = 14
	source := &settingsFixtureStream{events: []Event{first, second, {Kind: EventDone}}}
	stream := ApplyResponseSettings(source, &Request{Protocol: ProtocolOpenAIChat, Stream: true}, Target{Settings: map[string]any{"thinking_to_content": true}})
	a := settingsNext(t, stream)
	if a.Kind != first.Kind || a.Usage != usage || a.TextBytes != 6 || a.Name != first.Name {
		t.Fatalf("original accounting/event metadata changed: %+v", a)
	}
	if got := gjson.GetBytes(a.Payload, "choices.0.delta.content").String(); got != "<think>\nR2\n</think>\nA2" {
		t.Fatalf("simultaneous reasoning/content lost: %q", got)
	}
	if got := gjson.GetBytes(a.Payload, "choices.1.delta.content").String(); got != "<think>\nR0" {
		t.Fatalf("choices mixed reasoning: %q", got)
	}
	if !gjson.GetBytes(a.Payload, "vendor.keep").Bool() || gjson.GetBytes(a.Payload, "choices.0.delta.vendor").String() != "keep" {
		t.Fatal("thinking setting pruned vendor fields")
	}
	if gjson.GetBytes(a.Payload, "choices.1.delta.reasoning").Exists() {
		t.Fatal("converted reasoning remained in output")
	}
	b := settingsNext(t, stream)
	if gjson.GetBytes(b.Payload, "choices.0.delta.content").String() != "more\n</think>\nanswer" || gjson.GetBytes(b.Payload, "choices.1.delta.content").String() != "tail" {
		t.Fatalf("choice state did not follow index: %s", b.Payload)
	}
	if b.TextBytes != 14 || settingsNext(t, stream).Kind != EventDone {
		t.Fatal("closing markers entered accounting or terminal changed")
	}
	if err := stream.Close(); err != nil || !source.closed {
		t.Fatal("underlying response not closed")
	}
}

func TestResponseSettingsThinkingOnlyClosesAtTerminal(t *testing.T) {
	for _, termination := range []string{"finish", "done", "eof"} {
		t.Run(termination, func(t *testing.T) {
			first := settingsData(`{"id":"chat","choices":[{"index":1,"delta":{"reasoning_content":"why"}}]}`)
			first.TextBytes = 3
			events := []Event{first}
			if termination == "finish" {
				events = append(events, settingsData(`{"id":"chat","choices":[{"index":1,"delta":{},"finish_reason":"stop"}]}`))
			}
			if termination != "eof" {
				events = append(events, Event{Kind: EventDone})
			}
			stream := ApplyResponseSettings(&settingsFixtureStream{events: events}, &Request{Protocol: ProtocolOpenAIChat, Stream: true}, Target{Settings: map[string]any{"thinking_to_content": true, "force_format": true}})
			if settingsNext(t, stream).TextBytes != 3 {
				t.Fatal("opening marker entered accounting")
			}
			closing := settingsNext(t, stream)
			if closing.Kind != EventData || closing.TextBytes != 0 || closing.Usage != nil || gjson.GetBytes(closing.Payload, "choices.0.delta.content").String() != "\n</think>\n" || gjson.GetBytes(closing.Payload, "choices.0.index").Int() != 1 {
				t.Fatalf("invalid closing event: %+v %s", closing, closing.Payload)
			}
			if termination != "eof" {
				if settingsNext(t, stream).Kind != EventDone {
					t.Fatal("done marker lost")
				}
			}
			if _, err := stream.Next(); !errors.Is(err, io.EOF) {
				t.Fatalf("expected EOF, got %v", err)
			}
		})
	}
}

func TestResponseSettingsFormatProjection(t *testing.T) {
	for _, streaming := range []bool{true, false} {
		t.Run(map[bool]string{true: "stream", false: "json"}[streaming], func(t *testing.T) {
			choiceField := "message"
			if streaming {
				choiceField = "delta"
			}
			payload := `{"id":"chat","created":123,"vendor":"drop","choices":[{"index":0,"vendor":"drop","` + choiceField + `":{"role":"assistant","content":"hello","reasoning_content":"why","vendor":"drop","tool_calls":[{"index":0,"id":"call","type":"function","vendor":"drop","function":{"name":"search","arguments":"{}","vendor":"drop"}}]},"finish_reason":"stop","logprobs":{"preserve":true}}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7,"vendor":12,"prompt_tokens_details":{"cached_tokens":3,"vendor":20}}}`
			event := settingsData(payload)
			event.TextBytes = 8
			stream := ApplyResponseSettings(&settingsFixtureStream{events: []Event{event}}, &Request{Protocol: ProtocolOpenAIChat, Stream: streaming}, Target{Settings: map[string]any{"force_format": true}})
			got := settingsNext(t, stream)
			for _, path := range []string{"vendor", "choices.0.vendor", "choices.0." + choiceField + ".vendor", "usage.vendor", "usage.prompt_tokens_details.vendor"} {
				if gjson.GetBytes(got.Payload, path).Exists() {
					t.Fatalf("unknown field survived force_format: %s", path)
				}
			}
			if gjson.GetBytes(got.Payload, "choices.0."+choiceField+".content").String() != "hello" || gjson.GetBytes(got.Payload, "usage.prompt_tokens_details.cached_tokens").Int() != 3 || got.TextBytes != 8 {
				t.Fatalf("projection corrupted content/accounting: %s", got.Payload)
			}
			toolPath := "choices.0." + choiceField + ".tool_calls.0.vendor"
			if gjson.GetBytes(got.Payload, toolPath).Exists() != !streaming {
				t.Fatalf("tool calls must match legacy stream projection and opaque nonstream DTO: %s", got.Payload)
			}
		})
	}
}

func TestResponseSettingsNoOpAndFailurePaths(t *testing.T) {
	for _, req := range []*Request{{Protocol: ProtocolResponses, Stream: true}, {Protocol: ProtocolAnthropic, Stream: true}, {Protocol: ProtocolGemini, Stream: true}, {Protocol: ProtocolOpenAIChat}} {
		source := &settingsFixtureStream{}
		stream := ApplyResponseSettings(source, req, Target{Settings: map[string]any{"thinking_to_content": true}})
		if stream != source {
			t.Fatalf("unrelated protocol or nonstream conversion enabled: %+v", req)
		}
	}
	upstreamErr := &UpstreamError{Code: "failed"}
	failure := Event{Kind: EventError, Err: upstreamErr, Usage: &Usage{CompletionTokens: 4}, Payload: []byte(`{"error":"keep"}`)}
	readErr := errors.New("upstream cut")
	stream := ApplyResponseSettings(&settingsFixtureStream{events: []Event{failure}, err: readErr}, &Request{Protocol: ProtocolOpenAIChat, Stream: true}, Target{Settings: map[string]any{"force_format": true, "thinking_to_content": true}})
	got := settingsNext(t, stream)
	if got.Kind != failure.Kind || got.Err != upstreamErr || got.Usage != failure.Usage || string(got.Payload) != string(failure.Payload) {
		t.Fatal("failure event changed")
	}
	if _, err := stream.Next(); !errors.Is(err, readErr) {
		t.Fatalf("read failure swallowed: %v", err)
	}
	for _, payload := range []string{"not json", `{"choices":[{"index":"bad","delta":{}}]}`} {
		stream := ApplyResponseSettings(&settingsFixtureStream{events: []Event{settingsData(payload)}}, &Request{Protocol: ProtocolOpenAIChat, Stream: true}, Target{Settings: map[string]any{"thinking_to_content": true}})
		if _, err := stream.Next(); err == nil {
			t.Fatal("invalid response silently accepted")
		}
	}
}

func TestResponseSettingsUsageEventUnmodified(t *testing.T) {
	event := Event{Kind: EventUsage, Usage: &Usage{CompletionTokens: 10}, TextBytes: 5}
	stream := ApplyResponseSettings(&settingsFixtureStream{events: []Event{event}}, &Request{Protocol: ProtocolOpenAIChat, Stream: true}, Target{Settings: map[string]any{"force_format": true}})
	got := settingsNext(t, stream)
	if got.Kind != event.Kind || got.Usage != event.Usage || got.TextBytes != 5 {
		t.Fatal("usage-only event changed")
	}
}

func TestResponseSettingsNonstreamReasoningPreserved(t *testing.T) {
	event := settingsData(`{"choices":[{"index":0,"message":{"role":"assistant","content":"answer","reasoning_content":"why"}}]}`)
	stream := ApplyResponseSettings(&settingsFixtureStream{events: []Event{event}}, &Request{Protocol: ProtocolOpenAIChat}, Target{Settings: map[string]any{"force_format": true, "thinking_to_content": true}})
	got := settingsNext(t, stream)
	if strings.Contains(string(got.Payload), "<think>") || gjson.GetBytes(got.Payload, "choices.0.message.reasoning_content").String() != "why" {
		t.Fatal("stream-only reasoning conversion changed nonstream body")
	}
}
