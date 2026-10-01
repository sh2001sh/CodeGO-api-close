package anthropic

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

// Regression: lifecycle/role frames before an upstream error must not commit
// client headers; the gateway can then fail over without exposing the attempt.
func TestLifecycleBeforeErrorRemainsInvisible(t *testing.T) {
	fixture := sseFixture(
		`{"type":"message_start","message":{"id":"m","usage":{"input_tokens":20}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`)
	for _, protocol := range []gateway.Protocol{gateway.ProtocolAnthropic, gateway.ProtocolOpenAIChat} {
		stream := decodeFixture(protocol, fixture, "{}", true)
		ev, err := stream.Next()
		if err != nil || ev.Kind != gateway.EventError || ev.Err.Code != "overloaded_error" {
			t.Fatalf("lifecycle became visible for %d: %+v %v", protocol, ev, err)
		}
		_ = stream.Close()
	}
}

func TestLifecycleOnlyCompletedStreamPreservesUsageAndRemainsEmpty(t *testing.T) {
	fixture := sseFixture(
		`{"type":"message_start","message":{"id":"m","usage":{"input_tokens":20}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		`{"type":"message_stop"}`)
	for _, protocol := range []gateway.Protocol{gateway.ProtocolAnthropic, gateway.ProtocolOpenAIChat} {
		stream := decodeFixture(protocol, fixture, `{"stream_options":{"include_usage":true}}`, true)
		ev, err := stream.Next()
		if err != nil || ev.Kind != gateway.EventUsage || ev.Usage.PromptTokens != 20 || ev.Usage.CompletionTokens != 2 {
			t.Fatalf("empty stream usage lost for %d: %+v %v", protocol, ev, err)
		}
		ev, err = stream.Next()
		if err != nil || ev.Kind != gateway.EventDone {
			t.Fatalf("empty stream committed data for %d: %+v %v", protocol, ev, err)
		}
		if _, err = stream.Next(); !errors.Is(err, io.EOF) {
			t.Fatalf("expected EOF after empty stream: %v", err)
		}
		_ = stream.Close()
	}
}

func TestPreambleBounds(t *testing.T) {
	count := strings.Repeat(sseFixture(`{"type":"message_start","message":{"id":"m"}}`), maxLifecycleEvents+1)
	large := sseFixture(`{"type":"message_start","message":{"id":"` + strings.Repeat("x", maxLifecycleBytes) + `"}}`)
	for _, protocol := range []gateway.Protocol{gateway.ProtocolAnthropic, gateway.ProtocolOpenAIChat} {
		for _, fixture := range []string{count, large} {
			stream := decodeFixture(protocol, fixture, "{}", true)
			ev, err := stream.Next()
			if err != nil || ev.Kind != gateway.EventError || !strings.Contains(ev.Err.Message, "preamble exceeds") {
				t.Fatalf("unbounded lifecycle preamble for %d: %+v %v", protocol, ev, err)
			}
			_ = stream.Close()
		}
	}
}

func TestToolStartOpensGateAndFlushesLifecycleInOrder(t *testing.T) {
	fixture := sseFixture(
		`{"type":"message_start","message":{"id":"m","usage":{"input_tokens":3}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool1","name":"lookup","input":{}}}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":1}}`,
		`{"type":"message_stop"}`)
	for _, protocol := range []gateway.Protocol{gateway.ProtocolAnthropic, gateway.ProtocolOpenAIChat} {
		stream := decodeFixture(protocol, fixture, "{}", true)
		first, err := stream.Next()
		if err != nil || first.Kind != gateway.EventData {
			t.Fatalf("failed to flush lifecycle: %+v %v", first, err)
		}
		second, err := stream.Next()
		if err != nil || second.Kind != gateway.EventData {
			t.Fatalf("tool output missing: %+v %v", second, err)
		}
		if protocol == gateway.ProtocolAnthropic {
			if first.Name != "message_start" || second.Name != "content_block_start" {
				t.Fatalf("native ordering changed: %s %s", first.Name, second.Name)
			}
		} else if gjson.GetBytes(first.Payload, "choices.0.delta.role").Str != "assistant" || gjson.GetBytes(second.Payload, "choices.0.delta.tool_calls.0.id").Str != "tool1" {
			t.Fatalf("Chat ordering changed: %s %s", first.Payload, second.Payload)
		}
		_ = stream.Close()
	}
}
