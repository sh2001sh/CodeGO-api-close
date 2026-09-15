package dto

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"
)

// StripUnsupportedInputNamespaces removes non-standard namespace metadata from
// top-level Responses input items. Some clients replay this internal metadata
// in long conversations, while strict Responses-compatible upstreams reject it.
func (r *OpenAIResponsesRequest) StripUnsupportedInputNamespaces() (bool, error) {
	if r == nil || len(r.Input) == 0 {
		return false, nil
	}

	var items []json.RawMessage
	if err := json.Unmarshal(r.Input, &items); err != nil {
		return false, nil
	}

	removed := false
	for index, item := range items {
		var inputItem map[string]json.RawMessage
		if err := json.Unmarshal(item, &inputItem); err != nil {
			continue
		}
		if _, found := inputItem["namespace"]; !found {
			continue
		}
		delete(inputItem, "namespace")
		normalizedItem, err := json.Marshal(inputItem)
		if err != nil {
			return false, err
		}
		items[index] = normalizedItem
		removed = true
	}
	if !removed {
		return false, nil
	}

	normalizedInput, err := json.Marshal(items)
	if err != nil {
		return false, err
	}
	r.Input = normalizedInput
	return true, nil
}

// NormalizeCodexDelegationBootstrap converts the function_call_output emitted
// by Codex when create_thread/send_message_to_thread starts a child agent into
// a normal user message. Delegation output is a new user turn, not a tool
// result; forwarding it as function_call_output makes strict Responses
// gateways reject the request because it has no matching call_id.
func (r *OpenAIResponsesRequest) NormalizeCodexDelegationBootstrap() (bool, error) {
	if r == nil || len(r.Input) == 0 {
		return false, nil
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(r.Input, &items); err != nil {
		return false, nil
	}
	changed := false
	for index, item := range items {
		if !isCodexDelegationItem(item) {
			continue
		}
		output, _ := jsonRawString(item["output"])
		items[index] = map[string]json.RawMessage{
			"type":    json.RawMessage(`"message"`),
			"role":    json.RawMessage(`"user"`),
			"content": mustJSONRaw([]map[string]string{{"type": "input_text", "text": output}}),
		}
		changed = true
	}
	if !changed {
		return false, nil
	}
	normalized, err := json.Marshal(items)
	if err != nil {
		return false, err
	}
	r.Input = normalized
	return true, nil
}

// NormalizeCodexAgentMessages converts Codex MultiAgent V2 agent_message
// items into portable Responses user messages. Some Responses-compatible
// upstreams reject agent_message even though Codex emits it as conversation
// input after a child agent replies.
func (r *OpenAIResponsesRequest) NormalizeCodexAgentMessages() (bool, error) {
	if r == nil || len(r.Input) == 0 {
		return false, nil
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(r.Input, &items); err != nil {
		return false, nil
	}
	changed := false
	for _, item := range items {
		typ, _ := jsonRawString(item["type"])
		if typ != "agent_message" {
			continue
		}
		var parts []map[string]json.RawMessage
		if raw, ok := item["content"]; ok && json.Unmarshal(raw, &parts) == nil {
			for _, part := range parts {
				partType, _ := jsonRawString(part["type"])
				if partType != "encrypted_content" {
					continue
				}
				text, ok := jsonRawString(part["encrypted_content"])
				if !ok {
					continue
				}
				part["type"] = json.RawMessage(`"input_text"`)
				part["text"] = mustJSONRaw(text)
				delete(part, "encrypted_content")
			}
			item["content"] = mustJSONRaw(parts)
		}
		item["type"] = json.RawMessage(`"message"`)
		item["role"] = json.RawMessage(`"user"`)
		delete(item, "author")
		delete(item, "recipient")
		delete(item, "phase")
		changed = true
	}
	if !changed {
		return false, nil
	}
	normalized, err := json.Marshal(items)
	if err != nil {
		return false, err
	}
	r.Input = normalized
	return true, nil
}

// LiftCodexAdditionalTools moves turn-scoped tool declarations emitted by
// Codex Responses Lite from input into the public top-level tools field.
func (r *OpenAIResponsesRequest) LiftCodexAdditionalTools() (bool, error) {
	if r == nil || len(r.Input) == 0 {
		return false, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(r.Input, &items); err != nil {
		return false, nil
	}
	var tools []json.RawMessage
	if len(r.Tools) > 0 && string(r.Tools) != "null" {
		if err := json.Unmarshal(r.Tools, &tools); err != nil {
			return false, err
		}
	}
	kept := make([]json.RawMessage, 0, len(items))
	changed := false
	for _, raw := range items {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(raw, &item); err != nil {
			kept = append(kept, raw)
			continue
		}
		typ, _ := jsonRawString(item["type"])
		if typ != "additional_tools" {
			kept = append(kept, raw)
			continue
		}
		var additional []json.RawMessage
		if err := json.Unmarshal(item["tools"], &additional); err != nil {
			return false, fmt.Errorf("additional_tools.tools must be an array: %w", err)
		}
		tools = append(tools, additional...)
		changed = true
	}
	if !changed {
		return false, nil
	}
	input, err := json.Marshal(kept)
	if err != nil {
		return false, err
	}
	encodedTools, err := json.Marshal(tools)
	if err != nil {
		return false, err
	}
	r.Input, r.Tools = input, encodedTools
	return true, nil
}

// StripCodexMessageMetadata removes private multi-agent routing fields from
// ordinary messages after Codex history has been converted for a portable
// Responses provider.
func (r *OpenAIResponsesRequest) StripCodexMessageMetadata() (bool, error) {
	if r == nil || len(r.Input) == 0 {
		return false, nil
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(r.Input, &items); err != nil {
		return false, nil
	}
	changed := false
	for _, item := range items {
		typ, _ := jsonRawString(item["type"])
		if typ != "message" {
			continue
		}
		for _, field := range []string{"author", "recipient", "phase"} {
			if _, found := item[field]; found {
				delete(item, field)
				changed = true
			}
		}
	}
	if !changed {
		return false, nil
	}
	normalized, err := json.Marshal(items)
	if err != nil {
		return false, err
	}
	r.Input = normalized
	return true, nil
}

// NormalizePortableReasoningEffort handles clients that send Codex's UI-only
// ultra level directly. xhigh is the closest portable wire-level fallback.
func (r *OpenAIResponsesRequest) NormalizePortableReasoningEffort() bool {
	if r == nil || r.Reasoning == nil || !strings.EqualFold(strings.TrimSpace(r.Reasoning.Effort), "ultra") {
		return false
	}
	r.Reasoning.Effort = "xhigh"
	return true
}

func isCodexDelegationItem(item map[string]json.RawMessage) bool {
	typ, _ := jsonRawString(item["type"])
	ns, _ := jsonRawString(item["namespace"])
	name, _ := jsonRawString(item["name"])
	if typ != "function_call_output" || (ns != "codex_app" && ns != "codex_tui") ||
		(name != "create_thread" && name != "send_message_to_thread") {
		return false
	}
	callID, callIDPresent := item["call_id"]
	if callIDPresent {
		value, ok := jsonRawString(callID)
		if !ok || strings.TrimSpace(value) != "" {
			return false
		}
	}
	output, ok := jsonRawString(item["output"])
	return ok && validCodexDelegationEnvelope(output)
}

func validCodexDelegationEnvelope(value string) bool {
	var envelope struct {
		XMLName xml.Name
		Inner   string `xml:",innerxml"`
	}
	if err := xml.Unmarshal([]byte(value), &envelope); err != nil {
		return false
	}
	return envelope.XMLName.Local == "codex_delegation" && strings.TrimSpace(envelope.Inner) != ""
}

func mustJSONRaw(value any) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

// NormalizeCodexRemoteCompactionInput makes a replayed Codex history portable
// across upstream accounts. Responses item IDs are server-owned: an ID that
// was returned by another upstream can look syntactically valid while still
// being rejected as an unknown item. Full input items do not need those IDs,
// so remote compaction removes them and keeps call_id for tool pairing.
func (r *OpenAIResponsesRequest) NormalizeCodexRemoteCompactionInput() (bool, error) {
	if r == nil || len(r.Input) == 0 {
		return false, nil
	}

	items, changed, err := sanitizeCodexRemoteCompactionItems(r.Input)
	if err != nil || !changed {
		return changed, err
	}
	r.Input = items
	return true, nil
}

func sanitizeCodexRemoteCompactionItems(input json.RawMessage) (json.RawMessage, bool, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(input, &items); err != nil {
		return input, false, nil
	}

	changed := false
	for index, raw := range items {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}
		itemChanged := false
		for _, field := range []string{"id", "namespace"} {
			if _, found := item[field]; found {
				delete(item, field)
				itemChanged = true
			}
		}
		if !itemChanged {
			continue
		}
		normalized, err := json.Marshal(item)
		if err != nil {
			return input, false, err
		}
		items[index] = normalized
		changed = true
	}
	if !changed {
		return input, false, nil
	}
	normalized, err := json.Marshal(items)
	if err != nil {
		return input, false, err
	}
	return normalized, true, nil
}

// NormalizeCodexInputItemIDs gives every known Responses input item an ID
// prefix matching its type. Codex can replay a custom tool item with an fc_
// or generic item_ ID, while strict upstreams require ctc_/ctco_ prefixes.
func (r *OpenAIResponsesRequest) NormalizeCodexInputItemIDs() (bool, error) {
	if r == nil || len(r.Input) == 0 {
		return false, nil
	}
	items, changed, err := normalizeCodexResponseItems(r.Input, false)
	if err != nil || !changed {
		return changed, err
	}
	r.Input = items
	return true, nil
}

func normalizeCodexResponseItems(input json.RawMessage, stripNamespace bool) (json.RawMessage, bool, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(input, &items); err != nil {
		return input, false, nil
	}

	changed := false
	for index, item := range items {
		normalizedItem, itemChanged, _, _, err := normalizeCodexResponseItem(item, stripNamespace)
		if err != nil {
			return input, false, err
		}
		if itemChanged {
			items[index] = normalizedItem
			changed = true
		}
	}
	if !changed {
		return input, false, nil
	}
	normalizedItems, err := json.Marshal(items)
	if err != nil {
		return input, false, err
	}
	return normalizedItems, true, nil
}

func normalizeCodexResponseItem(raw json.RawMessage, stripNamespace bool) (json.RawMessage, bool, string, string, error) {
	var item map[string]json.RawMessage
	if err := json.Unmarshal(raw, &item); err != nil {
		return raw, false, "", "", nil
	}

	changed := false
	if stripNamespace {
		if _, found := item["namespace"]; found {
			delete(item, "namespace")
			changed = true
		}
	}

	itemType, _ := jsonRawString(item["type"])
	expectedPrefix := codexResponseItemIDPrefix(itemType)
	oldID, hasID := jsonRawString(item["id"])
	newID := oldID
	if hasID && oldID != "" && expectedPrefix != "" && !strings.HasPrefix(oldID, expectedPrefix) {
		newID = expectedPrefix + "_" + oldID
		if strings.HasPrefix(oldID, "item_") {
			newID = expectedPrefix + "_" + strings.TrimPrefix(oldID, "item_")
		}
		if newID != expectedPrefix+"_" {
			normalizedID, err := json.Marshal(newID)
			if err != nil {
				return raw, false, "", "", err
			}
			item["id"] = normalizedID
			changed = true
		}
	}
	if !changed {
		return raw, false, oldID, newID, nil
	}
	normalizedItem, err := json.Marshal(item)
	if err != nil {
		return raw, false, "", "", err
	}
	return normalizedItem, true, oldID, newID, nil
}

func jsonRawString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func codexResponseItemIDPrefix(itemType string) string {
	switch itemType {
	case "additional_tools":
		return "at"
	case "message":
		return "msg"
	case "agent_message":
		return "amsg"
	case "reasoning":
		return "rs"
	case "local_shell_call":
		return "lsh"
	case "function_call":
		return "fc"
	case "function_call_output":
		return "fco"
	case "tool_search_call":
		return "tsc"
	case "tool_search_output":
		return "tso"
	case "custom_tool_call":
		return "ctc"
	case "custom_tool_call_output":
		return "ctco"
	case "web_search_call":
		return "ws"
	case "image_generation_call":
		return "ig"
	case "compaction", "context_compaction":
		return "cmp"
	default:
		return ""
	}
}
