package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestLongContextGPTRequestClassification(t *testing.T) {
	require.False(t, IsLongContextGPTRequest("gpt-5.6-sol", LongContextPromptTokenThreshold-1))
	require.True(t, IsLongContextGPTRequest("gpt-5.6-sol", LongContextPromptTokenThreshold))
	require.True(t, IsLongContextGPTRequest(" GPT-5.6-SOL ", VeryLongContextPromptTokens))
	require.False(t, IsLongContextGPTRequest("claude-opus", VeryLongContextPromptTokens))
}

func TestResponsesHistoryContinuationUsesLongContextPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request := &dto.OpenAIResponsesRequest{PreviousResponseID: "resp_previous"}

	require.True(t, IsResponsesHistoryContinuation(request))
	MarkLongContextRequestWithContinuation(context, "gpt-5.6-sol", 1_000, IsResponsesHistoryContinuation(request))
	require.True(t, IsLongContextRequest(context))
}

func TestNewResponsesRequestDoesNotUseContinuationPolicy(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{}
	require.False(t, IsResponsesHistoryContinuation(request))
}

func TestMarkLongContextRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	MarkLongContextRequest(context, "gpt-5.6-sol", LongContextPromptTokenThreshold)
	require.True(t, IsLongContextRequest(context))

	MarkLongContextRequest(context, "gpt-5.6-sol", 100)
	require.False(t, IsLongContextRequest(context))
}

func TestConversationPromptHighWaterClassifiesResponsesDeltaAsLongContext(t *testing.T) {
	require.NoError(t, ResetConversationPromptHighWaterForTest())
	t.Cleanup(func() { require.NoError(t, ResetConversationPromptHighWaterForTest()) })

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	setChannelAffinityContext(context, channelAffinityMeta{
		RuleName:       "codex_prompt_cache",
		UsingGroup:     "default",
		KeyFingerprint: "session-fingerprint",
		TTLSeconds:     600,
	})

	ObserveConversationPromptHighWaterFromContext(context, "gpt-5.6-sol", &dto.Usage{PromptTokens: LongContextPromptTokenThreshold + 1})
	MarkLongContextRequest(context, "gpt-5.6-sol", 1_000)

	require.True(t, IsLongContextRequest(context))
	oldNormal := constant.StreamingMaxDuration
	oldLong := constant.StreamingLongContextMaxDuration
	constant.StreamingMaxDuration = 240
	constant.StreamingLongContextMaxDuration = 540
	t.Cleanup(func() {
		constant.StreamingMaxDuration = oldNormal
		constant.StreamingLongContextMaxDuration = oldLong
	})
	require.Equal(t, 540*time.Second, StreamMaxDurationForRequest(context, "gpt-5.6-sol", 1_000))
}

func TestConversationPromptHighWaterKeepsSmallGPTConversationAtNormalDuration(t *testing.T) {
	require.NoError(t, ResetConversationPromptHighWaterForTest())
	t.Cleanup(func() { require.NoError(t, ResetConversationPromptHighWaterForTest()) })

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	setChannelAffinityContext(context, channelAffinityMeta{
		RuleName:       "codex_prompt_cache",
		UsingGroup:     "default",
		KeyFingerprint: "small-session-fingerprint",
		TTLSeconds:     600,
	})

	ObserveConversationPromptHighWaterFromContext(context, "gpt-5.6-sol", &dto.Usage{PromptTokens: LongContextPromptTokenThreshold - 1})
	MarkLongContextRequest(context, "gpt-5.6-sol", 1_000)

	require.False(t, IsLongContextRequest(context))
}
