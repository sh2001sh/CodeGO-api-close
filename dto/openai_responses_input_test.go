package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesRequestStripsTopLevelInputNamespace(t *testing.T) {
	request := &OpenAIResponsesRequest{
		Input: json.RawMessage(`[
			{"type":"message","namespace":"internal","content":"hello"},
			{"type":"function_call","name":"weather","arguments":"{\"namespace\":\"keep\"}"}
		]`),
	}

	removed, err := request.StripUnsupportedInputNamespaces()
	require.NoError(t, err)
	require.True(t, removed)
	require.JSONEq(t, `[
		{"type":"message","content":"hello"},
		{"type":"function_call","name":"weather","arguments":"{\"namespace\":\"keep\"}"}
	]`, string(request.Input))
}

func TestOpenAIResponsesRequestPreservesStringInput(t *testing.T) {
	request := &OpenAIResponsesRequest{Input: json.RawMessage(`"hello"`)}

	removed, err := request.StripUnsupportedInputNamespaces()
	require.NoError(t, err)
	require.False(t, removed)
	require.JSONEq(t, `"hello"`, string(request.Input))
}
