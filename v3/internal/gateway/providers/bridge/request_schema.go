package bridge

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

// Gemini spells JSON Schema types in upper case; retain every schema field
// while converting its types and nullable flag to their JSON Schema equivalents.
func geminiSchema(schema gjson.Result) (json.RawMessage, error) {
	if !schema.Exists() {
		return nil, nil
	}
	if !schema.IsObject() {
		return nil, fmt.Errorf("bridge: Gemini function parameters must be an object schema")
	}
	out := make(map[string]json.RawMessage)
	var err error
	schema.ForEach(func(key, value gjson.Result) bool {
		out[key.Str], err = geminiSchemaField(key.Str, value)
		if err == nil && key.Str == "nullable" {
			delete(out, key.Str) // emitted as a type union below
		}
		return err == nil
	})
	if err != nil {
		return nil, err
	}
	if schema.Get("nullable").Bool() {
		kind := strings.ToLower(schema.Get("type").Str)
		if kind == "" {
			return nil, fmt.Errorf("bridge: Gemini nullable schema without type requires native upstream")
		}
		out["type"], err = json.Marshal([]string{kind, "null"})
		if err != nil {
			return nil, err
		}
	}
	body, err := json.Marshal(out)
	return body, err
}

// geminiSchemaField converts a single Gemini schema field to its JSON Schema
// equivalent. For "nullable" the returned value is a placeholder (discarded
// by the caller) since the flag is folded into "type" separately.
func geminiSchemaField(key string, value gjson.Result) (json.RawMessage, error) {
	switch key {
	case "type":
		return textContent(strings.ToLower(value.Str)), nil
	case "nullable":
		return nil, nil
	case "properties", "$defs", "definitions":
		properties := make(map[string]json.RawMessage)
		var err error
		value.ForEach(func(name, child gjson.Result) bool {
			properties[name.Str], err = geminiSchema(child)
			return err == nil
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(properties)
	case "items":
		return geminiSchema(value)
	case "anyOf", "allOf", "oneOf":
		children := make([]json.RawMessage, 0)
		for _, child := range value.Array() {
			converted, err := geminiSchema(child)
			if err != nil {
				return nil, err
			}
			children = append(children, converted)
		}
		return json.Marshal(children)
	case "additionalProperties":
		if value.IsObject() {
			return geminiSchema(value)
		}
		return raw(value), nil
	default:
		return raw(value), nil
	}
}
