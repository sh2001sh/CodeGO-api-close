package gateway

import (
	"errors"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func overrideSettingBool(settings map[string]any, key string) (bool, error) {
	value, exists := settings[key]
	if !exists || value == nil {
		return false, nil
	}
	result, ok := value.(bool)
	if !ok {
		return false, errors.New("channel boolean setting is invalid")
	}
	return result, nil
}

// These are the actual v2 ChannelOtherSettings switches, rather than invented
// fields such as disable_thinking. Param operations run after this filtering,
// so administrators can explicitly restore a field as in v2.
func applyOverrideSettings(data []byte, settings map[string]any, anthropic bool, req *Request) ([]byte, error) {
	passThrough, err := overrideSettingBool(settings, "pass_through_body_enabled")
	if err != nil {
		return nil, err
	}
	if passThrough {
		return data, nil
	}
	if !passThrough {
		// Collect only top-level names in one scan. Looking up each absent
		// policy field separately would rescan the entire long input.
		present := make(map[string]bool)
		gjson.GetBytes(data, "@keys").ForEach(func(_, key gjson.Result) bool {
			present[key.Str] = true
			return true
		})
		for field, setting := range map[string]string{"service_tier": "allow_service_tier", "inference_geo": "allow_inference_geo", "speed": "allow_speed", "safety_identifier": "allow_safety_identifier", "stream_options.include_obfuscation": "allow_include_obfuscation"} {
			allowed, err := overrideSettingBool(settings, setting)
			if err != nil {
				return nil, err
			}
			exists := present[field]
			if field == "stream_options.include_obfuscation" {
				exists = present["stream_options"] && gjson.GetBytes(data, field).Exists()
			}
			// Fast was explicitly admitted and priced. Filtering then restoring
			// it would copy a long input twice without changing its meaning.
			if !allowed && exists && (field != "service_tier" || FastServiceTier(req) == "") {
				data, err = sjson.DeleteBytes(data, field)
				if err != nil {
					return nil, err
				}
			}
		}
		disabled, err := overrideSettingBool(settings, "disable_store")
		if err != nil {
			return nil, err
		}
		if disabled && present["store"] {
			data, err = sjson.DeleteBytes(data, "store")
			if err != nil {
				return nil, err
			}
		}
		if present["stream_options"] {
			if streamOptions := gjson.GetBytes(data, "stream_options"); streamOptions.IsObject() && len(streamOptions.Map()) == 0 {
				data, err = sjson.DeleteBytes(data, "stream_options")
				if err != nil {
					return nil, err
				}
			}
		}
	}
	configured, exists := settings["system_prompt"]
	if !exists || configured == nil {
		return data, nil
	}
	prompt, ok := configured.(string)
	if !ok {
		return nil, errors.New("system prompt must be text")
	}
	if prompt == "" {
		return data, nil
	}
	replace, err := overrideSettingBool(settings, "system_prompt_override")
	if err != nil {
		return nil, err
	}
	return injectOverridePrompt(data, prompt, replace, anthropic)
}

func injectOverridePrompt(data []byte, prompt string, replace, anthropic bool) ([]byte, error) {
	switch {
	case gjson.GetBytes(data, "contents").IsArray():
		parts := gjson.GetBytes(data, "systemInstruction.parts")
		if !parts.Exists() || len(parts.Array()) == 0 {
			return sjson.SetBytes(data, "systemInstruction", map[string]any{"parts": []any{map[string]any{"text": prompt}}})
		}
		if !replace {
			return data, nil
		}
		for i, part := range parts.Array() {
			if part.Get("text").Exists() {
				return sjson.SetBytes(data, "systemInstruction.parts."+overrideIndex(i)+".text", prefixOverridePrompt(prompt, part.Get("text").String()))
			}
		}
		return sjson.SetBytes(data, "systemInstruction.parts", append([]any{map[string]any{"text": prompt}}, parts.Value().([]any)...))
	case gjson.GetBytes(data, "input").Exists() || gjson.GetBytes(data, "instructions").Exists():
		return injectOverrideTextPrompt(data, "instructions", prompt, replace)
	case anthropic || gjson.GetBytes(data, "anthropic_version").Exists():
		return injectOverrideTextPrompt(data, "system", prompt, replace)
	case gjson.GetBytes(data, "messages").IsArray():
		messages := gjson.GetBytes(data, "messages").Array()
		for i, message := range messages {
			if role := message.Get("role").String(); role == "system" || role == "developer" {
				if !replace {
					return data, nil
				}
				content := message.Get("content")
				path := "messages." + overrideIndex(i) + ".content"
				if content.Type == gjson.String {
					return sjson.SetBytes(data, path, prefixOverridePrompt(prompt, content.String()))
				}
				if content.IsArray() {
					return sjson.SetBytes(data, path, append([]any{map[string]any{"type": "text", "text": prompt}}, content.Value().([]any)...))
				}
				return nil, errors.New("invalid system message content")
			}
		}
		original := gjson.GetBytes(data, "messages").Value().([]any)
		return sjson.SetBytes(data, "messages", append([]any{map[string]any{"role": "system", "content": prompt}}, original...))
	default:
		return nil, errors.New("system prompt unsupported by upstream schema")
	}
}

func injectOverrideTextPrompt(data []byte, path, prompt string, replace bool) ([]byte, error) {
	original := gjson.GetBytes(data, path)
	if !original.Exists() || original.Type == gjson.Null {
		return sjson.SetBytes(data, path, prompt)
	}
	if !replace {
		return data, nil
	}
	if original.Type == gjson.String {
		return sjson.SetBytes(data, path, prefixOverridePrompt(prompt, original.String()))
	}
	if original.IsArray() {
		return sjson.SetBytes(data, path, append([]any{map[string]any{"type": "text", "text": prompt}}, original.Value().([]any)...))
	}
	return nil, errors.New("invalid system prompt schema")
}

func prefixOverridePrompt(prompt, original string) string {
	if strings.TrimSpace(original) == "" {
		return prompt
	}
	return prompt + "\n" + original
}

func overrideIndex(value int) string { return strconv.Itoa(value) }
