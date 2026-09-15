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

func TestNormalizeCodexAgentMessages(t *testing.T) {
	req := &OpenAIResponsesRequest{Input: json.RawMessage(`[
		{"type":"agent_message","content":[{"type":"input_text","text":"plain"},{"type":"encrypted_content","encrypted_content":"delegated task"}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"keep"}]}
	]`)}
	changed, err := req.NormalizeCodexAgentMessages()
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"plain"},{"type":"input_text","text":"delegated task"}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"keep"}]}
	]`, string(req.Input))
}

func TestNormalizeCodexAgentMessagesStripsPrivateMetadata(t *testing.T) {
	req := &OpenAIResponsesRequest{Input: json.RawMessage(`[{"type":"agent_message","author":"/root/a","recipient":"/root","phase":"commentary","content":[{"type":"input_text","text":"done"}]}]`)}
	changed, err := req.NormalizeCodexAgentMessages()
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `[{"type":"message","role":"user","content":[{"type":"input_text","text":"done"}]}]`, string(req.Input))
}

func TestLiftCodexAdditionalTools(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Input: json.RawMessage(`[{"type":"message","role":"user","content":"hi"},{"type":"additional_tools","tools":[{"type":"function","name":"spawn_agent"}]}]`),
		Tools: json.RawMessage(`[{"type":"function","name":"existing"}]`),
	}
	changed, err := req.LiftCodexAdditionalTools()
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `[{"type":"message","role":"user","content":"hi"}]`, string(req.Input))
	require.JSONEq(t, `[{"type":"function","name":"existing"},{"type":"function","name":"spawn_agent"}]`, string(req.Tools))
}

func TestNormalizePortableReasoningEffort(t *testing.T) {
	req := &OpenAIResponsesRequest{Reasoning: &Reasoning{Effort: "ultra"}}
	require.True(t, req.NormalizePortableReasoningEffort())
	require.Equal(t, "xhigh", req.Reasoning.Effort)
}

func TestNormalizeCodexAgentMessagesPreservesStringInput(t *testing.T) {
	req := &OpenAIResponsesRequest{Input: json.RawMessage(`"hello"`)}
	changed, err := req.NormalizeCodexAgentMessages()
	require.NoError(t, err)
	require.False(t, changed)
	require.JSONEq(t, `"hello"`, string(req.Input))
}

func TestOpenAIResponsesRequestStripsServerOwnedRemoteCompactionItemIDs(t *testing.T) {
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
		{"type":"message","role":"user","content":[]},
		{"type":"custom_tool_call","call_id":"call_1","name":"shell","input":"{}"},
		{"type":"custom_tool_call_output","call_id":"call_1","output":"ok"},
		{"type":"function_call","call_id":"call_2","name":"read","arguments":"{}"}
	]`, string(request.Input))
}

func TestOpenAIResponsesRequestNormalizesRemoteCompactionInputIdempotently(t *testing.T) {
	request := &OpenAIResponsesRequest{
		Input: json.RawMessage(`[{"type":"custom_tool_call","id":"ctc_call","call_id":"call_1","name":"shell","input":"{}"}]`),
	}

	changed, err := request.NormalizeCodexRemoteCompactionInput()
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `[{"type":"custom_tool_call","call_id":"call_1","name":"shell","input":"{}"}]`, string(request.Input))
}

func TestOpenAIResponsesRequestStripsSyntacticallyValidForeignMessageID(t *testing.T) {
	request := &OpenAIResponsesRequest{Input: json.RawMessage(`[
		{"type":"message","id":"msg_0707148ece7e1dd8016aa8019f172087d1b1d111cf7def5267","role":"assistant","content":[{"type":"output_text","text":"done"}]},
		{"type":"function_call_output","call_id":"call_1","output":"ok"},
		{"type":"compaction_trigger"}
	]`)}

	changed, err := request.NormalizeCodexRemoteCompactionInput()
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `[
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]},
		{"type":"function_call_output","call_id":"call_1","output":"ok"},
		{"type":"compaction_trigger"}
	]`, string(request.Input))
}

func TestNormalizeCodexInputItemIDsRepairsWrongToolPrefixes(t *testing.T) {
	request := &OpenAIResponsesRequest{Input: json.RawMessage(`[
		{"type":"custom_tool_call","id":"fc_call","call_id":"call_1","name":"apply_patch","input":"patch"},
		{"type":"custom_tool_call_output","id":"fco_result","call_id":"call_1","output":"done"},
		{"type":"function_call","id":"fc_valid","call_id":"call_2","name":"read","arguments":"{}"}
	]`)}

	changed, err := request.NormalizeCodexInputItemIDs()

	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `[
		{"type":"custom_tool_call","id":"ctc_fc_call","call_id":"call_1","name":"apply_patch","input":"patch"},
		{"type":"custom_tool_call_output","id":"ctco_fco_result","call_id":"call_1","output":"done"},
		{"type":"function_call","id":"fc_valid","call_id":"call_2","name":"read","arguments":"{}"}
	]`, string(request.Input))
}
