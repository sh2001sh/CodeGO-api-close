package translation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sh2001sh/new-api/dto"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
)

func responsesContentToChat(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	if platformencoding.GetJSONType(raw) == "string" {
		return rawString(raw), nil
	}
	var parts []map[string]json.RawMessage
	if err := platformencoding.Unmarshal(raw, &parts); err != nil {
		return nil, errors.New("message content must be a string or an array")
	}
	converted := make([]dto.MediaContent, 0, len(parts))
	for _, part := range parts {
		partType := rawString(part["type"])
		switch partType {
		case "input_text", "output_text", "text":
			converted = append(converted, dto.MediaContent{Type: dto.ContentTypeText, Text: rawString(part["text"])})
		case "input_image", "image_url":
			imageURL := rawString(part["image_url"])
			if imageURL == "" {
				var image map[string]json.RawMessage
				_ = platformencoding.Unmarshal(part["image_url"], &image)
				imageURL = rawString(image["url"])
			}
			if imageURL == "" {
				return nil, errors.New("input_image requires image_url")
			}
			converted = append(converted, dto.MediaContent{
				Type: dto.ContentTypeImageURL,
				ImageUrl: map[string]any{
					"url": imageURL, "detail": rawString(part["detail"]),
				},
			})
		case "input_audio":
			var audio any
			if err := platformencoding.Unmarshal(part["input_audio"], &audio); err != nil {
				return nil, errors.New("invalid input_audio content")
			}
			converted = append(converted, dto.MediaContent{Type: dto.ContentTypeInputAudio, InputAudio: audio})
		case "input_file", "file":
			var file any
			if value := part["file"]; len(value) > 0 {
				_ = platformencoding.Unmarshal(value, &file)
			} else {
				file = map[string]any{
					"file_id": rawString(part["file_id"]), "file_data": rawString(part["file_data"]),
					"file_name": rawString(part["filename"]),
				}
			}
			converted = append(converted, dto.MediaContent{Type: dto.ContentTypeFile, File: file})
		default:
			return nil, fmt.Errorf("unsupported responses content type %q", partType)
		}
	}
	return converted, nil
}

func responsesToolsToChat(topLevel json.RawMessage, additional []json.RawMessage, meta *ResponsesChatBridgeMeta) ([]dto.ToolCallRequest, error) {
	var tools []json.RawMessage
	if len(topLevel) > 0 && string(topLevel) != "null" {
		if err := platformencoding.Unmarshal(topLevel, &tools); err != nil {
			return nil, errors.New("tools must be an array")
		}
	}
	tools = append(tools, additional...)
	converted := make([]dto.ToolCallRequest, 0, len(tools))
	for _, rawTool := range tools {
		items, err := responsesToolToChat(rawTool, "", meta)
		if err != nil {
			return nil, err
		}
		converted = append(converted, items...)
	}
	return converted, nil
}

func responsesToolToChat(rawTool json.RawMessage, namespace string, meta *ResponsesChatBridgeMeta) ([]dto.ToolCallRequest, error) {
	var tool map[string]json.RawMessage
	if err := platformencoding.Unmarshal(rawTool, &tool); err != nil {
		return nil, errors.New("tool must be an object")
	}
	toolType := rawString(tool["type"])
	if toolType == "" {
		toolType = "function"
	}
	if toolType == "namespace" {
		return responsesNamespaceToolsToChat(tool, meta)
	}
	name := responsesToolNameFromMap(tool)
	if name == "" {
		return nil, fmt.Errorf("%s tool requires a name", toolType)
	}
	qualifiedName := qualifyResponsesToolName(namespace, name)
	if namespace != "" {
		meta.NamespaceTools[qualifiedName] = ResponsesNamespaceTool{Name: name, Namespace: namespace}
	}

	switch toolType {
	case "function":
		return []dto.ToolCallRequest{responsesFunctionTool(tool, qualifiedName)}, nil
	case "custom":
		meta.CustomToolNames[qualifiedName] = struct{}{}
		return []dto.ToolCallRequest{{
			Type: "function",
			Function: dto.FunctionRequest{
				Name: qualifiedName, Description: responsesToolDescriptionFromMap(tool),
				Parameters: map[string]any{
					"type": "object", "properties": map[string]any{"input": map[string]any{"type": "string"}},
					"required": []string{"input"},
				},
			},
		}}, nil
	case "tool_search":
		meta.ToolSearchNames[qualifiedName] = struct{}{}
		return []dto.ToolCallRequest{{
			Type: "function",
			Function: dto.FunctionRequest{
				Name: qualifiedName, Description: responsesToolDescriptionFromMap(tool),
				Parameters: map[string]any{"type": "object", "additionalProperties": true},
			},
		}}, nil
	default:
		return nil, fmt.Errorf("responses tool type %q cannot be represented by chat completions", toolType)
	}
}

func responsesNamespaceToolsToChat(tool map[string]json.RawMessage, meta *ResponsesChatBridgeMeta) ([]dto.ToolCallRequest, error) {
	namespace := strings.TrimSpace(rawString(tool["name"]))
	if namespace == "" {
		return nil, errors.New("namespace tool requires a name")
	}
	var children []json.RawMessage
	if err := platformencoding.Unmarshal(tool["tools"], &children); err != nil {
		return nil, errors.New("namespace tool requires a tools array")
	}
	var converted []dto.ToolCallRequest
	for _, child := range children {
		items, err := responsesToolToChat(child, namespace, meta)
		if err != nil {
			return nil, err
		}
		converted = append(converted, items...)
	}
	return converted, nil
}

