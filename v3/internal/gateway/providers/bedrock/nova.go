package bedrock

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

func novaRequest(data []byte) ([]byte, error) {
	root := gjson.ParseBytes(data)
	if err := validateNovaFields(root); err != nil {
		return nil, err
	}
	out := map[string]any{"schemaVersion": "messages-v1"}
	messages := []any{}
	for _, msg := range root.Get("messages").Array() {
		content, err := novaContent(msg.Get("content"))
		if err != nil {
			return nil, err
		}
		messages = append(messages, map[string]any{"role": msg.Get("role").Str, "content": content})
	}
	out["messages"] = messages
	if system := root.Get("system"); system.Exists() {
		content, err := novaContent(system)
		if err != nil {
			return nil, err
		}
		out["system"] = content
	}
	if config := novaInferenceConfig(root); len(config) > 0 {
		out["inferenceConfig"] = config
	}
	if tools := root.Get("tools").Array(); len(tools) > 0 {
		toolConfig, err := novaToolConfig(root, tools)
		if err != nil {
			return nil, err
		}
		out["toolConfig"] = toolConfig
	}
	return json.Marshal(out)
}

func validateNovaFields(root gjson.Result) error {
	var unsupported string
	root.ForEach(func(k, v gjson.Result) bool {
		switch k.Str {
		case "anthropic_version", "messages", "system", "max_tokens", "temperature", "top_p", "top_k", "stop_sequences", "tools", "tool_choice":
		default:
			if v.Type != gjson.Null {
				unsupported = k.Str
			}
		}
		return unsupported == ""
	})
	if unsupported != "" {
		return fmt.Errorf("bedrock Nova: unsupported field %q", unsupported)
	}
	return nil
}

func novaInferenceConfig(root gjson.Result) map[string]any {
	config := map[string]any{}
	for from, to := range map[string]string{"max_tokens": "maxTokens", "temperature": "temperature", "top_p": "topP", "top_k": "topK", "stop_sequences": "stopSequences"} {
		if v := root.Get(from); v.Exists() {
			config[to] = v.Value()
		}
	}
	return config
}

func novaToolConfig(root gjson.Result, tools []gjson.Result) (map[string]any, error) {
	list := []any{}
	for _, tool := range tools {
		spec := map[string]any{"name": tool.Get("name").Str, "inputSchema": map[string]any{"json": tool.Get("input_schema").Value()}}
		if desc := tool.Get("description"); desc.Exists() {
			spec["description"] = desc.Str
		}
		list = append(list, map[string]any{"toolSpec": spec})
	}
	toolConfig := map[string]any{"tools": list}
	choice := root.Get("tool_choice")
	if choice.Exists() {
		if choice.Get("disable_parallel_tool_use").Bool() {
			return nil, fmt.Errorf("bedrock Nova: disabling parallel tools is unsupported")
		}
		switch choice.Get("type").Str {
		case "auto", "any":
			toolConfig["toolChoice"] = map[string]any{choice.Get("type").Str: map[string]any{}}
		case "tool":
			toolConfig["toolChoice"] = map[string]any{"tool": map[string]any{"name": choice.Get("name").Str}}
		default:
			return nil, fmt.Errorf("bedrock Nova: unsupported tool_choice")
		}
	}
	return toolConfig, nil
}

