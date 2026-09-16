package dto

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

var forbiddenTopLevelFunctionSchemaKeywords = []string{"oneOf", "anyOf", "allOf", "enum", "const", "not"}

// HasToolDefinitions reports whether a Chat Completions request contains at
// least one modern or legacy function definition.
func (r *GeneralOpenAIRequest) HasToolDefinitions() bool {
	if r == nil {
		return false
	}
	if len(r.Tools) > 0 {
		return true
	}
	var functions []json.RawMessage
	return len(r.Functions) > 0 && json.Unmarshal(r.Functions, &functions) == nil && len(functions) > 0
}

// NormalizeToolSchemas converts top-level JSON Schema unions into a portable
// object schema. OpenAI-compatible endpoints reject top-level combinators even
// though clients and MCP servers commonly emit them for discriminated unions.
func (r *GeneralOpenAIRequest) NormalizeToolSchemas() (bool, error) {
	if r == nil {
		return false, nil
	}
	changed := false
	for index := range r.Tools {
		tool := &r.Tools[index]
		if tool.Type == "function" {
			normalized, schemaChanged := NormalizeFunctionParametersSchema(tool.Function.Parameters)
			if schemaChanged {
				tool.Function.Parameters = normalized
				strict := false
				tool.Function.Strict = &strict
				changed = true
			}
		}
		if len(tool.Tools) > 0 {
			normalized, nestedChanged, err := normalizeRawToolList(tool.Tools)
			if err != nil {
				return false, err
			}
			if nestedChanged {
				tool.Tools = normalized
				changed = true
			}
		}
	}
	return changed, nil
}

func (r *OpenAIResponsesRequest) NormalizeToolSchemas() (bool, error) {
	if r == nil || len(r.Tools) == 0 {
		return false, nil
	}
	normalized, changed, err := normalizeRawToolList(r.Tools)
	if err != nil || !changed {
		return changed, err
	}
	r.Tools = normalized
	return true, nil
}

func normalizeRawToolList(raw json.RawMessage) (json.RawMessage, bool, error) {
	var tools []any
	if err := json.Unmarshal(raw, &tools); err != nil {
		return raw, false, err
	}
	changed := normalizeToolList(tools)
	if !changed {
		return raw, false, nil
	}
	normalized, err := json.Marshal(tools)
	return normalized, err == nil, err
}

func normalizeToolList(tools []any) bool {
	changed := false
	for _, rawTool := range tools {
		tool, ok := rawTool.(map[string]any)
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(stringValue(tool["type"])), "function") {
			if parameters, found := tool["parameters"]; found {
				normalized, schemaChanged := NormalizeFunctionParametersSchema(parameters)
				if schemaChanged {
					tool["parameters"] = normalized
					tool["strict"] = false
					changed = true
				}
			}
		}
		if nested, ok := tool["tools"].([]any); ok && normalizeToolList(nested) {
			changed = true
		}
	}
	return changed
}

// NormalizeFunctionParametersSchema retains the fields accepted by every
// union branch, moves conflicting property constraints below properties, and
// removes only keywords forbidden at the parameter schema root.
func NormalizeFunctionParametersSchema(value any) (any, bool) {
	schema, ok := asStringAnyMap(value)
	if !ok {
		return value, false
	}
	needsNormalization := stringValue(schema["type"]) != "object"
	for _, keyword := range forbiddenTopLevelFunctionSchemaKeywords {
		if _, found := schema[keyword]; found {
			needsNormalization = true
		}
	}
	if !needsNormalization {
		return value, false
	}

	normalized := make(map[string]any, len(schema)+2)
	for key, candidate := range schema {
		normalized[key] = candidate
	}
	properties := copyProperties(normalized["properties"])
	branches := unionObjectBranches(schema)
	required := requiredSet(normalized["required"])
	if len(branches) > 0 {
		var commonRequired map[string]struct{}
		allDisallowAdditional := true
		for index, branch := range branches {
			for name, property := range copyProperties(branch["properties"]) {
				properties[name] = mergePropertySchema(properties[name], property)
			}
			branchRequired := requiredSet(branch["required"])
			if index == 0 {
				commonRequired = branchRequired
			} else {
				commonRequired = intersectSets(commonRequired, branchRequired)
			}
			if disallow, ok := branch["additionalProperties"].(bool); !ok || !disallow {
				allDisallowAdditional = false
			}
		}
		for field := range commonRequired {
			required[field] = struct{}{}
		}
		if allDisallowAdditional {
			normalized["additionalProperties"] = false
		}
	}
	for _, keyword := range forbiddenTopLevelFunctionSchemaKeywords {
		delete(normalized, keyword)
	}
	normalized["type"] = "object"
	if len(properties) > 0 {
		normalized["properties"] = properties
	}
	if len(required) > 0 {
		fields := make([]string, 0, len(required))
		for field := range required {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		normalized["required"] = fields
	} else {
		delete(normalized, "required")
	}
	return normalized, true
}

func unionObjectBranches(schema map[string]any) []map[string]any {
	var branches []map[string]any
	for _, keyword := range []string{"oneOf", "anyOf", "allOf"} {
		values, _ := schema[keyword].([]any)
		for _, value := range values {
			if branch, ok := asStringAnyMap(value); ok {
				branches = append(branches, branch)
			}
		}
	}
	return branches
}

func asStringAnyMap(value any) (map[string]any, bool) {
	if schema, ok := value.(map[string]any); ok {
		return schema, true
	}
	raw, ok := value.(json.RawMessage)
	if !ok {
		return nil, false
	}
	var schema map[string]any
	if json.Unmarshal(raw, &schema) != nil {
		return nil, false
	}
	return schema, true
}

func copyProperties(value any) map[string]any {
	properties := make(map[string]any)
	if source, ok := asStringAnyMap(value); ok {
		for name, property := range source {
			properties[name] = property
		}
	}
	return properties
}

func requiredSet(value any) map[string]struct{} {
	result := make(map[string]struct{})
	switch fields := value.(type) {
	case []any:
		for _, field := range fields {
			if name := stringValue(field); name != "" {
				result[name] = struct{}{}
			}
		}
	case []string:
		for _, field := range fields {
			if field != "" {
				result[field] = struct{}{}
			}
		}
	}
	return result
}

func intersectSets(left, right map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{})
	for value := range left {
		if _, found := right[value]; found {
			result[value] = struct{}{}
		}
	}
	return result
}

func mergePropertySchema(existing, next any) any {
	if existing == nil || reflect.DeepEqual(existing, next) {
		return next
	}
	return map[string]any{"anyOf": []any{existing, next}}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
