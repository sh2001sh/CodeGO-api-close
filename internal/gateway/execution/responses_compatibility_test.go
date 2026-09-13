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
	require.True(t, shouldNormalizeResponsesCompatibilityBody([]byte(`{"model":"gpt-5","input":[{"type":"agent_message"}]}`)))
}

func TestNormalizeResponsesCompatibilityBodyConvertsAgentMessage(t *testing.T) {
	body := []byte(`{"model":"gpt-6-astra","input":[{"type":"agent_message","content":[{"type":"encrypted_content","encrypted_content":"child result"}]}]}`)

	normalized, changed, err := normalizeResponsesCompatibilityBody(body)

	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"child result"}]}]}`, string(normalized))
}

func TestNormalizeResponsesCompatibilityBodyLiftsCodexMultiAgentFields(t *testing.T) {
	body := []byte(`{
		"model":"gpt-6-astra",
		"client_metadata":{"thread_id":"private"},
		"reasoning":{"effort":"ultra"},
		"input":[
			{"type":"additional_tools","id":"at_1","role":"system","tools":[{"type":"function","name":"spawn_agent","parameters":{"type":"object"}}]},
			{"type":"agent_message","id":"amsg_1","author":"/root/worker","recipient":"/root","phase":"commentary","content":[{"type":"input_text","text":"done"}]}
		]
	}`)

	normalized, changed, err := normalizeResponsesCompatibilityBody(body)

	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{
		"model":"gpt-6-astra",
		"reasoning":{"effort":"xhigh"},
		"tools":[{"type":"function","name":"spawn_agent","parameters":{"type":"object"}}],
		"input":[{"type":"message","id":"amsg_1","role":"user","content":[{"type":"input_text","text":"done"}]}]
	}`, string(normalized))
}

func TestNormalizeResponsesBackgroundFalseOmitsUnsupportedField(t *testing.T) {
	body := []byte(`{"model":"gpt-5","input":"hello","background":false,"stream":true}`)

	normalized, changed, err := normalizeResponsesBackgroundFalse(body)

	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{"model":"gpt-5","input":"hello","stream":true}`, string(normalized))
}

func TestNormalizeResponsesBackgroundTruePreservesNativeField(t *testing.T) {
	body := []byte(`{"model":"gpt-5","input":"hello","background":true,"stream":true}`)

	normalized, changed, err := normalizeResponsesBackgroundFalse(body)

	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, body, normalized)
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

func TestIsGenericInvalidRequestParametersError(t *testing.T) {
	matching := types.WithOpenAIError(types.OpenAIError{
		Code:    "invalid_request_error",
		Message: "Invalid request parameters. Check the request and try again.",
	}, http.StatusBadRequest)
	require.True(t, isGenericInvalidRequestParametersError(matching))

	explicit := types.WithOpenAIError(types.OpenAIError{
		Code:    "invalid_request_error",
		Message: "Unknown parameter: input[64].status",
	}, http.StatusBadRequest)
	require.False(t, isGenericInvalidRequestParametersError(explicit))

	require.False(t, isGenericInvalidRequestParametersError(types.WithOpenAIError(
		types.OpenAIError{Message: "Invalid request parameters"}, http.StatusOK,
	)))
}