func novaContent(content gjson.Result) ([]any, error) {
	if content.Type == gjson.String {
		return []any{map[string]any{"text": content.Str}}, nil
	}
	if !content.IsArray() {
		return nil, fmt.Errorf("bedrock Nova: content must be text or blocks")
	}
	out := []any{}
	for _, block := range content.Array() {
		if block.Get("cache_control").Exists() {
			return nil, fmt.Errorf("bedrock Nova: cache_control is unsupported")
		}
		switch block.Get("type").Str {
		case "text":
			out = append(out, map[string]any{"text": block.Get("text").Str})
		case "image":
			src := block.Get("source")
			format := strings.TrimPrefix(src.Get("media_type").Str, "image/")
			if src.Get("type").Str != "base64" || (format != "png" && format != "jpeg" && format != "gif" && format != "webp") {
				return nil, fmt.Errorf("bedrock Nova: images require a supported base64 source")
			}
			out = append(out, map[string]any{"image": map[string]any{"format": format, "source": map[string]any{"bytes": src.Get("data").Str}}})
		case "tool_use":
			out = append(out, map[string]any{"toolUse": map[string]any{"toolUseId": block.Get("id").Str, "name": block.Get("name").Str, "input": block.Get("input").Value()}})
		case "tool_result":
			result, err := novaContent(block.Get("content"))
			if err != nil {
				return nil, err
			}
			status := "success"
			if block.Get("is_error").Bool() {
				status = "error"
			}
			out = append(out, map[string]any{"toolResult": map[string]any{"toolUseId": block.Get("tool_use_id").Str, "content": result, "status": status}})
		default:
			return nil, fmt.Errorf("bedrock Nova: unsupported content type %q", block.Get("type").Str)
		}
	}
	return out, nil
}

func novaUsage(usage gjson.Result) map[string]any {
	out := map[string]any{}
	for from, to := range map[string]string{"inputTokens": "input_tokens", "outputTokens": "output_tokens", "cacheReadInputTokenCount": "cache_read_input_tokens", "cacheWriteInputTokenCount": "cache_creation_input_tokens"} {
		if v := usage.Get(from); v.Exists() {
			out[to] = v.Value()
		}
	}
	return out
}

func nativeContent(content gjson.Result) ([]any, error) {
	out := []any{}
	for _, block := range content.Array() {
		if text := block.Get("text"); text.Exists() {
			out = append(out, map[string]any{"type": "text", "text": text.Str})
		} else if tool := block.Get("toolUse"); tool.Exists() {
			out = append(out, map[string]any{"type": "tool_use", "id": tool.Get("toolUseId").Str, "name": tool.Get("name").Str, "input": tool.Get("input").Value()})
		} else {
			return nil, fmt.Errorf("bedrock Nova: unsupported response content")
		}
	}
	return out, nil
}

func novaStop(reason string) string {
	switch reason {
	case "end_turn", "stop_sequence", "max_tokens", "tool_use":
		return reason
	case "content_filtered", "guardrail_intervened":
		return "refusal"
	default:
		return reason
	}
}

func novaResponse(data []byte) ([]byte, error) {
	root := gjson.ParseBytes(data)
	content, err := nativeContent(root.Get("output.message.content"))
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"id": "bedrock", "type": "message", "role": "assistant", "content": content, "stop_reason": novaStop(root.Get("stopReason").Str), "usage": novaUsage(root.Get("usage"))})
}

type novaStream struct {
	started bool
	stopped bool
	blocks  map[int]string
}

func (s *novaStream) convert(data []byte) ([][]byte, error) {
	root := gjson.ParseBytes(data)
	if !s.started && !root.Get("messageStart").Exists() {
		return nil, fmt.Errorf("bedrock Nova: event before message start")
	}
	if s.stopped && !root.Get("metadata").Exists() {
		return nil, fmt.Errorf("bedrock Nova: event after message stop")
	}
	events, err := s.convertEvent(root)
	if err != nil {
		return nil, err
	}
	out := make([][]byte, 0, len(events))
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			return nil, err
		}
		out = append(out, encoded)
	}
	return out, nil
}

// convertEvent dispatches a single Nova stream event to its Anthropic
// equivalent(s), tracking per-index content block state on s.
func (s *novaStream) convertEvent(root gjson.Result) ([]map[string]any, error) {
	switch {
	case root.Get("messageStart").Exists():
		return s.convertMessageStart()
	case root.Get("contentBlockStart").Exists():
		return s.convertContentBlockStart(root.Get("contentBlockStart"))
	case root.Get("contentBlockDelta").Exists():
		return s.convertContentBlockDelta(root.Get("contentBlockDelta"))
	case root.Get("contentBlockStop").Exists():
		return s.convertContentBlockStop(root)
	case root.Get("messageStop").Exists():
		return s.convertMessageStop(root)
	case root.Get("metadata").Exists():
		return s.convertMetadata(root)
	default:
		return nil, fmt.Errorf("bedrock Nova: unknown event")
	}
}

