package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeFunctionParametersSchemaMergesTopLevelUnion(t *testing.T) {
	schema := map[string]any{
		"oneOf": []any{
			map[string]any{"type": "object", "properties": map[string]any{"mode": map[string]any{"const": "view"}, "id": map[string]any{"type": "string"}}, "required": []any{"mode", "id"}, "additionalProperties": false},
			map[string]any{"type": "object", "properties": map[string]any{"mode": map[string]any{"const": "create"}, "name": map[string]any{"type": "string"}}, "required": []any{"mode", "name"}, "additionalProperties": false},
		},
	}

	normalized, changed := NormalizeFunctionParametersSchema(schema)
	require.True(t, changed)
	object := normalized.(map[string]any)
	require.Equal(t, "object", object["type"])
	require.NotContains(t, object, "oneOf")
	require.Equal(t, []string{"mode"}, object["required"])
	properties := object["properties"].(map[string]any)
	require.Contains(t, properties, "id")
	require.Contains(t, properties, "name")
	require.Contains(t, properties["mode"].(map[string]any), "anyOf")
}

func TestNormalizeResponsesToolSchemasKeepsOrdinarySchema(t *testing.T) {
	request := &OpenAIResponsesRequest{Tools: json.RawMessage(`[
		{"type":"function","name":"ok","parameters":{"type":"object","properties":{"q":{"type":"string"}}}},
		{"type":"function","name":"union","strict":true,"parameters":{"anyOf":[{"type":"object","properties":{"a":{"type":"string"}}},{"type":"object","properties":{"b":{"type":"number"}}}]}}
	]`)}

	changed, err := request.NormalizeToolSchemas()
	require.NoError(t, err)
	require.True(t, changed)
	var tools []map[string]any
	require.NoError(t, json.Unmarshal(request.Tools, &tools))
	require.NotContains(t, tools[0], "strict")
	require.Equal(t, false, tools[1]["strict"])
	require.Equal(t, "object", tools[1]["parameters"].(map[string]any)["type"])
}

func TestGeneralOpenAIRequestRecognizesOnlyNonEmptyToolDefinitions(t *testing.T) {
	require.False(t, (&GeneralOpenAIRequest{Functions: json.RawMessage(`[]`)}).HasToolDefinitions())
	require.True(t, (&GeneralOpenAIRequest{Functions: json.RawMessage(`[{"name":"lookup"}]`)}).HasToolDefinitions())
	require.True(t, (&GeneralOpenAIRequest{Tools: []ToolCallRequest{{Type: "function"}}}).HasToolDefinitions())
}
