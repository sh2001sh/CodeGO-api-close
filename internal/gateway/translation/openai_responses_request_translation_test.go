package translation

import (
	"encoding/json"
	"testing"

	"github.com/sh2001sh/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestChatCompletionsToResponsesPreservesToolCallPair(t *testing.T) {
	request := chatRequestWithMessages(
		dto.Message{Role: "user", Content: "find the weather"},
		assistantToolCall("call_weather", "get_weather"),
		dto.Message{Role: "tool", ToolCallId: "call_weather", Content: `{"temperature":20}`},
	)

	converted, err := ChatCompletionsRequestToResponsesRequest(request)
	require.NoError(t, err)

	items := responseInputItems(t, converted)
	require.Len(t, items, 4)
	require.Equal(t, "assistant", items[1]["role"])
	require.Equal(t, "function_call", items[2]["type"])
	require.Equal(t, "call_weather", items[2]["call_id"])
	require.Equal(t, "function_call_output", items[3]["type"])
	require.Equal(t, "call_weather", items[3]["call_id"])
}

func TestChatCompletionsToResponsesSupportsParallelToolCalls(t *testing.T) {
	assistant := dto.Message{Role: "assistant"}
	assistant.SetToolCalls([]dto.ToolCallRequest{
		{ID: "call_weather", Type: "function", Function: dto.FunctionRequest{Name: "get_weather"}},
		{ID: "call_time", Type: "function", Function: dto.FunctionRequest{Name: "get_time"}},
	})
	request := chatRequestWithMessages(
		dto.Message{Role: "user", Content: "weather and time"},
		assistant,
		dto.Message{Role: "tool", ToolCallId: "call_weather", Content: "sunny"},
		dto.Message{Role: "tool", ToolCallId: "call_time", Content: "10:00"},
	)

	converted, err := ChatCompletionsRequestToResponsesRequest(request)
	require.NoError(t, err)

	items := responseInputItems(t, converted)
	require.Len(t, items, 6)
	require.Equal(t, "call_weather", items[4]["call_id"])
	require.Equal(t, "call_time", items[5]["call_id"])
}

func TestChatCompletionsToResponsesAllowsEmptyToolCallList(t *testing.T) {
	request := chatRequestWithMessages(dto.Message{
		Role:      "assistant",
		Content:   "No tool is needed.",
		ToolCalls: json.RawMessage(`[]`),
	})

	_, err := ChatCompletionsRequestToResponsesRequest(request)
	require.NoError(t, err)
}

func TestChatCompletionsToResponsesRejectsIncompleteToolHistory(t *testing.T) {
	testCases := []struct {
		name     string
		messages []dto.Message
		contains string
	}{
		{
			name:     "missing output",
			messages: []dto.Message{assistantToolCall("call_weather", "get_weather")},
			contains: "tool output is missing",
		},
		{
			name: "missing tool call id",
			messages: []dto.Message{
				assistantToolCall("call_weather", "get_weather"),
				{Role: "tool", Content: "sunny"},
			},
			contains: "missing tool_call_id",
		},
		{
			name: "unknown tool call id",
			messages: []dto.Message{
				assistantToolCall("call_weather", "get_weather"),
				{Role: "tool", ToolCallId: "call_time", Content: "10:00"},
			},
			contains: "does not match a pending function call",
		},
		{
			name: "duplicate tool output",
			messages: []dto.Message{
				assistantToolCall("call_weather", "get_weather"),
				{Role: "tool", ToolCallId: "call_weather", Content: "sunny"},
				{Role: "tool", ToolCallId: "call_weather", Content: "sunny"},
			},
			contains: "does not match a pending function call",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ChatCompletionsRequestToResponsesRequest(chatRequestWithMessages(testCase.messages...))
			require.ErrorIs(t, err, ErrIncompleteToolCallContext)
			require.ErrorContains(t, err, testCase.contains)
		})
	}
}

func chatRequestWithMessages(messages ...dto.Message) *dto.GeneralOpenAIRequest {
	return &dto.GeneralOpenAIRequest{Model: "gpt-5.6-sol", Messages: messages}
}

func assistantToolCall(callID, name string) dto.Message {
	message := dto.Message{Role: "assistant"}
	message.SetToolCalls([]dto.ToolCallRequest{{
		ID:       callID,
		Type:     "function",
		Function: dto.FunctionRequest{Name: name, Arguments: `{}`},
	}})
	return message
}

func responseInputItems(t *testing.T, request *dto.OpenAIResponsesRequest) []map[string]any {
	t.Helper()
	var items []map[string]any
	require.NoError(t, json.Unmarshal(request.Input, &items))
	return items
}
