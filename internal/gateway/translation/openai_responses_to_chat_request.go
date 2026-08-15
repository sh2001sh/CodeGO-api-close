package translation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sh2001sh/new-api/dto"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
)

type ResponsesNamespaceTool struct {
	Name      string
	Namespace string
}

type ResponsesChatBridgeMeta struct {
	CustomToolNames map[string]struct{}
	ToolSearchNames map[string]struct{}
	NamespaceTools  map[string]ResponsesNamespaceTool
}

func NewResponsesChatBridgeMeta() *ResponsesChatBridgeMeta {
	return &ResponsesChatBridgeMeta{
		CustomToolNames: make(map[string]struct{}),
		ToolSearchNames: make(map[string]struct{}),
		NamespaceTools:  make(map[string]ResponsesNamespaceTool),
	}
}

// ResponsesRequestToChatCompletionsRequest converts the stateless subset of
// the Responses API into a Chat Completions request without silently dropping
// unsupported conversation state.
func ResponsesRequestToChatCompletionsRequest(req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, *ResponsesChatBridgeMeta, error) {
	if req == nil {
		return nil, nil, errors.New("request is nil")
	}
	if strings.TrimSpace(req.Model) == "" {
		return nil, nil, errors.New("model is required")
	}
	if strings.TrimSpace(req.PreviousResponseID) != "" {
		return nil, nil, errors.New("previous_response_id cannot be represented by a stateless chat completions upstream")
	}

	meta := NewResponsesChatBridgeMeta()
	messages, additionalTools, err := responsesInputToChatMessages(req.Input, meta)
	if err != nil {
		return nil, nil, err
	}
	if instruction := responsesInstructionText(req.Instructions); instruction != "" {
		messages = append([]dto.Message{{Role: "system", Content: instruction}}, messages...)
	}

	tools, err := responsesToolsToChat(req.Tools, additionalTools, meta)
	if err != nil {
		return nil, nil, err
	}
	toolChoice, err := responsesToolChoiceToChat(req.ToolChoice, meta)
	if err != nil {
		return nil, nil, err
	}
	responseFormat, err := responsesTextFormatToChat(req.Text)
	if err != nil {
		return nil, nil, err
	}

	chat := buildResponsesBridgeChatRequest(req, messages, tools, toolChoice, responseFormat)
	return chat, meta, nil
}

func buildResponsesBridgeChatRequest(
	req *dto.OpenAIResponsesRequest,
	messages []dto.Message,
	tools []dto.ToolCallRequest,
	toolChoice any,
	responseFormat *dto.ResponseFormat,
) *dto.GeneralOpenAIRequest {
	chat := &dto.GeneralOpenAIRequest{
		Model:            req.Model,
		Messages:         messages,
		Stream:           req.Stream,
		Temperature:      req.Temperature,
		TopP:             req.TopP,
		FrequencyPenalty: req.FrequencyPenalty,
		PresencePenalty:  req.PresencePenalty,
		ResponseFormat:   responseFormat,
		Tools:            tools,
		ToolChoice:       toolChoice,
		User:             req.User,
		Store:            req.Store,
		Metadata:         req.Metadata,
		ServiceTier:      rawStringMessage(req.ServiceTier),
		PromptCacheKey:   rawString(req.PromptCacheKey),
		ParallelTooCalls: rawBoolPointer(req.ParallelToolCalls),
		TopLogProbs:      req.TopLogProbs,
	}
	if req.MaxOutputTokens != nil {
		chat.MaxTokens = req.MaxOutputTokens
	}
	if req.Reasoning != nil {
		chat.ReasoningEffort = req.Reasoning.Effort
	}
	if req.Stream != nil && *req.Stream {
		chat.StreamOptions = &dto.StreamOptions{IncludeUsage: true}
	}
	if req.TopLogProbs != nil && *req.TopLogProbs > 0 {
		chat.LogProbs = boolPointer(true)
	}
	return chat
}