func responsesFunctionTool(tool map[string]json.RawMessage, name string) dto.ToolCallRequest {
	parameters := any(map[string]any{})
	for _, field := range []string{"parameters", "input_schema"} {
		if raw := tool[field]; len(raw) > 0 {
			_ = platformencoding.Unmarshal(raw, &parameters)
			break
		}
	}
	strict := rawBoolPointer(tool["strict"])
	return dto.ToolCallRequest{
		Type: "function",
		Function: dto.FunctionRequest{
			Name: name, Description: responsesToolDescriptionFromMap(tool), Parameters: parameters, Strict: strict,
		},
	}
}

func responsesToolChoiceToChat(raw json.RawMessage, meta *ResponsesChatBridgeMeta) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if platformencoding.GetJSONType(raw) == "string" {
		return rawString(raw), nil
	}
	var choice map[string]json.RawMessage
	if err := platformencoding.Unmarshal(raw, &choice); err != nil {
		return nil, errors.New("invalid tool_choice")
	}
	choiceType := rawString(choice["type"])
	name := rawString(choice["name"])
	if namespace := rawString(choice["namespace"]); namespace != "" {
		name = qualifyResponsesToolName(namespace, name)
	}
	if choiceType == "function" || choiceType == "custom" || choiceType == "tool_search" {
		if name == "" {
			return nil, errors.New("tool_choice requires a name")
		}
		if choiceType == "custom" {
			meta.CustomToolNames[name] = struct{}{}
		}
		if choiceType == "tool_search" {
			meta.ToolSearchNames[name] = struct{}{}
		}
		return map[string]any{"type": "function", "function": map[string]string{"name": name}}, nil
	}
	return nil, fmt.Errorf("unsupported tool_choice type %q", choiceType)
}

func responsesTextFormatToChat(raw json.RawMessage) (*dto.ResponseFormat, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var text map[string]json.RawMessage
	if err := platformencoding.Unmarshal(raw, &text); err != nil {
		return nil, errors.New("text must be an object")
	}
	var format map[string]json.RawMessage
	if err := platformencoding.Unmarshal(text["format"], &format); err != nil || len(format) == 0 {
		return nil, nil
	}
	formatType := rawString(format["type"])
	result := &dto.ResponseFormat{Type: formatType}
	if formatType == "json_schema" {
		schema, err := platformencoding.Marshal(format)
		if err != nil {
			return nil, err
		}
		result.JsonSchema = schema
	}
	return result, nil
}

func responsesReasoningText(item map[string]json.RawMessage) string {
	var summary []map[string]json.RawMessage
	_ = platformencoding.Unmarshal(item["summary"], &summary)
	var parts []string
	for _, part := range summary {
		if text := rawString(part["text"]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "")
}

func responsesToolOutputText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if platformencoding.GetJSONType(raw) == "string" {
		return rawString(raw)
	}
	var parts []map[string]json.RawMessage
	if platformencoding.Unmarshal(raw, &parts) == nil {
		var text strings.Builder
		for _, part := range parts {
			text.WriteString(rawString(part["text"]))
		}
		if text.Len() > 0 {
			return text.String()
		}
	}
	return string(raw)
}

func responsesInstructionText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if platformencoding.GetJSONType(raw) == "string" {
		return rawString(raw)
	}
	content, err := responsesContentToChat(raw)
	if err != nil {
		return ""
	}
	parts, ok := content.([]dto.MediaContent)
	if !ok {
		return fmt.Sprintf("%v", content)
	}
	var text []string
	for _, part := range parts {
		if part.Type == dto.ContentTypeText && part.Text != "" {
			text = append(text, part.Text)
		}
	}
	return strings.Join(text, "\n")
}

func responsesToolNameFromMap(tool map[string]json.RawMessage) string {
	if name := rawString(tool["name"]); name != "" {
		return name
	}
	var function map[string]json.RawMessage
	_ = platformencoding.Unmarshal(tool["function"], &function)
	return rawString(function["name"])
}

func responsesToolDescriptionFromMap(tool map[string]json.RawMessage) string {
	if description := rawString(tool["description"]); description != "" {
		return description
	}
	var function map[string]json.RawMessage
	_ = platformencoding.Unmarshal(tool["function"], &function)
	return rawString(function["description"])
}

func qualifyResponsesToolName(namespace, name string) string {
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if namespace == "" || name == "" || strings.HasPrefix(name, "mcp__") {
		return name
	}
	return strings.TrimSuffix(namespace, "__") + "__" + name
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if platformencoding.Unmarshal(raw, &value) == nil {
		return value
	}
	return ""
}

func rawJSONText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if platformencoding.GetJSONType(raw) == "string" {
		return rawString(raw)
	}
	return string(raw)
}

func rawBoolPointer(raw json.RawMessage) *bool {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var value bool
	if platformencoding.Unmarshal(raw, &value) != nil {
		return nil
	}
	return &value
}

func rawStringMessage(value string) json.RawMessage {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	raw, _ := platformencoding.Marshal(value)
	return raw
}
