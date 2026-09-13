package dto

import (
	"encoding/json"
	"encoding/xml"
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

// NormalizeCodexRemoteCompactionInput converts legacy generic item IDs in a
// Codex Responses history to the type-specific identifiers required by the
// remote-compaction protocol. Tool pairing continues to use call_id, which is
// intentionally never changed here.
func (r *OpenAIResponsesRequest) NormalizeCodexRemoteCompactionInput() (bool, error) {
	if r == nil || len(r.Input) == 0 {
		return false, nil
	}

	items, changed, err := normalizeCodexResponseItems(r.Input, true)
	if err != nil || !changed {
		return changed, err
	}
	r.Input = items
	return true, nil
}

// NormalizeCodexRemoteCompactionResponse normalizes item IDs in a non-stream
// Responses payload before it is returned to a remote-compaction client.
func NormalizeCodexRemoteCompactionResponse(body []byte) ([]byte, bool, error) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		return body, false, err
	}
	output, found := response["output"]
	if !found {
		return body, false, nil
	}
	normalizedOutput, changed, err := normalizeCodexResponseItems(output, false)
	if err != nil || !changed {
		return body, changed, err
	}
	response["output"] = normalizedOutput
	normalizedBody, err := json.Marshal(response)
	if err != nil {
		return body, false, err
	}
	return normalizedBody, true, nil
}

// NormalizeCodexRemoteCompactionStreamEvent normalizes one Responses SSE
// payload. The ID map keeps subsequent delta events aligned with a rewritten
// output-item event without altering call_id.
func NormalizeCodexRemoteCompactionStreamEvent(body []byte, rewrites map[string]string) ([]byte, bool, error) {
	var event map[string]json.RawMessage
	if err := json.Unmarshal(body, &event); err != nil {
		return body, false, err
	}

	changed := false
	if item, found := event["item"]; found {
		normalizedItem, itemChanged, oldID, newID, err := normalizeCodexResponseItem(item, false)
		if err != nil {
			return body, false, err
		}
		if itemChanged {
			event["item"] = normalizedItem
			changed = true
			if oldID != "" && newID != "" && rewrites != nil {
				rewrites[oldID] = newID
			}
		}
	}
	if itemID, found := jsonRawString(event["item_id"]); found {
		if replacement, exists := rewrites[itemID]; exists {
			raw, err := json.Marshal(replacement)
			if err != nil {
				return body, false, err
			}
			event["item_id"] = raw
			changed = true
		}
	}
	if !changed {
		return body, false, nil
	}
	normalizedBody, err := json.Marshal(event)
	if err != nil {
		return body, false, err
	}
	return normalizedBody, true, nil
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
	if hasID && expectedPrefix != "" && strings.HasPrefix(oldID, "item_") {
		suffix := strings.TrimPrefix(oldID, "item_")
		if suffix != "" {
			newID = expectedPrefix + "_" + suffix
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