func responsesInputToChatMessages(raw json.RawMessage, meta *ResponsesChatBridgeMeta) ([]dto.Message, []json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil, nil
	}
	if platformencoding.GetJSONType(raw) == "string" {
		var text string
		if err := platformencoding.Unmarshal(raw, &text); err != nil {
			return nil, nil, err
		}
		return []dto.Message{{Role: "user", Content: text}}, nil, nil
	}

	var items []json.RawMessage
	if err := platformencoding.Unmarshal(raw, &items); err != nil {
		return nil, nil, errors.New("responses input must be a string or an array")
	}
	builder := newResponsesChatMessageBuilder(meta)
	var additionalTools []json.RawMessage
	for index, item := range items {
		var envelope map[string]json.RawMessage
		if err := platformencoding.Unmarshal(item, &envelope); err != nil {
			return nil, nil, fmt.Errorf("input[%d] must be an object: %w", index, err)
		}
		itemType := rawString(envelope["type"])
		switch itemType {
		case "", "message":
			if err := builder.addMessage(envelope); err != nil {
				return nil, nil, fmt.Errorf("input[%d]: %w", index, err)
			}
		case "reasoning":
			builder.addReasoning(responsesReasoningText(envelope))
		case "function_call", "custom_tool_call", "tool_search_call":
			if err := builder.addToolCall(itemType, envelope); err != nil {
				return nil, nil, fmt.Errorf("input[%d]: %w", index, err)
			}
		case "function_call_output", "custom_tool_call_output", "tool_search_output":
			if err := builder.addToolOutput(envelope); err != nil {
				return nil, nil, fmt.Errorf("input[%d]: %w", index, err)
			}
		case "additional_tools":
			var tools []json.RawMessage
			if err := platformencoding.Unmarshal(envelope["tools"], &tools); err != nil {
				return nil, nil, fmt.Errorf("input[%d].tools must be an array", index)
			}
			additionalTools = append(additionalTools, tools...)
		default:
			return nil, nil, fmt.Errorf("unsupported responses input item type %q", itemType)
		}
	}
	builder.flushPendingAssistant()
	return builder.messages, additionalTools, nil
}

type responsesChatMessageBuilder struct {
	messages         []dto.Message
	pendingTools     []dto.ToolCallRequest
	pendingReasoning []string
	meta             *ResponsesChatBridgeMeta
}

func newResponsesChatMessageBuilder(meta *ResponsesChatBridgeMeta) *responsesChatMessageBuilder {
	return &responsesChatMessageBuilder{meta: meta}
}

func (b *responsesChatMessageBuilder) addReasoning(text string) {
	if strings.TrimSpace(text) != "" {
		b.pendingReasoning = append(b.pendingReasoning, text)
	}
}

func (b *responsesChatMessageBuilder) addMessage(item map[string]json.RawMessage) error {
	role := strings.TrimSpace(rawString(item["role"]))
	if role == "" {
		return errors.New("message role is required")
	}
	content, err := responsesContentToChat(item["content"])
	if err != nil {
		return err
	}
	if role != "assistant" {
		b.flushPendingAssistant()
	}
	message := dto.Message{Role: role, Content: content}
	if role == "assistant" && len(b.pendingReasoning) > 0 {
		reasoning := strings.Join(b.pendingReasoning, "")
		message.ReasoningContent = &reasoning
		b.pendingReasoning = nil
	}
	b.messages = append(b.messages, message)
	return nil
}

func (b *responsesChatMessageBuilder) addToolCall(itemType string, item map[string]json.RawMessage) error {
	callID := strings.TrimSpace(rawString(item["call_id"]))
	if callID == "" {
		callID = strings.TrimSpace(rawString(item["id"]))
	}
	name := strings.TrimSpace(rawString(item["name"]))
	if callID == "" || name == "" {
		return errors.New("tool call requires call_id and name")
	}
	arguments := rawJSONText(item["arguments"])
	if itemType == "custom_tool_call" {
		input := rawJSONText(item["input"])
		wrapped, _ := platformencoding.Marshal(map[string]string{"input": input})
		arguments = string(wrapped)
		b.meta.CustomToolNames[name] = struct{}{}
	}
	if itemType == "tool_search_call" {
		if arguments == "" {
			arguments = "{}"
		}
		b.meta.ToolSearchNames[name] = struct{}{}
	}
	if namespace := strings.TrimSpace(rawString(item["namespace"])); namespace != "" {
		qualified := qualifyResponsesToolName(namespace, name)
		b.meta.NamespaceTools[qualified] = ResponsesNamespaceTool{Name: name, Namespace: namespace}
		name = qualified
	}
	b.pendingTools = append(b.pendingTools, dto.ToolCallRequest{
		ID: callID, Type: "function",
		Function: dto.FunctionRequest{Name: name, Arguments: arguments},
	})
	return nil
}

func (b *responsesChatMessageBuilder) addToolOutput(item map[string]json.RawMessage) error {
	b.flushPendingAssistant()
	callID := strings.TrimSpace(rawString(item["call_id"]))
	if callID == "" {
		return errors.New("tool output requires call_id")
	}
	b.messages = append(b.messages, dto.Message{
		Role: "tool", ToolCallId: callID, Content: responsesToolOutputText(item["output"]),
	})
	return nil
}

func (b *responsesChatMessageBuilder) flushPendingAssistant() {
	if len(b.pendingTools) == 0 && len(b.pendingReasoning) == 0 {
		return
	}
	message := dto.Message{Role: "assistant", Content: ""}
	if len(b.pendingTools) > 0 {
		message.ToolCalls, _ = platformencoding.Marshal(b.pendingTools)
	}
	if len(b.pendingReasoning) > 0 {
		reasoning := strings.Join(b.pendingReasoning, "")
		message.ReasoningContent = &reasoning
	}
	b.messages = append(b.messages, message)
	b.pendingTools = nil
	b.pendingReasoning = nil
}

func boolPointer(value bool) *bool { return &value }
