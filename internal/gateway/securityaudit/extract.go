package securityaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var errNoPromptText = errors.New("prompt audit request contains no text")

type segment struct {
	text       string
	role       string
	persistent bool
}

func ExtractSnapshot(req Request, latestTurnOnly bool) (Snapshot, error) {
	var root any
	if len(req.Body) > 0 && json.Unmarshal(req.Body, &root) == nil {
		segments := extractProtocolSegments(strings.ToLower(req.Protocol), root)
		segments = normalizeSegments(segments)
		if latestTurnOnly {
			segments = narrowLatestTurn(segments)
		}
		if snapshot, ok := snapshotFromSegments(req, segments); ok {
			return snapshot, nil
		}
	}
	fallback := normalizeText(req.FallbackText)
	if fallback == "" {
		return Snapshot{}, errNoPromptText
	}
	return snapshotFromText(req, fallback), nil
}

func extractProtocolSegments(protocol string, root any) []segment {
	object, _ := root.(map[string]any)
	if object == nil {
		return nil
	}
	switch {
	case strings.Contains(protocol, "chat"):
		result := extractMessages(object["messages"])
		return append(result, extractToolDefinitions(object)...)
	case strings.Contains(protocol, "responses"):
		result := persistentSegments(extractValueText(object["instructions"], "system"))
		result = append(result, extractResponsesInput(object["input"])...)
		result = append(result, extractToolDefinitions(object)...)
		return result
	case strings.Contains(protocol, "claude") || strings.Contains(protocol, "anthropic"):
		result := persistentSegments(extractValueText(object["system"], "system"))
		result = append(result, extractMessages(object["messages"])...)
		return append(result, extractToolDefinitions(object)...)
	case strings.Contains(protocol, "gemini"):
		result := persistentSegments(extractValueText(object["systemInstruction"], "system"))
		if len(result) == 0 {
			result = persistentSegments(extractValueText(object["system_instruction"], "system"))
		}
		result = append(result, extractValueText(object["contents"], "user")...)
		return append(result, extractToolDefinitions(object)...)
	case strings.Contains(protocol, "realtime"):
		return extractRealtimeSegments(object)
	case strings.Contains(protocol, "image") || strings.Contains(protocol, "media"):
		return extractKnownFields(object, "user")
	default:
		result := extractMessages(object["messages"])
		result = append(result, persistentSegments(extractValueText(object["instructions"], "system"))...)
		result = append(result, extractToolDefinitions(object)...)
		return append(result, extractKnownFields(object, "user")...)
	}
}

func extractRealtimeSegments(object map[string]any) []segment {
	if object == nil {
		return nil
	}
	result := make([]segment, 0, 4)
	result = append(result, persistentSegments(extractValueText(object["session"], "system"))...)
	for _, key := range []string{"item", "response"} {
		result = append(result, extractValueText(object[key], "user")...)
	}
	return append(result, extractKnownFields(object, "user")...)
}

func extractToolDefinitions(object map[string]any) []segment {
	result := extractValueText(object["tools"], "tool")
	result = append(result, extractValueText(object["functions"], "tool")...)
	return persistentSegments(result)
}

func extractMessages(value any) []segment {
	items, _ := value.([]any)
	result := make([]segment, 0, len(items))
	for _, item := range items {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(stringValue(message["role"]))
		if !isClientRole(role) {
			continue
		}
		segments := extractValueText(message["content"], role)
		if role == "system" || role == "developer" {
			segments = persistentSegments(segments)
		}
		result = append(result, segments...)
	}
	return result
}

func extractResponsesInput(value any) []segment {
	items, ok := value.([]any)
	if !ok {
		return extractValueText(value, "user")
	}
	result := make([]segment, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(stringValue(object["role"]))
		if role == "" {
			role = "user"
		}
		result = append(result, extractValueText(object["content"], role)...)
		result = append(result, extractValueText(object["text"], role)...)
		result = append(result, extractValueText(object["output_text"], role)...)
	}
	return result
}

func extractKnownFields(object map[string]any, role string) []segment {
	result := make([]segment, 0, 4)
	for _, key := range []string{"prompt", "input", "query", "description", "negative_prompt", "positive_prompt", "tools"} {
		result = append(result, extractValueText(object[key], role)...)
	}
	return result
}

func extractValueText(value any, role string) []segment {
	switch typed := value.(type) {
	case string:
		return []segment{{text: typed, role: role}}
	case []any:
		result := make([]segment, 0, len(typed))
		for _, item := range typed {
			result = append(result, extractValueText(item, role)...)
		}
		return result
	case map[string]any:
		result := make([]segment, 0, 2)
		for _, key := range []string{
			"text", "content", "parts", "input_text", "output_text", "description", "prompt", "query",
			"name", "instructions", "transcript", "tools", "parameters", "function", "functionDeclarations",
			"function_declarations",
		} {
			if nested, exists := typed[key]; exists {
				result = append(result, extractValueText(nested, role)...)
			}
		}
		return result
	default:
		return nil
	}
}

func normalizeSegments(values []segment) []segment {
	result := make([]segment, 0, len(values))
	for _, value := range values {
		value.text = normalizeText(value.text)
		if value.text != "" {
			result = append(result, value)
		}
	}
	return result
}

func narrowLatestTurn(values []segment) []segment {
	latest := -1
	for index := len(values) - 1; index >= 0; index-- {
		if values[index].role == "user" {
			latest = index
			break
		}
	}
	if latest < 0 {
		return values
	}
	result := make([]segment, 0, len(values))
	for _, value := range values {
		if value.persistent {
			result = append(result, value)
		}
	}
	for index := latest - 1; index >= 0; index-- {
		if values[index].role == "assistant" || values[index].role == "model" {
			if !values[index].persistent {
				result = append(result, values[index])
			}
			break
		}
	}
	for index := latest; index < len(values); index++ {
		if !values[index].persistent {
			result = append(result, values[index])
		}
	}
	return result
}

func persistentSegments(values []segment) []segment {
	for index := range values {
		values[index].persistent = true
	}
	return values
}

func snapshotFromSegments(req Request, values []segment) (Snapshot, bool) {
	if len(values) == 0 {
		return Snapshot{}, false
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, value.text)
	}
	return snapshotFromTextWithCount(req, strings.Join(parts, "\n\n"), len(values)), true
}

func snapshotFromText(req Request, text string) Snapshot {
	return snapshotFromTextWithCount(req, text, 1)
}

func snapshotFromTextWithCount(req Request, text string, count int) Snapshot {
	digest := sha256.Sum256([]byte(text))
	stage := strings.TrimSpace(req.Stage)
	if stage == "" {
		stage = "http"
	}
	return Snapshot{
		RequestID: req.RequestID, Group: req.Group, Protocol: req.Protocol, Model: req.Model,
		Stage: stage, PromptHash: hex.EncodeToString(digest[:]), PromptLength: utf8.RuneCountInString(text),
		MessageCount: count, RedactedPreview: promptPreview(text), ScanText: text,
	}
}

func normalizeText(value string) string {
	value = norm.NFKC.String(value)
	var builder strings.Builder
	for _, r := range value {
		if unicode.Is(unicode.Cf, r) || r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\ufeff' {
			continue
		}
		builder.WriteRune(r)
	}
	return strings.TrimSpace(builder.String())
}

func isClientRole(role string) bool {
	switch role {
	case "user", "system", "developer", "assistant", "tool", "model":
		return true
	default:
		return false
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}
