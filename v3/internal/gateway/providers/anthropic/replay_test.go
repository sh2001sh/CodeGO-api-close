package anthropic

import (
	"io"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestChatStreamWithoutClientUsageChunk(t *testing.T) {
	fixture := sseFixture(
		`{"type":"message_start","message":{"id":"m","usage":{"input_tokens":2}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":3}}`,
		`{"type":"message_stop"}`)
	stream := decodeFixture(gateway.ProtocolOpenAIChat, fixture, "{}", true)
	defer func() { _ = stream.Close() }()
	var last gateway.Event
	count := 0
	for {
		ev, err := stream.Next()
		if err != nil {
			t.Fatal(err)
		}
		if ev.Kind == gateway.EventDone {
			break
		}
		count++
		last = ev
		if gjson.GetBytes(ev.Payload, "usage").Exists() {
			t.Fatal("usage forwarded without client request")
		}
	}
	if count != 3 || gjson.GetBytes(last.Payload, "choices.0.finish_reason").Str != "length" || last.Usage == nil || last.Usage.PromptTokens != 2 || last.Usage.CompletionTokens != 3 {
		t.Fatalf("billing usage or finish reason lost: %d %+v", count, last)
	}
}

func TestEmptyToolResultAndNoopOptions(t *testing.T) {
	data := []byte(`{"n":1,"frequency_penalty":0,"presence_penalty":0,"logprobs":false,"response_format":{"type":"text"},"messages":[{"role":"tool","tool_call_id":"t","content":""}]}`)
	out, err := convertRequest(data, "m")
	if err != nil {
		t.Fatal(err)
	}
	if got := gjson.GetBytes(out, "messages.0.content.0.content"); got.Type != gjson.String || got.Str != "" {
		t.Fatalf("empty tool result must remain a string: %s", out)
	}
}

func BenchmarkMessagesStream(b *testing.B) {
	fixture := sseFixture(
		`{"type":"message_start","message":{"id":"m","usage":{"input_tokens":2}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`,
		`{"type":"message_stop"}`)
	b.ReportAllocs()
	for _, protocol := range []gateway.Protocol{gateway.ProtocolAnthropic, gateway.ProtocolOpenAIChat} {
		name := "native"
		if protocol == gateway.ProtocolOpenAIChat {
			name = "chat"
		}
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				stream := decodeFixture(protocol, fixture, "{}", true)
				for {
					ev, err := stream.Next()
					if err != nil && err != io.EOF {
						b.Fatal(err)
					}
					if err == io.EOF || ev.Kind == gateway.EventDone {
						break
					}
				}
				if err := stream.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
