package runtime

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestStreamOutputTimingSeparatesProtocolMetadataReasoningToolsAndText(t *testing.T) {
	for _, test := range []struct {
		name, protocol, payload string
		semantic, text          bool
	}{
		{"chat role", "chat", `{"choices":[{"delta":{"role":"assistant"}}]}`, false, false},
		{"chat usage", "chat", `{"choices":[],"usage":{"completion_tokens":5}}`, false, false},
		{"chat reasoning", "chat", `{"choices":[{"delta":{"reasoning_content":"thinking"}}]}`, true, false},
		{"chat tool", "chat", `{"choices":[{"delta":{"tool_calls":[{"id":"call_1","type":"function"}]}}]}`, true, false},
		{"chat empty tool", "chat", `{"choices":[{"delta":{"tool_calls":[{"index":0}]}}]}`, false, false},
		{"chat text", "chat", `{"choices":[{"delta":{"content":"answer"}}]}`, true, true},
		{"message start", "claude", `{"type":"message_start","message":{"id":"msg_1"}}`, false, false},
		{"empty text block", "claude", `{"type":"content_block_start","content_block":{"type":"text","text":""}}`, false, false},
		{"message signature", "claude", `{"type":"content_block_delta","delta":{"type":"signature_delta","signature":"sig"}}`, false, false},
		{"message thinking", "claude", `{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"thinking"}}`, true, false},
		{"message tool", "claude", `{"type":"content_block_start","content_block":{"type":"tool_use","id":"call_1","name":"search"}}`, true, false},
		{"message text", "claude", `{"type":"content_block_delta","delta":{"type":"text_delta","text":"answer"}}`, true, true},
		{"responses created", "responses", `{"type":"response.created","response":{"id":"resp_1"}}`, false, false},
		{"responses reasoning", "responses", `{"type":"response.reasoning_summary_text.delta","delta":"thinking"}`, true, false},
		{"responses tool", "responses", `{"type":"response.function_call_arguments.delta","delta":"{}"}`, true, false},
		{"responses empty delta", "responses", `{"type":"response.output_text.delta","delta":""}`, false, false},
		{"responses text", "responses", `{"type":"response.output_text.delta","delta":"answer"}`, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			start := time.Now().Add(-time.Second)
			info := &RelayInfo{StartTime: start, IsStream: true, isFirstResponse: true, FirstByteTrace: NewFirstByteTrace(start)}
			info.SetFirstResponseTime() // A raw event alone must not produce semantic timing.
			switch test.protocol {
			case "chat":
				var response dto.ChatCompletionsStreamResponse
				require.NoError(t, json.Unmarshal([]byte(test.payload), &response))
				info.ObserveChatStreamOutput(response, time.Now())
			case "claude":
				var response dto.ClaudeResponse
				require.NoError(t, json.Unmarshal([]byte(test.payload), &response))
				info.ObserveClaudeStreamOutput(response, time.Now())
			case "responses":
				var response dto.ResponsesStreamResponse
				require.NoError(t, json.Unmarshal([]byte(test.payload), &response))
				info.ObserveResponsesStreamOutput(response, time.Now())
			}
			require.Equal(t, test.semantic, info.HasSemanticResponse())
			_, hasTTFT := info.EndToEndTTFT()
			require.Equal(t, test.semantic, hasTTFT)
			trace := info.FirstByteTrace.Snapshot()
			if test.text {
				require.Contains(t, trace, "total_text_ms")
				require.Contains(t, trace, "e2e_first_text_ms")
			} else {
				require.NotContains(t, trace, "total_text_ms")
				require.NotContains(t, trace, "e2e_first_text_ms")
			}
		})
	}
}
