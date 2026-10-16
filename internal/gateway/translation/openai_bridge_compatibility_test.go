package translation

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestChatToResponsesPreservesMediaAndChatDefaults(t *testing.T) {
	var request dto.GeneralOpenAIRequest
	require.NoError(t, json.Unmarshal([]byte(`{
		"model":"test","max_tokens":200,"max_completion_tokens":100,
		"messages":[{"role":"user","content":[
			{"type":"image_url","image_url":{"url":"https://example.com/a.png","detail":"low"}},
			{"type":"file","file":{"filename":"document.pdf","file_data":"data:application/pdf;base64,YQ=="}}
		]}],
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"q":{"type":"string"}}}}}]
	}`), &request))
	converted, err := ChatCompletionsRequestToResponsesRequest(&request)
	require.NoError(t, err)
	require.EqualValues(t, 100, *converted.MaxOutputTokens)
	var input []struct {
		Content []map[string]any `json:"content"`
	}
	require.NoError(t, json.Unmarshal(converted.Input, &input))
	require.Equal(t, "low", input[0].Content[0]["detail"])
	require.Equal(t, "document.pdf", input[0].Content[1]["filename"])
	require.Equal(t, "data:application/pdf;base64,YQ==", input[0].Content[1]["file_data"])
	require.NotContains(t, input[0].Content[1], "file")
	var tools []map[string]any
	require.NoError(t, json.Unmarshal(converted.Tools, &tools))
	require.Equal(t, false, tools[0]["strict"])
}