func (s *novaStream) convertMessageStart() ([]map[string]any, error) {
	if s.started {
		return nil, fmt.Errorf("bedrock Nova: duplicate message start")
	}
	s.started = true
	return []map[string]any{{"type": "message_start", "message": map[string]any{"id": "bedrock", "type": "message", "role": "assistant", "content": []any{}, "usage": map[string]any{}}}}, nil
}

func (s *novaStream) convertContentBlockStart(block gjson.Result) ([]map[string]any, error) {
	index := int(block.Get("contentBlockIndex").Int())
	tool := block.Get("start.toolUse")
	if !tool.Exists() {
		return nil, fmt.Errorf("bedrock Nova: unsupported content start")
	}
	if s.blocks == nil {
		s.blocks = map[int]string{}
	}
	if s.blocks[index] != "" {
		return nil, fmt.Errorf("bedrock Nova: duplicate content start")
	}
	s.blocks[index] = "tool"
	return []map[string]any{{"type": "content_block_start", "index": index, "content_block": map[string]any{"type": "tool_use", "id": tool.Get("toolUseId").Str, "name": tool.Get("name").Str, "input": map[string]any{}}}}, nil
}

func (s *novaStream) convertContentBlockDelta(block gjson.Result) ([]map[string]any, error) {
	index := int(block.Get("contentBlockIndex").Int())
	var events []map[string]any
	var delta map[string]any
	if text := block.Get("delta.text"); text.Exists() {
		if s.blocks[index] == "" {
			if s.blocks == nil {
				s.blocks = map[int]string{}
			}
			s.blocks[index] = "text"
			events = append(events, map[string]any{"type": "content_block_start", "index": index, "content_block": map[string]any{"type": "text", "text": ""}})
		}
		if s.blocks[index] != "text" {
			return nil, fmt.Errorf("bedrock Nova: text delta in tool block")
		}
		delta = map[string]any{"type": "text_delta", "text": text.Str}
	} else if tool := block.Get("delta.toolUse.input"); tool.Exists() {
		if s.blocks[index] != "tool" {
			return nil, fmt.Errorf("bedrock Nova: tool delta without tool start")
		}
		delta = map[string]any{"type": "input_json_delta", "partial_json": tool.Str}
	} else {
		return nil, fmt.Errorf("bedrock Nova: unsupported content delta")
	}
	events = append(events, map[string]any{"type": "content_block_delta", "index": index, "delta": delta})
	return events, nil
}

func (s *novaStream) convertContentBlockStop(root gjson.Result) ([]map[string]any, error) {
	index := int(root.Get("contentBlockStop.contentBlockIndex").Int())
	if s.blocks[index] == "" {
		return nil, fmt.Errorf("bedrock Nova: content stop without start")
	}
	delete(s.blocks, index)
	return []map[string]any{{"type": "content_block_stop", "index": index}}, nil
}

func (s *novaStream) convertMessageStop(root gjson.Result) ([]map[string]any, error) {
	if len(s.blocks) != 0 || root.Get("messageStop.stopReason").Str == "" {
		return nil, fmt.Errorf("bedrock Nova: incomplete message stop")
	}
	s.stopped = true
	return []map[string]any{{"type": "message_delta", "delta": map[string]any{"stop_reason": novaStop(root.Get("messageStop.stopReason").Str)}}}, nil
}

func (s *novaStream) convertMetadata(root gjson.Result) ([]map[string]any, error) {
	if !s.stopped {
		return nil, fmt.Errorf("bedrock Nova: metadata before message stop")
	}
	return []map[string]any{
		{"type": "message_delta", "delta": map[string]any{}, "usage": novaUsage(root.Get("metadata.usage"))},
		{"type": "message_stop"},
	}, nil
}
