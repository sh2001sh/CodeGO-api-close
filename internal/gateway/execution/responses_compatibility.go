package execution

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"github.com/sh2001sh/new-api/types"
)

var rejectedResponsesFieldPattern = regexp.MustCompile(`(?i)(?:unknown|unsupported)\s+(?:parameter|field)\s*(?::|=|is)?\s*["']?([a-zA-Z0-9_.\[\]-]+)`)

func shouldNormalizeResponsesCompatibilityBody(body []byte) bool {
	return bytes.Contains(body, []byte(`"transformer_metadata"`)) ||
		bytes.Contains(body, []byte(`"include"`)) ||
		bytes.Contains(body, []byte(`"namespace"`)) ||
		bytes.Contains(body, []byte(`"function_call_output"`)) ||
		bytes.Contains(body, []byte(`"custom_tool_call_output"`)) ||
		bytes.Contains(body, []byte(`"tool_search_output"`))
}

// normalizeResponsesCompatibilityBody repairs deterministic compatibility
// issues without changing message content or valid provider-specific fields.
func normalizeResponsesCompatibilityBody(body []byte) ([]byte, bool, error) {
	var payload map[string]json.RawMessage
	if err := platformencoding.Unmarshal(body, &payload); err != nil {
		return nil, false, err
	}
	changed := false
	if _, ok := payload["transformer_metadata"]; ok {
		delete(payload, "transformer_metadata")
		changed = true
	}
	if raw, ok := payload["include"]; ok {
		normalized, includeChanged := normalizeResponsesInclude(raw)
		if includeChanged {
			changed = true
			if len(normalized) == 0 {
				delete(payload, "include")
			} else {
				payload["include"] = normalized
			}
		}
	}
	if raw, ok := payload["input"]; ok {
		normalized, inputChanged, err := normalizeResponsesInput(raw, hasNonEmptyRawString(payload["previous_response_id"]))
		if err != nil {
			return nil, false, err
		}
		if inputChanged {
			payload["input"] = normalized
			changed = true
		}
	}
	if !changed {
		return body, false, nil
	}
	normalized, err := platformencoding.Marshal(payload)
	return normalized, true, err
}

// normalizeResponsesBackgroundFalse removes an explicit false Background
// flag. Omitting the field is the only portable representation for providers
// that do not implement the parameter at all.
func normalizeResponsesBackgroundFalse(body []byte) ([]byte, bool, error) {
	if !bytes.Contains(body, []byte(`"background"`)) {
		return body, false, nil
	}
	var payload map[string]json.RawMessage
	if err := platformencoding.Unmarshal(body, &payload); err != nil {
		return nil, false, err
	}
	raw, ok := payload["background"]
	if !ok {
		return body, false, nil
	}
	var background bool
	if err := platformencoding.Unmarshal(raw, &background); err != nil || background {
		return body, false, nil
	}
	delete(payload, "background")
	normalized, err := platformencoding.Marshal(payload)
	if err != nil {
		return nil, false, err
	}
	return normalized, true, nil
}

func normalizeResponsesInclude(raw json.RawMessage) (json.RawMessage, bool) {
	var values []string
	if platformencoding.Unmarshal(raw, &values) != nil {
		return raw, false
	}
	filtered := make([]string, 0, len(values))
	changed := false
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), "usage") {
			changed = true
			continue
		}
		filtered = append(filtered, value)
	}
	if !changed {
		return raw, false
	}
	if len(filtered) == 0 {
		return nil, true
	}
	normalized, err := platformencoding.Marshal(filtered)
	if err != nil {
		return raw, false
	}
	return normalized, true
}

