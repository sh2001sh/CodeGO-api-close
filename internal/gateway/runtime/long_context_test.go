package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLongContextGPTRequestClassification(t *testing.T) {
	require.False(t, IsLongContextGPTRequest("gpt-5.6-sol", LongContextPromptTokenThreshold-1))
	require.True(t, IsLongContextGPTRequest("gpt-5.6-sol", LongContextPromptTokenThreshold))
	require.True(t, IsLongContextGPTRequest(" GPT-5.6-SOL ", VeryLongContextPromptTokens))
	require.False(t, IsLongContextGPTRequest("claude-opus", VeryLongContextPromptTokens))
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
