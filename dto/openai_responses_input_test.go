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

func TestNormalizeCodexDelegationBootstrap(t *testing.T) {
	req := &OpenAIResponsesRequest{Input: json.RawMessage(`[{"type":"function_call_output","namespace":"codex_app","name":"create_thread","output":"<codex_delegation>do work</codex_delegation>"}]`)}
	changed, err := req.NormalizeCodexDelegationBootstrap()
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `[{"type":"message","role":"user","content":[{"type":"input_text","text":"<codex_delegation>do work</codex_delegation>"}]}]`, string(req.Input))
}

func TestNormalizeCodexDelegationBootstrapKeepsPairedToolOutput(t *testing.T) {
	raw := json.RawMessage(`[{"type":"function_call","call_id":"call-1","name":"create_thread","namespace":"codex_app","arguments":"{}"},{"type":"function_call_output","call_id":"call-1","namespace":"codex_app","name":"create_thread","output":"<codex_delegation>do work</codex_delegation>"}]`)
	req := &OpenAIResponsesRequest{Input: raw}
	changed, err := req.NormalizeCodexDelegationBootstrap()
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, raw, req.Input)
}

func TestNormalizeCodexDelegationBootstrapSupportsTUIAndRejectsInvalidXML(t *testing.T) {
	req := &OpenAIResponsesRequest{Input: json.RawMessage(`[
		{"type":"function_call_output","namespace":"codex_tui","name":"send_message_to_thread","output":"<codex_delegation><message>continue</message></codex_delegation>"},
		{"type":"function_call_output","namespace":"codex_app","name":"create_thread","output":"<codex_delegation>"}
	]`)}
	changed, err := req.NormalizeCodexDelegationBootstrap()
	require.NoError(t, err)
	require.True(t, changed)
	var items []map[string]any
	require.NoError(t, json.Unmarshal(req.Input, &items))
	require.Equal(t, "message", items[0]["type"])
	require.Equal(t, "function_call_output", items[1]["type"])
}

func TestOpenAIResponsesRequestNormalizesRemoteCompactionItemIDs(t *testing.T) {
	request := &OpenAIResponsesRequest{
		Input: json.RawMessage(`[
			{"type":"message","id":"item_message","role":"user","content":[]},
			{"type":"custom_tool_call","id":"item_call","call_id":"call_1","name":"shell","input":"{}"},
			{"type":"custom_tool_call_output","id":"item_output","call_id":"call_1","output":"ok"},
			{"type":"function_call","id":"fc_existing","call_id":"call_2","name":"read","arguments":"{}"}
		]`),
	}

	changed, err := request.NormalizeCodexRemoteCompactionInput()
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `[
		{"type":"message","id":"msg_message","role":"user","content":[]},
		{"type":"custom_tool_call","id":"ctc_call","call_id":"call_1","name":"shell","input":"{}"},
		{"type":"custom_tool_call_output","id":"ctco_output","call_id":"call_1","output":"ok"},
		{"type":"function_call","id":"fc_existing","call_id":"call_2","name":"read","arguments":"{}"}
	]`, string(request.Input))
}

func TestOpenAIResponsesRequestNormalizesRemoteCompactionInputIdempotently(t *testing.T) {
	request := &OpenAIResponsesRequest{
		Input: json.RawMessage(`[{"type":"custom_tool_call","id":"ctc_call","call_id":"call_1","name":"shell","input":"{}"}]`),
	}

	changed, err := request.NormalizeCodexRemoteCompactionInput()
	require.NoError(t, err)
	require.False(t, changed)
}

func TestNormalizeCodexRemoteCompactionResponseAndStreamEvent(t *testing.T) {
	response, changed, err := NormalizeCodexRemoteCompactionResponse([]byte(`{
		"id":"resp_1",
		"output":[{"type":"custom_tool_call","id":"item_call","call_id":"call_1","name":"shell","input":"{}"}]
	}`))
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{
		"id":"resp_1",
		"output":[{"type":"custom_tool_call","id":"ctc_call","call_id":"call_1","name":"shell","input":"{}"}]
	}`, string(response))

	rewrites := make(map[string]string)
	itemAdded, changed, err := NormalizeCodexRemoteCompactionStreamEvent([]byte(`{
		"type":"response.output_item.added",
		"item":{"type":"custom_tool_call","id":"item_call","call_id":"call_1","name":"shell","input":"{}"}
	}`), rewrites)
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{
		"type":"response.output_item.added",
		"item":{"type":"custom_tool_call","id":"ctc_call","call_id":"call_1","name":"shell","input":"{}"}
	}`, string(itemAdded))

	delta, changed, err := NormalizeCodexRemoteCompactionStreamEvent([]byte(`{
		"type":"response.custom_tool_call_input.delta",
		"item_id":"item_call",
		"delta":"{}"
	}`), rewrites)
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{
		"type":"response.custom_tool_call_input.delta",
		"item_id":"ctc_call",
		"delta":"{}"
	}`, string(delta))
}