func TestResponsesToChatPreservesModernLimitsAndRequestMetadata(t *testing.T) {
	var request dto.OpenAIResponsesRequest
	require.NoError(t, json.Unmarshal([]byte(`{
		"model":"test","max_output_tokens":123,"prompt_cache_retention":"24h","safety_identifier":"user-hash",
		"input":[{"role":"user","content":[{"type":"input_file","filename":"a.pdf","file_data":"data:application/pdf;base64,YQ=="}]}]
	}`), &request))
	chat, _, err := ResponsesRequestToChatCompletionsRequest(&request)
	require.NoError(t, err)
	require.Nil(t, chat.MaxTokens)
	require.EqualValues(t, 123, *chat.MaxCompletionTokens)
	require.Equal(t, request.PromptCacheRetention, chat.PromptCacheRetention)
	require.Equal(t, request.SafetyIdentifier, chat.SafetyIdentifier)
	encoded, err := json.Marshal(chat)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"filename":"a.pdf"`)
	require.NotContains(t, string(encoded), `"file_name"`)
}

func TestBridgeIncompleteReasonsAndReasoningUsageRoundTrip(t *testing.T) {
	for _, finish := range []string{"length", "content_filter"} {
		t.Run(finish, func(t *testing.T) {
			chat := &dto.OpenAITextResponse{
				Choices: []dto.OpenAITextResponseChoice{{Message: dto.Message{Content: "partial"}, FinishReason: finish}},
				Usage:   dto.Usage{PromptTokens: 11, CompletionTokens: 9, TotalTokens: 20, CompletionTokenDetails: dto.OutputTokenDetails{ReasoningTokens: 7}},
			}
			responses, _, err := ChatCompletionsResponseToResponsesResponse(chat, nil, nil)
			require.NoError(t, err)
			encoded, err := json.Marshal(responses)
			require.NoError(t, err)
			reason := "max_output_tokens"
			if finish == "content_filter" {
				reason = finish
			}
			require.Contains(t, string(encoded), `"incomplete_details":{"reason":"`+reason+`"}`)
			require.Contains(t, string(encoded), `"output_tokens_details"`)
			// Decode the wire shape to verify the Responses spelling is read too.
			var fromWire dto.OpenAIResponsesResponse
			require.NoError(t, json.Unmarshal(encoded, &fromWire))
			result, usage, err := ResponsesResponseToChatCompletionsResponse(&fromWire, "chat-test")
			require.NoError(t, err)
			require.Equal(t, finish, result.Choices[0].FinishReason)
			require.Equal(t, 7, usage.CompletionTokenDetails.ReasoningTokens)
		})
	}
}

func TestChatToResponsesRejectsTruncatedStream(t *testing.T) {
	state := NewChatToResponsesStreamState(nil, nil)
	text := "partial output"
	events, err := state.ConvertChunk(&dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: &text}}}})
	require.NoError(t, err)
	require.Equal(t, "response.output_text.delta", events[len(events)-1].Type)
	_, _, err = state.Finalize()
	require.ErrorContains(t, err, "finish_reason")
}

func TestChatToResponsesSparseToolsAndDelayedCallID(t *testing.T) {
	state := NewChatToResponsesStreamState(nil, nil)
	index := 7
	chunks := []dto.ToolCallResponse{
		{Index: &index, Function: dto.FunctionResponse{Name: "lookup", Arguments: `{"q":`}},
		{Index: &index, ID: "call_real", Function: dto.FunctionResponse{Arguments: `"hello"}`}},
	}
	var events []ResponsesBridgeEvent
	for _, tool := range chunks {
		converted, err := state.ConvertChunk(&dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{tool}}}}})
		require.NoError(t, err)
		events = append(events, converted...)
	}
	finish := "tool_calls"
	_, err := state.ConvertChunk(&dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: &finish}}})
	require.NoError(t, err)
	final, _, err := state.Finalize()
	require.NoError(t, err)
	events = append(events, final...)
	output := final[len(final)-1].Response.Output
	require.Len(t, output, 1)
	require.Equal(t, "call_real", output[0].CallId)
	require.Equal(t, `{"q":"hello"}`, output[0].ArgumentsString())
	for _, event := range events {
		if event.Type == "response.output_item.added" {
			require.Equal(t, "call_real", event.Item.CallId)
		}
	}
	negative := -1
	_, err = state.addToolDelta(dto.ToolCallResponse{Index: &negative})
	require.Error(t, err)
}

func TestCustomToolStreamingDecodesFragmentedEscapes(t *testing.T) {
	for _, arguments := range []string{`{"input":"echo \"a\"\n\u4f60\u597d\ud83d\ude00"}`, `{"input":""}`} {
		meta := NewResponsesChatBridgeMeta()
		meta.CustomToolNames["shell"] = struct{}{}
		state := NewChatToResponsesStreamState(nil, meta)
		var result strings.Builder
		for index := range len(arguments) {
			call := dto.ToolCallResponse{Function: dto.FunctionResponse{Arguments: arguments[index : index+1]}}
			if index == 0 {
				call.ID = "call_shell"
				call.Function.Name = "shell"
			}
			events, err := state.addToolDelta(call)
			require.NoError(t, err)
			for _, event := range events {
				if event.Type == "response.custom_tool_call_input.delta" {
					result.WriteString(event.Delta)
				}
			}
		}
		var expected struct {
			Input string `json:"input"`
		}
		require.NoError(t, json.Unmarshal([]byte(arguments), &expected))
		events, err := state.closeTools()
		require.NoError(t, err)
		for _, event := range events {
			if event.Type == "response.custom_tool_call_input.delta" {
				result.WriteString(event.Delta)
			}
			if event.Type == "response.custom_tool_call_input.done" {
				require.Equal(t, expected.Input, event.Input)
			}
		}
		require.Equal(t, expected.Input, result.String())
	}
}

func TestChatToResponsesRefusalAndUniqueOutputItemIDs(t *testing.T) {
	state := NewChatToResponsesStreamState(nil, nil)
	text, refusal, finish := "before", "cannot assist", "content_filter"
	chunks := []dto.ChatCompletionsStreamResponseChoice{
		{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: &text}},
		{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{ID: "call_1", Function: dto.FunctionResponse{Name: "test", Arguments: `{}`}}}}},
		{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Refusal: &refusal}, FinishReason: &finish},
	}
	for _, choice := range chunks {
		_, err := state.ConvertChunk(&dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{choice}})
		require.NoError(t, err)
	}
	events, _, err := state.Finalize()
	require.NoError(t, err)
	completed := events[len(events)-1]
	require.Equal(t, "response.incomplete", completed.Type)
	ids := map[string]bool{}
	for _, output := range completed.Response.Output {
		require.False(t, ids[output.ID])
		ids[output.ID] = true
	}
	chat, _, err := ResponsesResponseToChatCompletionsResponse(completed.Response, "chat-test")
	require.NoError(t, err)
	require.NotNil(t, chat.Choices[0].Refusal)
	require.Equal(t, refusal, *chat.Choices[0].Refusal)
}

func TestResponsesToolImagesFollowAllParallelToolResults(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{Model: "test", Input: json.RawMessage(`[
		{"type":"function_call","name":"view_image","call_id":"a","arguments":"{}"},
		{"type":"function_call","name":"inspect","call_id":"b","arguments":"{}"},
		{"type":"function_call_output","call_id":"a","output":[{"type":"input_text","text":"screenshot"},{"type":"input_image","image_url":"data:image/png;base64,YQ==","detail":"low"}]},
		{"type":"function_call_output","call_id":"b","output":"done"},
		{"role":"user","content":"continue"}
	]`)}
	chat, _, err := ResponsesRequestToChatCompletionsRequest(request)
	require.NoError(t, err)
	require.Len(t, chat.Messages, 5)
	require.Equal(t, []string{"assistant", "tool", "tool", "user", "user"}, []string{chat.Messages[0].Role, chat.Messages[1].Role, chat.Messages[2].Role, chat.Messages[3].Role, chat.Messages[4].Role})
	require.Equal(t, "screenshot", chat.Messages[1].Content)
	encoded, err := json.Marshal(chat.Messages[3])
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"image_url":{"detail":"low","url":"data:image/png;base64,YQ=="}`)
	require.NotContains(t, chat.Messages[1].Content, "base64")
	request.Input = json.RawMessage(`[{"role":"user","content":[{"type":"input_file","file_url":"https://example.com/a.pdf"}]}]`)
	_, _, err = ResponsesRequestToChatCompletionsRequest(request)
	require.ErrorContains(t, err, "native Responses")
}

func TestResponsesToChatRejectsOrphanedToolOutput(t *testing.T) {
	_, _, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{Model: "test", Input: json.RawMessage(`[{"type":"function_call_output","call_id":"missing","output":"done"}]`)})
	require.ErrorIs(t, err, ErrIncompleteToolCallContext)
}
