package codex

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/dto"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIResponsesRequestPreservesRemoteCompactionV2Fields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("X-Codex-Beta-Features", "remote_compaction_v2")
	maxOutputTokens := uint(2048)
	temperature := 0.2
	request := dto.OpenAIResponsesRequest{
		Model:           "gpt-5",
		Input:           json.RawMessage(`[{"type":"compaction_trigger"}]`),
		Store:           json.RawMessage("true"),
		PromptCacheKey:  json.RawMessage(`"cache-1"`),
		MaxOutputTokens: &maxOutputTokens,
		Temperature:     &temperature,
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, request)
	require.NoError(t, err)
	result, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.JSONEq(t, "true", string(result.Store))
	require.JSONEq(t, `"cache-1"`, string(result.PromptCacheKey))
	require.Equal(t, &maxOutputTokens, result.MaxOutputTokens)
	require.Equal(t, &temperature, result.Temperature)
}

func TestGetRequestURLAlphaSearch(t *testing.T) {
	url, err := (&Adaptor{}).GetRequestURL(&relaycommon.RelayInfo{
		RelayMode: gatewaycontract.RelayModeAlphaSearch,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://chatgpt.com",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://chatgpt.com/backend-api/codex/alpha/search", url)
}
