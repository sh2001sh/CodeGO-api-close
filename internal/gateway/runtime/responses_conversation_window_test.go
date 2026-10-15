package runtime

import (
	"encoding/json"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestResponsesConversationWindowSendsOnlyNewSuffix(t *testing.T) {
	previousWindow := constant.ResponsesConversationWindowSeconds
	constant.ResponsesConversationWindowSeconds = 600
	t.Cleanup(func() { constant.ResponsesConversationWindowSeconds = previousWindow })
	if responsesConversationCache != nil {
		require.NoError(t, responsesConversationCache.Purge())
	}

	info := &RelayInfo{UserId: 7, OriginModelName: "gpt-5.6-sol", ChannelMeta: &ChannelMeta{ChannelId: 94, ChannelMultiKeyIndex: 2}}
	firstBody := []byte(`{"model":"gpt-5.6-sol","store":true,"prompt_cache_key":"turn-1","input":[{"role":"user","content":"hello"}]}`)
	firstContext, _ := gin.CreateTestContext(nil)
	optimized, fallback, applied, err := PrepareResponsesConversationWindow(firstContext, info, firstBody)
	require.NoError(t, err)
	require.False(t, applied)
	require.Nil(t, fallback)
	require.Equal(t, firstBody, optimized)

	RecordResponsesConversationWindow(firstContext, info, []byte(`{"type":"response.completed","response":{"id":"resp_1","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"world"}]}]}}`))

	secondBody := []byte(`{"model":"gpt-5.6-sol","store":true,"prompt_cache_key":"turn-1","input":[{"role":"user","content":"hello"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"world"}]},{"role":"user","content":"continue"}]}`)
	secondContext, _ := gin.CreateTestContext(nil)
	optimized, fallback, applied, err = PrepareResponsesConversationWindow(secondContext, info, secondBody)
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, secondBody, fallback)

	var payload struct {
		PreviousResponseID string            `json:"previous_response_id"`
		Input              []json.RawMessage `json:"input"`
	}
	require.NoError(t, json.Unmarshal(optimized, &payload))
	require.Equal(t, "resp_1", payload.PreviousResponseID)
	require.Len(t, payload.Input, 1)
	require.JSONEq(t, `{"role":"user","content":"continue"}`, string(payload.Input[0]))
}

func TestResponsesConversationWindowRequiresStoredConversation(t *testing.T) {
	previousWindow := constant.ResponsesConversationWindowSeconds
	constant.ResponsesConversationWindowSeconds = 600
	t.Cleanup(func() { constant.ResponsesConversationWindowSeconds = previousWindow })
	info := &RelayInfo{UserId: 7, OriginModelName: "gpt-5.6-sol", ChannelMeta: &ChannelMeta{ChannelId: 94}}
	context, _ := gin.CreateTestContext(nil)
	body := []byte(`{"model":"gpt-5.6-sol","prompt_cache_key":"turn-1","input":[{"role":"user","content":"hello"}]}`)

	optimized, _, applied, err := PrepareResponsesConversationWindow(context, info, body)
	require.NoError(t, err)
	require.False(t, applied)
	require.Equal(t, body, optimized)
}

func TestResponsesConversationWindowCollectsStreamOutputItems(t *testing.T) {
	previousWindow := constant.ResponsesConversationWindowSeconds
	constant.ResponsesConversationWindowSeconds = 600
	t.Cleanup(func() { constant.ResponsesConversationWindowSeconds = previousWindow })
	if responsesConversationCache != nil {
		require.NoError(t, responsesConversationCache.Purge())
	}
	info := &RelayInfo{UserId: 8, OriginModelName: "gpt-5.6-sol", ChannelMeta: &ChannelMeta{ChannelId: 94}}
	firstBody := []byte(`{"model":"gpt-5.6-sol","store":true,"prompt_cache_key":"stream-turn","input":[{"role":"user","content":"hello"}]}`)
	firstContext, _ := gin.CreateTestContext(nil)
	_, _, _, err := PrepareResponsesConversationWindow(firstContext, info, firstBody)
	require.NoError(t, err)
	RecordResponsesConversationWindow(firstContext, info, []byte(`{"type":"response.created","response":{"id":"resp_stream"}}`))
	RecordResponsesConversationWindow(firstContext, info, []byte(`{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"world"}]}}`))
	RecordResponsesConversationWindow(firstContext, info, []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":1}}}`))

	secondBody := []byte(`{"model":"gpt-5.6-sol","store":true,"prompt_cache_key":"stream-turn","input":[{"role":"user","content":"hello"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"world"}]},{"role":"user","content":"continue"}]}`)
	secondContext, _ := gin.CreateTestContext(nil)
	optimized, _, applied, err := PrepareResponsesConversationWindow(secondContext, info, secondBody)
	require.NoError(t, err)
	require.True(t, applied)
	require.Contains(t, string(optimized), `"previous_response_id":"resp_stream"`)
}
