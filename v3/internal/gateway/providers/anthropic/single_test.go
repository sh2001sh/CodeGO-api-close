package anthropic

import (
	"errors"
	"io"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestSingleNativeAndChatToolResponse(t *testing.T) {
	data := `{"id":"msg1","type":"message","content":[{"type":"thinking","thinking":"consider"},{"type":"tool_use","id":"t1","name":"lookup","input":{"q":"weather"}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":4,"cache_read_input_tokens":20,"cache_creation_input_tokens":6}}`
	for _, protocol := range []gateway.Protocol{gateway.ProtocolAnthropic, gateway.ProtocolOpenAIChat} {
		stream := decodeFixture(protocol, data, "{}", false)
		ev, err := stream.Next()
		if err != nil {
			t.Fatal(err)
		}
		if ev.Kind != gateway.EventData || ev.Usage == nil || ev.Usage.PromptTokens != 36 || ev.Usage.CompletionTokens != 4 || ev.Usage.CacheWriteTokens != 6 {
			t.Fatalf("incorrect single event: %+v", ev)
		}
		if protocol == gateway.ProtocolAnthropic {
			if string(ev.Payload) != data {
				t.Fatal("native response changed")
			}
		} else {
			if gjson.GetBytes(ev.Payload, "choices.0.message.content").Type != gjson.Null || gjson.GetBytes(ev.Payload, "choices.0.message.reasoning_content").Str != "consider" || gjson.GetBytes(ev.Payload, "choices.0.finish_reason").Str != "tool_calls" || gjson.GetBytes(ev.Payload, "choices.0.message.tool_calls.0.function.arguments").Str != `{"q":"weather"}` {
				t.Fatalf("lost tool/reasoning fields: %s", ev.Payload)
			}
		}
		if _, err := stream.Next(); !errors.Is(err, io.EOF) {
			t.Fatalf("expected EOF, got %v", err)
		}
		if err := stream.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSingleErrorAndMissingUsage(t *testing.T) {
	for _, data := range []string{`{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`, `{bad`, `{"content":[{"type":"server_tool_use"}]}`} {
		stream := decodeFixture(gateway.ProtocolOpenAIChat, data, "{}", false)
		ev, err := stream.Next()
		if err != nil || ev.Kind != gateway.EventError {
			t.Fatalf("error not decoded: %s %+v %v", data, ev, err)
		}
		_ = stream.Close()
	}
	stream := decodeFixture(gateway.ProtocolOpenAIChat, `{"id":"m","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn"}`, "{}", false)
	defer func() { _ = stream.Close() }()
	ev, err := stream.Next()
	if err != nil || ev.Usage != nil || ev.TextBytes != 2 || gjson.GetBytes(ev.Payload, "choices.0.message.content").Str != "hi" {
		t.Fatalf("missing usage must remain estimable: %+v %v", ev, err)
	}
}

func TestUsageExplicitZeroOverridesPreviousCount(t *testing.T) {
	state := usageState{}
	input, cache, output := int64(3), int64(8), int64(4)
	if _, err := state.merge(&wireUsage{Input: &input, CacheRead: &cache, Output: &output}); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	u, err := state.merge(&wireUsage{CacheRead: &zero, Output: &zero})
	if err != nil || u.PromptTokens != 3 || u.CachedTokens != 0 || u.CompletionTokens != 0 {
		t.Fatalf("explicit zero lost: %+v %v", u, err)
	}
}

func TestEmptyNativeResponseRemainsEmpty(t *testing.T) {
	stream := decodeFixture(gateway.ProtocolAnthropic, `{"content":[],"usage":{"input_tokens":0,"output_tokens":0}}`, "{}", false)
	defer func() { _ = stream.Close() }()
	if _, err := stream.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("empty upstream must not commit client headers: %v", err)
	}
}
