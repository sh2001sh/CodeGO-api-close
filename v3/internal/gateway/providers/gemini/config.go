package gemini

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
)

func chatConfig(root gjson.Result) (generationConfig, error) {
	out := generationConfig{}
	if err := applyFloatConfigFields(root, &out); err != nil {
		return out, err
	}
	if err := applyIntConfigFields(root, &out); err != nil {
		return out, err
	}
	if err := applyStopSequences(root, &out); err != nil {
		return out, err
	}
	if err := applyResponseFormat(root, &out); err != nil {
		return out, err
	}
	if err := applyReasoningEffort(root, &out); err != nil {
		return out, err
	}
	return out, nil
}

func applyFloatConfigFields(root gjson.Result, out *generationConfig) error {
	for _, field := range []struct {
		name string
		dest **float64
	}{{"temperature", &out.Temperature}, {"top_p", &out.TopP}, {"presence_penalty", &out.PresencePenalty}, {"frequency_penalty", &out.FrequencyPenalty}} {
		if value := root.Get(field.name); value.Exists() {
			if value.Type != gjson.Number {
				return fmt.Errorf("gemini: %s must be a number", field.name)
			}
			v := value.Float()
			*field.dest = &v
		}
	}
	return nil
}

func applyIntConfigFields(root gjson.Result, out *generationConfig) error {
	for _, field := range []struct {
		name string
		dest **int64
	}{{"max_tokens", &out.MaxTokens}, {"max_completion_tokens", &out.MaxTokens}, {"n", &out.Candidates}, {"seed", &out.Seed}} {
		if value := root.Get(field.name); value.Exists() {
			if value.Type != gjson.Number || value.Float() != float64(value.Int()) {
				return fmt.Errorf("gemini: %s must be an integer", field.name)
			}
			v := value.Int()
			*field.dest = &v
		}
	}
	if out.MaxTokens != nil && *out.MaxTokens <= 0 {
		return fmt.Errorf("gemini: maximum output tokens must be positive")
	}
	if out.Candidates != nil && (*out.Candidates < 1 || *out.Candidates > 8) {
		return fmt.Errorf("gemini: n must be between 1 and 8")
	}
	return nil
}

func applyStopSequences(root gjson.Result, out *generationConfig) error {
	if stop := root.Get("stop"); stop.Exists() && stop.Type != gjson.Null {
		switch {
		case stop.Type == gjson.String:
			out.Stop = []string{stop.Str}
		case stop.IsArray():
			for _, value := range stop.Array() {
				if value.Type != gjson.String {
					return fmt.Errorf("gemini: stop sequences must be strings")
				}
				out.Stop = append(out.Stop, value.Str)
			}
		default:
			return fmt.Errorf("gemini: invalid stop sequences")
		}
	}
	if len(out.Stop) > 5 {
		return fmt.Errorf("gemini: at most five stop sequences are supported")
	}
	return nil
}

func applyResponseFormat(root gjson.Result, out *generationConfig) error {
	format := root.Get("response_format")
	if !format.Exists() || format.Type == gjson.Null {
		return nil
	}
	switch format.Get("type").Str {
	case "text":
	case "json_object":
		out.MIMEType = "application/json"
	case "json_schema":
		schema := format.Get("json_schema.schema")
		if !schema.IsObject() {
			return fmt.Errorf("gemini: response JSON schema must be an object")
		}
		out.MIMEType, out.Schema = "application/json", json.RawMessage(schema.Raw)
	default:
		return fmt.Errorf("gemini: unsupported response format")
	}
	return nil
}

func applyReasoningEffort(root gjson.Result, out *generationConfig) error {
	effort := root.Get("reasoning_effort")
	if !effort.Exists() {
		return nil
	}
	budgets := map[string]int64{"none": 0, "minimal": 128, "low": 1024, "medium": 8192, "high": 24576}
	budget, ok := budgets[effort.Str]
	if !ok {
		return fmt.Errorf("gemini: unsupported reasoning effort")
	}
	out.Thinking = &thinkingConfig{Budget: budget, Include: budget != 0}
	return nil
}

func chatTools(root gjson.Result, out *generateRequest) error {
	var declarations []functionDeclaration
	for _, value := range root.Get("tools").Array() {
		if value.Get("type").Str != "function" || value.Get("function.name").Str == "" {
			return fmt.Errorf("gemini: only named function tools are supported")
		}
		if value.Get("function.strict").Bool() {
			return fmt.Errorf("gemini: strict function schema enforcement is unsupported")
		}
		params := value.Get("function.parameters")
		if params.Exists() && !params.IsObject() {
			return fmt.Errorf("gemini: function parameters must be a JSON schema object")
		}
		declarations = append(declarations, functionDeclaration{Name: value.Get("function.name").Str,
			Description: value.Get("function.description").Str, Parameters: json.RawMessage(params.Raw)})
	}
	if len(declarations) > 0 {
		out.Tools = []tool{{Functions: declarations}}
	}
	choice := root.Get("tool_choice")
	if !choice.Exists() || choice.Type == gjson.Null {
		return nil
	}
	config := functionCallingConfig{}
	if choice.Type == gjson.String {
		switch choice.Str {
		case "auto":
			config.Mode = "AUTO"
		case "none":
			config.Mode = "NONE"
		case "required":
			config.Mode = "ANY"
		default:
			return fmt.Errorf("gemini: unsupported tool choice")
		}
	} else {
		name := choice.Get("function.name").Str
		if choice.Get("type").Str != "function" || name == "" {
			return fmt.Errorf("gemini: unsupported tool choice")
		}
		found := false
		for _, declaration := range declarations {
			found = found || declaration.Name == name
		}
		if !found {
			return fmt.Errorf("gemini: chosen function is not declared")
		}
		config.Mode, config.Allowed = "ANY", []string{name}
	}
	out.Tool = &toolConfig{Calling: config}
	return nil
}
