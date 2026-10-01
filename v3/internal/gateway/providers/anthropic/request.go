package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
)

func convertRequest(data []byte, model string) ([]byte, error) {
	if err := validateChatFields(data); err != nil {
		return nil, err
	}
	var in chatRequest
	if err := json.Unmarshal(data, &in); err != nil {
		return nil, fmt.Errorf("anthropic: invalid Chat request: %w", err)
	}
	out := messagesRequest{Model: model, Stream: in.Stream, MaxTokens: 4096, Temperature: in.Temperature, TopP: in.TopP}
	if in.MaxTokens != nil {
		out.MaxTokens = *in.MaxTokens
	}
	if in.MaxCompletionTokens != nil {
		out.MaxTokens = *in.MaxCompletionTokens
	}
	if out.MaxTokens <= 0 {
		return nil, fmt.Errorf("anthropic: max_tokens must be positive")
	}
	if err := convertChatMessages(in, &out); err != nil {
		return nil, err
	}
	if len(out.Messages) == 0 {
		return nil, fmt.Errorf("anthropic: at least one non-system message is required")
	}
	if err := convertStop(in, &out); err != nil {
		return nil, err
	}
	if err := convertTools(in, &out); err != nil {
		return nil, err
	}
	if err := convertToolChoiceField(in, &out); err != nil {
		return nil, err
	}
	if in.User != "" {
		out.Metadata = &metadata{UserID: in.User}
	}
	return json.Marshal(out)
}

// convertChatMessages translates Chat messages into Anthropic messages,
// folding system/developer messages into out.System and merging adjacent
// same-role turns (e.g. consecutive tool results).
func convertChatMessages(in chatRequest, out *messagesRequest) error {
	for _, msg := range in.Messages {
		if len(msg.ToolCalls) > 0 && msg.Role != "assistant" {
			return fmt.Errorf("anthropic: tool_calls require an assistant message")
		}
		if msg.ToolCallID != "" && msg.Role != "tool" {
			return fmt.Errorf("anthropic: tool_call_id requires a tool message")
		}
		content, err := convertContent(msg.Content)
		if err != nil {
			return err
		}
		role, content, isSystem, err := convertMessageRole(msg, content, out)
		if err != nil {
			return err
		}
		if isSystem {
			continue
		}
		if len(content) == 0 {
			return fmt.Errorf("anthropic: message content cannot be empty")
		}
		appendTurn(out, role, content)
	}
	return nil
}

// convertMessageRole applies Chat-role-specific conversion (system folding,
// tool-result wrapping, assistant tool-use blocks) to one message's content.
// isSystem is true when the message was folded into out.System and the
// caller should skip appending a turn.
func convertMessageRole(msg chatMessage, content []block, out *messagesRequest) (role string, outContent []block, isSystem bool, err error) {
	switch msg.Role {
	case "system", "developer":
		for _, b := range content {
			if b.Type != "text" {
				return "", nil, false, fmt.Errorf("anthropic: system content must be text")
			}
		}
		out.System = append(out.System, content...)
		return "", nil, true, nil
	case "tool":
		if msg.ToolCallID == "" {
			return "", nil, false, fmt.Errorf("anthropic: tool result requires tool_call_id")
		}
		result, err := json.Marshal(content)
		if err != nil {
			return "", nil, false, err
		}
		if len(content) == 0 {
			result = []byte(`""`)
		}
		return "user", []block{{Type: "tool_result", ToolUseID: msg.ToolCallID, Content: result}}, false, nil
	case "assistant":
		for _, call := range msg.ToolCalls {
			if call.Type != "function" || call.ID == "" || call.Function.Name == "" {
				return "", nil, false, fmt.Errorf("anthropic: invalid assistant tool call")
			}
			if args := gjson.Parse(call.Function.Arguments); !gjson.Valid(call.Function.Arguments) || !args.IsObject() {
				return "", nil, false, fmt.Errorf("anthropic: tool arguments must be a JSON object")
			}
			content = append(content, block{Type: "tool_use", ID: call.ID, Name: call.Function.Name, Input: json.RawMessage(call.Function.Arguments)})
		}
		return "assistant", content, false, nil
	case "user":
		return "user", content, false, nil
	default:
		return "", nil, false, fmt.Errorf("anthropic: unsupported message role %q", msg.Role)
	}
}

// appendTurn merges content into the previous turn when it shares the same
// role (consecutive tool results and adjacent same-role messages are one
// turn), otherwise it appends a new turn.
func appendTurn(out *messagesRequest, role string, content []block) {
	last := len(out.Messages) - 1
	if last >= 0 && out.Messages[last].Role == role {
		out.Messages[last].Content = append(out.Messages[last].Content, content...)
	} else {
		out.Messages = append(out.Messages, message{Role: role, Content: content})
	}
}

func convertStop(in chatRequest, out *messagesRequest) error {
	if len(in.Stop) == 0 || string(in.Stop) == "null" {
		return nil
	}
	var one string
	if json.Unmarshal(in.Stop, &one) == nil {
		out.Stop = []string{one}
		return nil
	}
	if err := json.Unmarshal(in.Stop, &out.Stop); err != nil {
		return fmt.Errorf("anthropic: stop must be a string or string array")
	}
	return nil
}