func normalizeResponsesInput(raw json.RawMessage, hasPreviousResponse bool) (json.RawMessage, bool, error) {
	var items []map[string]json.RawMessage
	if err := platformencoding.Unmarshal(raw, &items); err != nil {
		return raw, false, nil
	}
	callIDs := make(map[string]struct{})
	changed := false
	for _, item := range items {
		itemType := rawString(item["type"])
		if isResponsesFunctionCall(itemType) {
			if _, ok := item["arguments"]; !ok && itemType == "function_call" {
				item["arguments"] = json.RawMessage(`"{}"`)
				changed = true
			}
			if _, ok := item["namespace"]; ok {
				delete(item, "namespace")
				changed = true
			}
			if callID := rawString(item["call_id"]); callID != "" {
				callIDs[callID] = struct{}{}
			}
		}
	}
	if !hasPreviousResponse {
		filtered := items[:0]
		for _, item := range items {
			if isResponsesFunctionOutput(rawString(item["type"])) {
				callID := rawString(item["call_id"])
				if _, ok := callIDs[callID]; callID == "" || !ok {
					changed = true
					continue
				}
			}
			filtered = append(filtered, item)
		}
		items = filtered
	}
	if !changed {
		return raw, false, nil
	}
	normalized, err := platformencoding.Marshal(items)
	return normalized, true, err
}

func normalizeRejectedResponsesField(body []byte, apiErr *types.NewAPIError) ([]byte, string, bool) {
	if apiErr == nil || apiErr.StatusCode != 400 {
		return nil, "", false
	}
	openAIError, ok := apiErr.RelayError.(types.OpenAIError)
	if !ok {
		return nil, "", false
	}
	code := strings.ToLower(fmt.Sprint(openAIError.Code))
	if code != "unknown_parameter" && code != "unsupported_parameter" && code != "invalid_request_error" {
		return nil, "", false
	}
	field := strings.TrimSpace(openAIError.Param)
	if field == "" {
		matches := rejectedResponsesFieldPattern.FindStringSubmatch(openAIError.Message)
		if len(matches) == 2 {
			field = matches[1]
		}
	}
	if code == "invalid_request_error" && !strings.Contains(strings.ToLower(openAIError.Message), "unsupported parameter") && !strings.Contains(strings.ToLower(openAIError.Message), "unknown parameter") {
		return nil, "", false
	}
	if !isAllowedResponsesRejectedField(field) {
		return nil, "", false
	}
	normalized, changed := removeResponsesRejectedField(body, field)
	return normalized, field, changed
}

func removeResponsesRejectedField(body []byte, field string) ([]byte, bool) {
	var payload map[string]json.RawMessage
	if platformencoding.Unmarshal(body, &payload) != nil {
		return nil, false
	}
	if field == "max_output_tokens" || field == "transformer_metadata" {
		if _, ok := payload[field]; !ok {
			return nil, false
		}
		delete(payload, field)
		normalized, err := platformencoding.Marshal(payload)
		return normalized, err == nil
	}
	matches := regexp.MustCompile(`^input\[(\d+)\]\.namespace$`).FindStringSubmatch(field)
	if len(matches) != 2 {
		return nil, false
	}
	index, err := strconv.Atoi(matches[1])
	if err != nil {
		return nil, false
	}
	var items []map[string]json.RawMessage
	if platformencoding.Unmarshal(payload["input"], &items) != nil || index < 0 || index >= len(items) {
		return nil, false
	}
	if _, ok := items[index]["namespace"]; !ok {
		return nil, false
	}
	delete(items[index], "namespace")
	payload["input"], err = platformencoding.Marshal(items)
	if err != nil {
		return nil, false
	}
	normalized, err := platformencoding.Marshal(payload)
	return normalized, err == nil
}

func isAllowedResponsesRejectedField(field string) bool {
	return field == "max_output_tokens" || field == "transformer_metadata" || regexp.MustCompile(`^input\[\d+\]\.namespace$`).MatchString(field)
}

func isResponsesFunctionCall(itemType string) bool {
	return itemType == "function_call" || itemType == "custom_tool_call" || itemType == "tool_search_call"
}

func isResponsesFunctionOutput(itemType string) bool {
	return itemType == "function_call_output" || itemType == "custom_tool_call_output" || itemType == "tool_search_output"
}

func rawString(raw json.RawMessage) string {
	var value string
	_ = platformencoding.Unmarshal(raw, &value)
	return strings.TrimSpace(value)
}

func hasNonEmptyRawString(raw json.RawMessage) bool { return rawString(raw) != "" }
