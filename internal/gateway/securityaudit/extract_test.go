package securityaudit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractSnapshotScansAllClientControlledRoles(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"system text"},{"role":"assistant","content":"assistant text"},{"role":"tool","content":"tool text"},{"role":"user","content":"user text"}]}`)
	snapshot, err := ExtractSnapshot(Request{Protocol: "openai_chat", Body: body}, false)
	require.NoError(t, err)
	require.Contains(t, snapshot.ScanText, "system text")
	require.Contains(t, snapshot.ScanText, "assistant text")
	require.Contains(t, snapshot.ScanText, "tool text")
	require.Contains(t, snapshot.ScanText, "user text")
	require.Equal(t, 4, snapshot.MessageCount)
}

func TestExtractSnapshotNormalizesUnicodeAndZeroWidth(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"ｉｇｎｏｒｅ\u200b rules"}]}`)
	snapshot, err := ExtractSnapshot(Request{Protocol: "openai_chat", Body: body}, false)
	require.NoError(t, err)
	require.Equal(t, "ignore rules", snapshot.ScanText)
}

func TestLatestTurnSnapshotKeepsPreviousAssistantOnly(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"global rules"},{"role":"user","content":"old"},{"role":"assistant","content":"previous output"},{"role":"developer","content":"developer rules"},{"role":"user","content":"latest"}],"tools":[{"type":"function","function":{"name":"lookup","description":"tool rules"}}]}`)
	snapshot, err := ExtractSnapshot(Request{Protocol: "openai_chat", Body: body}, true)
	require.NoError(t, err)
	require.Contains(t, snapshot.ScanText, "latest")
	require.Contains(t, snapshot.ScanText, "previous output")
	require.Contains(t, snapshot.ScanText, "global rules")
	require.Contains(t, snapshot.ScanText, "developer rules")
	require.Contains(t, snapshot.ScanText, "tool rules")
	require.NotContains(t, snapshot.ScanText, "old")
}

func TestExtractProtocolControlledInstructionsAndTools(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     string
		want     []string
	}{
		{
			name: "responses", protocol: "openai_responses",
			body: `{"instructions":"response rules","input":"user input","tools":[{"type":"function","name":"search","description":"response tool"}]}`,
			want: []string{"response rules", "user input", "response tool"},
		},
		{
			name: "claude", protocol: "claude_messages",
			body: `{"system":"claude rules","messages":[{"role":"user","content":[{"type":"text","text":"claude input"}]}],"tools":[{"name":"shell","description":"claude tool"}]}`,
			want: []string{"claude rules", "claude input", "claude tool"},
		},
		{
			name: "gemini", protocol: "gemini_generate_content",
			body: `{"systemInstruction":{"parts":[{"text":"gemini rules"}]},"contents":[{"role":"user","parts":[{"text":"gemini input"},{"inlineData":{"mimeType":"image/png","data":"BASE64_SECRET"}}]}],"tools":[{"functionDeclarations":[{"name":"lookup","description":"gemini tool","parameters":{"description":"parameter rules"}}]}]}`,
			want: []string{"gemini rules", "gemini input", "gemini tool", "parameter rules"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot, err := ExtractSnapshot(Request{Protocol: test.protocol, Body: []byte(test.body)}, false)
			require.NoError(t, err)
			for _, expected := range test.want {
				require.Contains(t, snapshot.ScanText, expected)
			}
			require.NotContains(t, snapshot.ScanText, "BASE64_SECRET")
		})
	}
}

func TestPromptPreviewRedactsCredentialsAndPII(t *testing.T) {
	prompt := "Bearer auth-secret-123456 api_key=key-secret-123456 email person@example.com phone +86 138 0013 8000"
	snapshot, err := ExtractSnapshot(Request{FallbackText: prompt}, false)
	require.NoError(t, err)
	require.NotContains(t, snapshot.RedactedPreview, "auth-secret")
	require.NotContains(t, snapshot.RedactedPreview, "key-secret")
	require.NotContains(t, snapshot.RedactedPreview, "person@example.com")
	require.NotContains(t, snapshot.RedactedPreview, "138 0013 8000")
	require.Contains(t, snapshot.RedactedPreview, "[redacted]")
	require.Equal(t, prompt, snapshot.ScanText)
}

func TestExtractRealtimeFrameScansInstructionsAndToolDescriptions(t *testing.T) {
	body := []byte(`{"type":"session.update","session":{"instructions":"system rules","tools":[{"name":"shell","description":"runs commands"}]}}`)
	snapshot, err := ExtractSnapshot(Request{Protocol: "openai_realtime", Body: body}, false)
	require.NoError(t, err)
	require.Contains(t, snapshot.ScanText, "system rules")
	require.Contains(t, snapshot.ScanText, "runs commands")
}
