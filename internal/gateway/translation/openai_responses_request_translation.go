package translation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/sh2001sh/new-api/dto"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
)

func normalizeChatImageURLToString(v any) any {
	switch vv := v.(type) {
	case string:
		return vv
	case map[string]any:
		if url := platformencoding.Interface2String(vv["url"]); url != "" {
			return url
		}
		return v
	case dto.MessageImageUrl:
		if vv.Url != "" {
			return vv.Url
		}
		return v
	case *dto.MessageImageUrl:
		if vv != nil && vv.Url != "" {
			return vv.Url
		}
		return v
	default:
		return v
	}
}

func convertChatResponseFormatToResponsesText(reqFormat *dto.ResponseFormat) json.RawMessage {
	if reqFormat == nil || strings.TrimSpace(reqFormat.Type) == "" {
		return nil
	}

	format := map[string]any{"type": reqFormat.Type}
	if reqFormat.Type == "json_schema" && len(reqFormat.JsonSchema) > 0 {
		var chatSchema map[string]any
		if err := platformencoding.Unmarshal(reqFormat.JsonSchema, &chatSchema); err == nil {
			for key, value := range chatSchema {
				if key != "type" {
					format[key] = value
				}
			}
			if nested, ok := format["json_schema"].(map[string]any); ok {
				for key, value := range nested {
					if _, exists := format[key]; !exists {
						format[key] = value
					}
				}
				delete(format, "json_schema")
			}
		} else {
			format["json_schema"] = reqFormat.JsonSchema
		}
	}

	textRaw, _ := platformencoding.Marshal(map[string]any{"format": format})
	return textRaw
}

func ChatCompletionsRequestToResponsesRequest(req *dto.GeneralOpenAIRequest) (*dto.OpenAIResponsesRequest, error) {
	if req == nil {
		return nil, errors.New("request is nil")
	}
	if req.Model == "" {
		return nil, errors.New("model is required")
	}
	if lo.FromPtrOr(req.N, 1) > 1 {
		return nil, fmt.Errorf("n>1 is not supported in responses translation mode")
	}
	if err := validateChatToolCallHistory(req.Messages); err != nil {
		return nil, err
	}

	var instructionsParts []string
	inputItems := make([]map[string]any, 0, len(req.Messages))
	for _, msg := range req.Messages {
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			continue
		}
		if role == "tool" || role == "function" {
			inputItems = append(inputItems, map[string]any{
				"type":    "function_call_output",
				"call_id": strings.TrimSpace(msg.ToolCallId),
				"output":  toolOutputContent(msg),
			})
			continue
		}
		if role == "system" || role == "developer" {
			appendResponsesInstructions(&instructionsParts, msg)
			continue
		}

		inputItems = append(inputItems, chatMessageToResponsesItem(msg, role))
		inputItems = appendAssistantToolCalls(inputItems, role, msg.ParseToolCalls())
	}

	inputRaw, err := platformencoding.Marshal(inputItems)
	if err != nil {
		return nil, err
	}
	return buildResponsesRequest(req, inputRaw, instructionsParts), nil
}

func toolOutputContent(msg dto.Message) any {
	if msg.Content == nil {
		return ""
	}
	if msg.IsStringContent() {
		return msg.StringContent()
	}
	if body, err := platformencoding.Marshal(msg.Content); err == nil {
		return string(body)
	}
	return fmt.Sprintf("%v", msg.Content)
}

func appendResponsesInstructions(instructions *[]string, msg dto.Message) {
	if msg.Content == nil {
		return
	}
	if msg.IsStringContent() {
		if text := strings.TrimSpace(msg.StringContent()); text != "" {
			*instructions = append(*instructions, text)
		}
		return
	}

	parts := msg.ParseContent()
	var text strings.Builder
	for _, part := range parts {
		if part.Type != dto.ContentTypeText || strings.TrimSpace(part.Text) == "" {
			continue
		}
		if text.Len() > 0 {
			text.WriteString("\n")
		}
		text.WriteString(part.Text)
	}
	if value := strings.TrimSpace(text.String()); value != "" {
		*instructions = append(*instructions, value)
	}
}

func chatMessageToResponsesItem(msg dto.Message, role string) map[string]any {
	item := map[string]any{"role": role}
	if msg.Content == nil {
		item["content"] = ""
		return item
	}
	if msg.IsStringContent() {
		item["content"] = msg.StringContent()
		return item
	}

	parts := msg.ParseContent()
	content := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		content = append(content, chatContentPartToResponses(part, role))
	}
	item["content"] = content
	return item
}

