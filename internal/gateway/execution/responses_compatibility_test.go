package execution

import (
	"net/http"
	"testing"

	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestNormalizeResponsesCompatibilityBodyRepairsToolHistory(t *testing.T) {
	body := []byte(`{
      "model":"gpt-5.6-sol",
      "include":["usage","reasoning.encrypted_content"],
      "transformer_metadata":{"client":"codex"},
      "input":[
        {"type":"function_call","call_id":"call_1","name":"lookup","namespace":"tools"},
        {"type":"function_call_output","call_id":"call_1","output":"ok"},
        {"type":"function_call_output","call_id":"orphan","output":"remove"}
      ]
    }`)

	normalized, changed, err := normalizeResponsesCompatibilityBody(body)

	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{
      "model":"gpt-5.6-sol",
      "include":["reasoning.encrypted_content"],
      "input":[
        {"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"},
        {"type":"function_call_output","call_id":"call_1","output":"ok"}
      ]
    }`, string(normalized))
}

func TestShouldNormalizeResponsesCompatibilityBodyFastRejectsOrdinaryBody(t *testing.T) {
	require.False(t, shouldNormalizeResponsesCompatibilityBody([]byte(`{"model":"gpt-5","stream":true,"input":"hello"}`)))
	require.True(t, shouldNormalizeResponsesCompatibilityBody([]byte(`{"model":"gpt-5","include":["usage"]}`)))
}

func TestNormalizeResponsesCompatibilityBodyPreservesContinuationOutput(t *testing.T) {
	body := []byte(`{
      "model":"gpt-5.6-sol",
      "previous_response_id":"resp_1",
      "input":[{"type":"function_call_output","call_id":"call_from_previous","output":"ok"}]
    }`)

	normalized, changed, err := normalizeResponsesCompatibilityBody(body)

	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, body, normalized)
}

func TestNormalizeResponsesCompatibilityBodyRemovesOutputWithoutLocalCall(t *testing.T) {
	body := []byte(`{
      "model":"gpt-5.6-sol",
      "input":[
        {"type":"function_call_output","call_id":"missing_call","output":"stale"},
        {"type":"message","role":"user","content":"continue"}
      ]
    }`)

	normalized, changed, err := normalizeResponsesCompatibilityBody(body)

	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{
      "model":"gpt-5.6-sol",
      "input":[{"type":"message","role":"user","content":"continue"}]
    }`, string(normalized))
}

func TestNormalizeRejectedResponsesFieldRemovesExplicitUnsupportedField(t *testing.T) {
	body := []byte(`{
      "model":"gpt-5.6-sol",
      "max_output_tokens":4096,
      "input":[{"type":"message","role":"user","content":"hello"}]
    }`)
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Code: "unsupported_parameter", Param: "max_output_tokens",
		Message: "Unsupported parameter: max_output_tokens",
	}, http.StatusBadRequest)

	normalized, field, changed := normalizeRejectedResponsesField(body, apiErr)

	require.True(t, changed)
	require.Equal(t, "max_output_tokens", field)
	require.JSONEq(t, `{
      "model":"gpt-5.6-sol",
      "input":[{"type":"message","role":"user","content":"hello"}]
    }`, string(normalized))
}

func TestNormalizeRejectedResponsesFieldUsesIndexedNamespaceMessage(t *testing.T) {
	body := []byte(`{
      "model":"gpt-5.6-sol",
      "input":[
        {"type":"function_call","call_id":"call_1","name":"first","namespace":"keep","arguments":"{}"},
        {"type":"function_call","call_id":"call_2","name":"second","namespace":"remove","arguments":"{}"}
      ]
    }`)
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Code:    "unknown_parameter",
		Message: "Unknown parameter: 'input[1].namespace'.",
	}, http.StatusBadRequest)

	normalized, field, changed := normalizeRejectedResponsesField(body, apiErr)

	require.True(t, changed)
	require.Equal(t, "input[1].namespace", field)
	require.Contains(t, string(normalized), `"namespace":"keep"`)
	require.NotContains(t, string(normalized), `"namespace":"remove"`)
}

func TestNormalizeRejectedResponsesFieldDoesNotRetryBusiness400(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":"hello"}`)
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Code: "missing_required_parameter", Param: "input[0].arguments",
		Message: "Missing required parameter: input[0].arguments",
	}, http.StatusBadRequest)

	normalized, field, changed := normalizeRejectedResponsesField(body, apiErr)

	require.False(t, changed)
	require.Empty(t, field)
	require.Nil(t, normalized)
}

func TestNormalizeRejectedResponsesFieldAcceptsGenericInvalidRequestCode(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","max_output_tokens":4096,"input":"hello"}`)
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Code:    "invalid_request_error",
		Message: "Unsupported parameter: max_output_tokens",
	}, http.StatusBadRequest)

	normalized, field, changed := normalizeRejectedResponsesField(body, apiErr)

	require.True(t, changed)
	require.Equal(t, "max_output_tokens", field)
	require.NotContains(t, string(normalized), "max_output_tokens")
}