func convertTools(in chatRequest, out *messagesRequest) error {
	for _, t := range in.Tools {
		if t.Type != "function" || t.Function.Name == "" {
			return fmt.Errorf("anthropic: only named function tools are supported")
		}
		if t.Function.Strict != nil && *t.Function.Strict {
			return fmt.Errorf("anthropic: strict tool schemas are unsupported")
		}
		schema := t.Function.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		if !gjson.ParseBytes(schema).IsObject() {
			return fmt.Errorf("anthropic: tool parameters must be an object")
		}
		out.Tools = append(out.Tools, tool{Name: t.Function.Name, Description: t.Function.Description, InputSchema: schema})
	}
	return nil
}

func convertToolChoiceField(in chatRequest, out *messagesRequest) error {
	if len(out.Tools) == 0 {
		if len(in.ToolChoice) > 0 && string(in.ToolChoice) != `"none"` && string(in.ToolChoice) != "null" {
			return fmt.Errorf("anthropic: tool_choice requires tools")
		}
		return nil
	}
	choice, err := convertToolChoice(in.ToolChoice)
	if err != nil {
		return err
	}
	if in.ParallelToolCalls != nil {
		disabled := !*in.ParallelToolCalls
		choice.DisableParallel = &disabled
	}
	out.ToolChoice = choice
	if choice.Type == "tool" {
		found := false
		for _, t := range out.Tools {
			if t.Name == choice.Name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("anthropic: tool_choice names an undeclared tool")
		}
	}
	return nil
}

func convertToolChoice(raw json.RawMessage) (*toolChoice, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return &toolChoice{Type: "auto"}, nil
	}
	var name string
	if json.Unmarshal(raw, &name) == nil {
		switch name {
		case "auto", "none":
			return &toolChoice{Type: name}, nil
		case "required":
			return &toolChoice{Type: "any"}, nil
		}
	} else {
		choice := gjson.ParseBytes(raw)
		if choice.Get("type").Str == "function" && choice.Get("function.name").Str != "" {
			return &toolChoice{Type: "tool", Name: choice.Get("function.name").Str}, nil
		}
	}
	return nil, fmt.Errorf("anthropic: unsupported tool_choice")
}

func validateChatFields(data []byte) error {
	root := gjson.ParseBytes(data)
	if !gjson.ValidBytes(data) || !root.IsObject() {
		return fmt.Errorf("anthropic: invalid Chat JSON")
	}
	if invalid := validateTopLevelFields(root); invalid != "" {
		return fmt.Errorf("anthropic: unsupported Chat field %q", invalid)
	}
	if invalid := validateMessagesAndTools(root); invalid != "" {
		return fmt.Errorf("anthropic: unsupported Chat field %q", invalid)
	}
	return nil
}

func validateTopLevelFields(root gjson.Result) string {
	var invalid string
	root.ForEach(func(key, val gjson.Result) bool {
		if val.Type == gjson.Null {
			return true
		}
		switch key.Str {
		case "model", "messages", "stream", "max_tokens", "max_completion_tokens", "temperature", "top_p", "stop", "tools", "tool_choice", "parallel_tool_calls", "user":
		case "stream_options":
			val.ForEach(func(k, _ gjson.Result) bool {
				if k.Str != "include_usage" {
					invalid = "stream_options." + k.Str
				}
				return invalid == ""
			})
		case "n":
			if val.Int() != 1 {
				invalid = key.Str
			}
		case "frequency_penalty", "presence_penalty":
			if val.Float() != 0 {
				invalid = key.Str
			}
		case "logprobs":
			if val.Bool() {
				invalid = key.Str
			}
		case "response_format":
			if val.Get("type").Str != "text" {
				invalid = key.Str
			}
		default:
			invalid = key.Str
		}
		return invalid == ""
	})
	return invalid
}

func validateMessagesAndTools(root gjson.Result) string {
	var invalid string
	for _, msg := range root.Get("messages").Array() {
		msg.ForEach(func(k, v gjson.Result) bool {
			switch k.Str {
			case "role", "content", "tool_calls", "tool_call_id":
			default:
				if v.Type != gjson.Null {
					invalid = "messages." + k.Str
				}
			}
			return invalid == ""
		})
		for _, call := range msg.Get("tool_calls").Array() {
			if bad := unknownField(call, "id", "type", "function"); bad != "" {
				invalid = "tool_calls." + bad
			}
			if bad := unknownField(call.Get("function"), "name", "arguments"); bad != "" {
				invalid = "tool_calls.function." + bad
			}
		}
	}
	for _, t := range root.Get("tools").Array() {
		if bad := unknownField(t, "type", "function"); bad != "" {
			invalid = "tools." + bad
		}
		if bad := unknownField(t.Get("function"), "name", "description", "parameters", "strict"); bad != "" {
			invalid = "tools.function." + bad
		}
	}
	return invalid
}

func unknownField(value gjson.Result, allowed ...string) string {
	var invalid string
	value.ForEach(func(k, v gjson.Result) bool {
		if v.Type == gjson.Null {
			return true
		}
		for _, field := range allowed {
			if k.Str == field {
				return true
			}
		}
		invalid = k.Str
		return false
	})
	return invalid
}
