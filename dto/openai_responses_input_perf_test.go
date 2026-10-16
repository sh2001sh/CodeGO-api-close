package dto

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesNormalizationEscapedMarkers(t *testing.T) {
	cases := []struct {
		name, input, want string
		normalize         func(*OpenAIResponsesRequest) (bool, error)
	}{
		{"delegation", `[{"type":"function_call_\u006futput","namespace":"codex_app","name":"create_thread","output":"<codex_delegation>work</codex_delegation>"}]`, `[{"type":"message","role":"user","content":[{"type":"input_text","text":"<codex_delegation>work</codex_delegation>"}]}]`, (*OpenAIResponsesRequest).NormalizeCodexDelegationBootstrap},
		{"agent", `[{"type":"agent_\u006dessage","content":"work"}]`, `[{"type":"message","role":"user","content":"work"}]`, (*OpenAIResponsesRequest).NormalizeCodexAgentMessages},
		{"metadata", `[{"type":"message","\u0061uthor":"agent","phase":"final","content":"work"}]`, `[{"type":"message","content":"work"}]`, (*OpenAIResponsesRequest).StripCodexMessageMetadata},
		{"additional tools", `[{"type":"additional_\u0074ools","tools":[{"type":"function","name":"work"}]}]`, `[]`, (*OpenAIResponsesRequest).LiftCodexAdditionalTools},
		{"item id", `[{"type":"message","\u0069d":"msg_old","content":"work"}]`, `[{"type":"message","content":"work"}]`, (*OpenAIResponsesRequest).NormalizeCodexRemoteCompactionInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &OpenAIResponsesRequest{Input: json.RawMessage(tc.input)}
			changed, err := tc.normalize(r)
			require.NoError(t, err)
			require.True(t, changed)
			require.JSONEq(t, tc.want, string(r.Input))
			if tc.name == "additional tools" {
				require.JSONEq(t, `[{"type":"function","name":"work"}]`, string(r.Tools))
			}
		})
	}
}

func TestResponsesNormalizationPreservesUnchangedInputAndValidation(t *testing.T) {
	input := json.RawMessage("[ {\"role\":\"user\", \"content\":\"a \\\"phase\\\" and \\\"id\\\" in code\"} ]")
	r := &OpenAIResponsesRequest{Input: input}
	for _, normalize := range []func(*OpenAIResponsesRequest) (bool, error){
		(*OpenAIResponsesRequest).NormalizeCodexDelegationBootstrap,
		(*OpenAIResponsesRequest).NormalizeCodexAgentMessages,
		(*OpenAIResponsesRequest).LiftCodexAdditionalTools,
		(*OpenAIResponsesRequest).StripCodexMessageMetadata,
		(*OpenAIResponsesRequest).NormalizeCodexRemoteCompactionInput,
	} {
		changed, err := normalize(r)
		require.NoError(t, err)
		require.False(t, changed)
		require.Equal(t, input, r.Input)
	}
	// Absent additional_tools must not hide invalid top-level declarations.
	r.Tools = json.RawMessage(`{"invalid":"tools must be an array"}`)
	_, err := r.LiftCodexAdditionalTools()
	require.Error(t, err)
	// Existing string-input handling does not inspect top-level tools.
	r.Input = json.RawMessage(`"ordinary text"`)
	_, err = r.LiftCodexAdditionalTools()
	require.NoError(t, err)
}

func BenchmarkResponsesPortableInput(b *testing.B) {
	input, err := json.Marshal([]map[string]any{{"role": "user", "content": []map[string]string{{"type": "input_text", "text": strings.Repeat("context 42 / request; ", 40000)}}}})
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(input)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := &OpenAIResponsesRequest{Input: input}
		for _, normalize := range []func(*OpenAIResponsesRequest) (bool, error){
			(*OpenAIResponsesRequest).NormalizeCodexDelegationBootstrap,
			(*OpenAIResponsesRequest).NormalizeCodexAgentMessages,
			(*OpenAIResponsesRequest).LiftCodexAdditionalTools,
			(*OpenAIResponsesRequest).StripCodexMessageMetadata,
			(*OpenAIResponsesRequest).NormalizeCodexRemoteCompactionInput,
		} {
			if changed, err := normalize(r); err != nil || changed {
				b.Fatalf("unexpected rewrite: %v / %v", changed, err)
			}
		}
	}
}
