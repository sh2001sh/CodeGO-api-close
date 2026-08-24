package translation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/dto"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
)

func ChatCompletionsResponseToResponsesResponse(
	resp *dto.OpenAITextResponse,
	req *dto.OpenAIResponsesRequest,
	meta *ResponsesChatBridgeMeta,
) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, errors.New("chat completions response is nil")
	}
	if len(resp.Choices) == 0 {
		return nil, nil, errors.New("chat completions response has no choices")
	}
	if len(resp.Choices) > 1 {
		return nil, nil, errors.New("multiple chat completion choices cannot be represented by one responses result")
	}

	choice := resp.Choices[0]
	responseID := responsesIDFromChat(resp.Id)
	output := chatMessageToResponsesOutput(choice.Message, responseID, meta)
	usage := chatUsageToResponsesUsage(resp.Usage)
	status := "completed"
	var incomplete *dto.IncompleteDetails
	if choice.FinishReason == "length" {
		status = "incomplete"
		incomplete = &dto.IncompleteDetails{Reasoning: "max_output_tokens"}
	}
	statusRaw, _ := platformencoding.Marshal(status)

	result := &dto.OpenAIResponsesResponse{
		ID:                responseID,
		Object:            "response",
		CreatedAt:         chatCreatedAt(resp.Created),
		Status:            statusRaw,
		IncompleteDetails: incomplete,
		Model:             resp.Model,
		Output:            output,
		Usage:             usage,
	}
	if result.Model == "" && req != nil {
		result.Model = req.Model
	}
	applyResponsesRequestEcho(result, req)
	return result, usage, nil
}

func chatMessageToResponsesOutput(message dto.Message, responseID string, meta *ResponsesChatBridgeMeta) []dto.ResponsesOutput {
	output := make([]dto.ResponsesOutput, 0, 2)
	if reasoning := chatMessageReasoning(message); reasoning != "" {
		output = append(output, dto.ResponsesOutput{
			Type: "reasoning", ID: "rs_" + responseID,
			Status:  "completed",
			Summary: []dto.ResponsesReasoningSummaryPart{{Type: "summary_text", Text: reasoning}},
		})
	}
	if text := message.StringContent(); text != "" {
		output = append(output, dto.ResponsesOutput{
			Type: "message", ID: "msg_" + responseID, Status: "completed", Role: "assistant",
			Content: []dto.ResponsesOutputContent{{Type: "output_text", Text: text, Annotations: []interface{}{}}},
		})
	}
	for index, call := range message.ParseToolCalls() {
		output = append(output, chatToolCallToResponsesOutput(call, responseID, index, meta))
	}
	return output
}

func chatToolCallToResponsesOutput(call dto.ToolCallRequest, responseID string, index int, meta *ResponsesChatBridgeMeta) dto.ResponsesOutput {
	name := call.Function.Name
	item := dto.ResponsesOutput{
		Type: "function_call", ID: fmt.Sprintf("fc_%s_%d", responseID, index), Status: "completed",
		CallId: call.ID, Name: name, Arguments: argumentsRaw(call.Function.Arguments),
	}
	if meta == nil {
		return item
	}
	if namespace, ok := meta.NamespaceTools[name]; ok {
		item.Name = namespace.Name
		item.Namespace = namespace.Namespace
	}
	if _, ok := meta.CustomToolNames[name]; ok {
		item.Type = "custom_tool_call"
		item.Input = unwrapCustomToolArguments(call.Function.Arguments)
		item.Arguments = nil
	}
	if _, ok := meta.ToolSearchNames[name]; ok {
		item.Type = "tool_search_call"
	}
	return item
}

func chatUsageToResponsesUsage(usage dto.Usage) *dto.Usage {
	result := usage
	result.InputTokens = usage.PromptTokens
	result.OutputTokens = usage.CompletionTokens
	inputDetails := usage.PromptTokensDetails
	result.InputTokensDetails = &inputDetails
	if result.TotalTokens == 0 {
		result.TotalTokens = result.InputTokens + result.OutputTokens
	}
	return &result
}

func applyResponsesRequestEcho(resp *dto.OpenAIResponsesResponse, req *dto.OpenAIResponsesRequest) {
	if resp == nil || req == nil {
		return
	}
	resp.Instructions = req.Instructions
	if req.MaxOutputTokens != nil {
		resp.MaxOutputTokens = int(*req.MaxOutputTokens)
	}
	resp.MaxToolCalls = req.MaxToolCalls
	resp.ParallelToolCalls = rawBoolPointerValue(req.ParallelToolCalls)
	resp.PreviousResponseID, _ = platformencoding.Marshal(req.PreviousResponseID)
	resp.Reasoning = req.Reasoning
	resp.ServiceTier = req.ServiceTier
	resp.Store = rawBoolPointerValue(req.Store)
	if req.Temperature != nil {
		resp.Temperature = *req.Temperature
	}
	resp.Text = req.Text
	resp.ToolChoice = req.ToolChoice
	resp.Tools = req.GetToolsMap()
	if req.TopP != nil {
		resp.TopP = *req.TopP
	}
	resp.Truncation = req.Truncation
	resp.User = req.User
	resp.Metadata = req.Metadata
	resp.PromptCacheKey = req.PromptCacheKey
	resp.SafetyIdentifier = req.SafetyIdentifier
}

func chatMessageReasoning(message dto.Message) string {
	if message.ReasoningContent != nil {
		return *message.ReasoningContent
	}
	if message.Reasoning != nil {
		return *message.Reasoning
	}
	return ""
}

func unwrapCustomToolArguments(arguments string) string {
	var wrapped map[string]json.RawMessage
	if platformencoding.UnmarshalString(arguments, &wrapped) == nil {
		if input := rawJSONText(wrapped["input"]); input != "" {
			return input
		}
	}
	return arguments
}

func rawBoolPointerValue(raw json.RawMessage) bool {
	value := rawBoolPointer(raw)
	return value != nil && *value
}

func responsesIDFromChat(chatID string) string {
	chatID = strings.TrimSpace(chatID)
	if strings.HasPrefix(chatID, "resp_") {
		return chatID
	}
	chatID = strings.TrimPrefix(chatID, "chatcmpl-")
	chatID = strings.TrimPrefix(chatID, "chatcmpl_")
	if chatID == "" {
		chatID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return "resp_" + chatID
}

func chatCreatedAt(value any) int {
	switch created := value.(type) {
	case int:
		return created
	case int64:
		return int(created)
	case float64:
		return int(created)
	case json.Number:
		parsed, _ := created.Int64()
		return int(parsed)
	default:
		return int(time.Now().Unix())
	}
}
