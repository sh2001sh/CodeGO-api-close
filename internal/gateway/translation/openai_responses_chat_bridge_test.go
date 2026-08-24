package translation

import (
	"encoding/json"
	"testing"

	"github.com/sh2001sh/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestResponsesRequestToChatCompletionsPreservesReasoningToolsAndMedia(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[
				{"type":"input_text","text":"inspect"},
				{"type":"input_image","image_url":"https://example.com/a.png","detail":"high"}
			]},
			{"type":"reasoning","summary":[{"type":"summary_text","text":"use gmail"}]},
			{"type":"function_call","call_id":"call_1","name":"send","namespace":"gmail","arguments":"{\"to\":\"a\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"sent"}
		]`),
		Tools: json.RawMessage(`[{"type":"namespace","name":"gmail","tools":[
			{"type":"function","name":"send","parameters":{"type":"object"}},
			{"type":"custom","name":"exec","description":"run command"}
		]}]`),
	}

	chat, meta, err := ResponsesRequestToChatCompletionsRequest(request)
	require.NoError(t, err)
	require.Len(t, chat.Messages, 3)
	contentParts, ok := chat.Messages[0].Content.([]dto.MediaContent)
	require.True(t, ok)
	require.Len(t, contentParts, 2)
	require.Equal(t, "use gmail", chat.Messages[1].GetReasoningContent())
	require.Equal(t, "gmail__send", chat.Messages[1].ParseToolCalls()[0].Function.Name)
	require.Equal(t, "call_1", chat.Messages[2].ToolCallId)
	require.Len(t, chat.Tools, 2)
	require.Equal(t, "gmail__send", chat.Tools[0].Function.Name)
	require.Equal(t, "gmail__exec", chat.Tools[1].Function.Name)
	require.Contains(t, meta.CustomToolNames, "gmail__exec")
	require.Equal(t, ResponsesNamespaceTool{Name: "send", Namespace: "gmail"}, meta.NamespaceTools["gmail__send"])
}

func TestResponsesRequestToChatCompletionsRejectsPreviousResponseID(t *testing.T) {
	_, _, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol", PreviousResponseID: "resp_previous",
	})
	require.ErrorContains(t, err, "previous_response_id")
}

func TestChatCompletionsResponseToResponsesPreservesMixedOutput(t *testing.T) {
	message := dto.Message{Role: "assistant", Content: "I will call the tool."}
	reasoning := "tool required"
	message.ReasoningContent = &reasoning
	message.SetToolCalls([]dto.ToolCallRequest{{
		ID: "call_1", Type: "function",
		Function: dto.FunctionRequest{Name: "gmail__send", Arguments: `{"to":"a"}`},
	}})
	response := &dto.OpenAITextResponse{
		Id: "chatcmpl-123", Model: "gpt-5.6-sol", Created: int64(10),
		Choices: []dto.OpenAITextResponseChoice{{Message: message, FinishReason: "tool_calls"}},
		Usage:   dto.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
	meta := NewResponsesChatBridgeMeta()
	meta.NamespaceTools["gmail__send"] = ResponsesNamespaceTool{Name: "send", Namespace: "gmail"}

	converted, usage, err := ChatCompletionsResponseToResponsesResponse(response, &dto.OpenAIResponsesRequest{Model: "gpt-5.6-sol"}, meta)
	require.NoError(t, err)
	require.Len(t, converted.Output, 3)
	require.Equal(t, "reasoning", converted.Output[0].Type)
	require.Equal(t, "message", converted.Output[1].Type)
	require.Equal(t, "function_call", converted.Output[2].Type)
	require.Equal(t, "gmail", converted.Output[2].Namespace)
	require.Equal(t, "send", converted.Output[2].Name)
	require.Equal(t, 10, usage.InputTokens)
	require.Equal(t, 5, usage.OutputTokens)
}

func TestChatToResponsesStreamLifecyclePreservesReasoningTextAndTool(t *testing.T) {
	state := NewChatToResponsesStreamState(&dto.OpenAIResponsesRequest{Model: "gpt-5.6-sol"}, NewResponsesChatBridgeMeta())
	reasoning := "think"
	content := "answer"
	firstArgs := `{"x":`
	secondArgs := `1}`
	finishReason := "tool_calls"

	chunks := []*dto.ChatCompletionsStreamResponse{
		{Id: "chatcmpl-1", Model: "gpt-5.6-sol", Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ReasoningContent: &reasoning},
		}}},
		{Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: &content},
		}}},
		{Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
				Index: intTestPointer(0), ID: "call_1", Type: "function",
				Function: dto.FunctionResponse{Name: "lookup", Arguments: firstArgs},
			}}},
		}}},
		{Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0, FinishReason: &finishReason,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
				Index: intTestPointer(0), Function: dto.FunctionResponse{Arguments: secondArgs},
			}}},
		}}, Usage: &dto.Usage{PromptTokens: 8, CompletionTokens: 4, TotalTokens: 12}},
	}

	var events []ResponsesBridgeEvent
	for _, chunk := range chunks {
		converted, err := state.ConvertChunk(chunk)
		require.NoError(t, err)
		events = append(events, converted...)
	}
	finalEvents, usage, err := state.Finalize()
	require.NoError(t, err)
	events = append(events, finalEvents...)

	requireEventOrder(t, events,
		"response.created",
		"response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.done",
		"response.output_text.delta",
		"response.output_text.done",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.completed",
	)
	require.Equal(t, 12, usage.TotalTokens)
	completed := events[len(events)-1].Response
	require.NotNil(t, completed)
	require.Len(t, completed.Output, 3)
	require.Equal(t, `{"x":1}`, completed.Output[2].ArgumentsString())
}

func requireEventOrder(t *testing.T, events []ResponsesBridgeEvent, expected ...string) {
	t.Helper()
	position := 0
	for _, event := range events {
		if position < len(expected) && event.Type == expected[position] {
			position++
		}
	}
	require.Equal(t, len(expected), position)
}

func intTestPointer(value int) *int { return &value }