func chatContentPartToResponses(part dto.MediaContent, role string) map[string]any {
	switch part.Type {
	case dto.ContentTypeText:
		textType := "input_text"
		if role == "assistant" {
			textType = "output_text"
		}
		return map[string]any{"type": textType, "text": part.Text}
	case dto.ContentTypeImageURL:
		return map[string]any{"type": "input_image", "image_url": normalizeChatImageURLToString(part.ImageUrl)}
	case dto.ContentTypeInputAudio:
		return map[string]any{"type": "input_audio", "input_audio": part.InputAudio}
	case dto.ContentTypeFile:
		return map[string]any{"type": "input_file", "file": part.File}
	case dto.ContentTypeVideoUrl:
		return map[string]any{"type": "input_video", "video_url": part.VideoUrl}
	default:
		return map[string]any{"type": part.Type}
	}
}

func buildResponsesRequest(req *dto.GeneralOpenAIRequest, input json.RawMessage, instructions []string) *dto.OpenAIResponsesRequest {
	var instructionsRaw json.RawMessage
	if len(instructions) > 0 {
		instructionsRaw, _ = platformencoding.Marshal(strings.Join(instructions, "\n\n"))
	}

	maxOutputTokens := lo.FromPtrOr(req.MaxTokens, uint(0))
	if maxCompletionTokens := lo.FromPtrOr(req.MaxCompletionTokens, uint(0)); maxCompletionTokens > maxOutputTokens {
		maxOutputTokens = maxCompletionTokens
	}

	request := &dto.OpenAIResponsesRequest{
		Model:             req.Model,
		Input:             input,
		Instructions:      instructionsRaw,
		Stream:            req.Stream,
		Temperature:       req.Temperature,
		FrequencyPenalty:  req.FrequencyPenalty,
		PresencePenalty:   req.PresencePenalty,
		Text:              convertChatResponseFormatToResponsesText(req.ResponseFormat),
		ToolChoice:        convertChatToolChoice(req.ToolChoice),
		Tools:             convertChatTools(req.Tools),
		TopP:              copyTopP(req.TopP),
		User:              req.User,
		ParallelToolCalls: convertParallelToolCalls(req.ParallelTooCalls),
		Store:             req.Store,
		Metadata:          req.Metadata,
	}
	if req.MaxTokens != nil || req.MaxCompletionTokens != nil {
		request.MaxOutputTokens = lo.ToPtr(maxOutputTokens)
	}
	if req.ReasoningEffort != "" {
		request.Reasoning = &dto.Reasoning{Effort: req.ReasoningEffort, Summary: "detailed"}
	}
	return request
}

func convertChatTools(tools []dto.ToolCallRequest) json.RawMessage {
	if tools == nil {
		return nil
	}
	converted := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if tool.Type == "function" {
			converted = append(converted, map[string]any{
				"type": "function", "name": tool.Function.Name,
				"description": tool.Function.Description, "parameters": tool.Function.Parameters,
			})
			continue
		}
		var item map[string]any
		if body, err := platformencoding.Marshal(tool); err == nil {
			_ = platformencoding.Unmarshal(body, &item)
		}
		if len(item) == 0 {
			item = map[string]any{"type": tool.Type}
		}
		converted = append(converted, item)
	}
	result, _ := platformencoding.Marshal(converted)
	return result
}

func convertChatToolChoice(choice any) json.RawMessage {
	if choice == nil {
		return nil
	}
	if value, ok := choice.(string); ok {
		result, _ := platformencoding.Marshal(value)
		return result
	}

	var item map[string]any
	if body, err := platformencoding.Marshal(choice); err == nil {
		_ = platformencoding.Unmarshal(body, &item)
	}
	if item == nil {
		result, _ := platformencoding.Marshal(choice)
		return result
	}
	if toolType, _ := item["type"].(string); toolType == "function" {
		if name, _ := item["name"].(string); name != "" {
			result, _ := platformencoding.Marshal(map[string]any{"type": "function", "name": name})
			return result
		}
		if function, _ := item["function"].(map[string]any); function != nil {
			if name, _ := function["name"].(string); name != "" {
				result, _ := platformencoding.Marshal(map[string]any{"type": "function", "name": name})
				return result
			}
		}
	}
	result, _ := platformencoding.Marshal(choice)
	return result
}

func convertParallelToolCalls(value *bool) json.RawMessage {
	if value == nil {
		return nil
	}
	result, _ := platformencoding.Marshal(*value)
	return result
}

func copyTopP(value *float64) *float64 {
	if value == nil {
		return nil
	}
	return platformruntime.GetPointer(lo.FromPtr(value))
}

func appendAssistantToolCalls(inputItems []map[string]any, role string, toolCalls []dto.ToolCallRequest) []map[string]any {
	if role != "assistant" {
		return inputItems
	}
	for _, toolCall := range toolCalls {
		if strings.TrimSpace(toolCall.ID) == "" || (toolCall.Type != "" && toolCall.Type != "function") {
			continue
		}
		name := strings.TrimSpace(toolCall.Function.Name)
		if name == "" {
			continue
		}
		inputItems = append(inputItems, map[string]any{
			"type": "function_call", "call_id": toolCall.ID,
			"name": name, "arguments": toolCall.Function.Arguments,
		})
	}
	return inputItems
}
