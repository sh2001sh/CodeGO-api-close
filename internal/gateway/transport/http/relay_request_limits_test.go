package http

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/stretchr/testify/require"
)

func TestResponsesLongRequestPreservesFieldsAndReplay(t *testing.T) {
	prompt := strings.Repeat("long context 中文 ", 40000)
	body, err := json.Marshal(map[string]any{
		"model": "gpt-6.1-sol", "input": prompt,
		"previous_response_id": "resp_previous", "service_tier": "priority",
		"reasoning":         map[string]string{"effort": "high", "summary": "auto"},
		"client_metadata":   map[string]string{"opaque": "preserved"},
		"max_output_tokens": 1484,
	})
	require.NoError(t, err)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(string(body)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { platformhttpx.CleanupBodyStorage(ctx) })
	snapshot, err := platformhttpx.GetRequestBodySnapshot(ctx)
	require.NoError(t, err)
	request, err := getAndValidateResponsesRequest(ctx)
	require.NoError(t, err)
	require.Equal(t, "gpt-6.1-sol", snapshot.Model)
	require.Equal(t, "resp_previous", snapshot.PreviousResponseID)
	require.Equal(t, snapshot.Model, request.Model)
	require.Equal(t, snapshot.PreviousResponseID, request.PreviousResponseID)
	require.Equal(t, "priority", request.ServiceTier)
	require.Equal(t, "high", request.Reasoning.Effort)
	require.Equal(t, "auto", request.Reasoning.Summary)
	require.JSONEq(t, `{"opaque":"preserved"}`, string(request.ClientMetadata))
	require.Equal(t, uint(1484), *request.MaxOutputTokens)
	var input string
	require.NoError(t, json.Unmarshal(request.Input, &input))
	require.Equal(t, prompt, input)
	replayed, err := io.ReadAll(ctx.Request.Body)
	require.NoError(t, err)
	require.Equal(t, body, replayed)
}

func TestResponsesLongRequestRejectsMalformedTail(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(
		`{"model":"gpt-6.1-sol","input":"`+strings.Repeat("context ", 40000)+`"} trailing`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { platformhttpx.CleanupBodyStorage(ctx) })
	_, err := getAndValidateResponsesRequest(ctx)
	require.Error(t, err)
}

func TestMarketplaceUserConcurrencyLimitHonorsUserBypass(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.Equal(t, 3, marketplaceUserConcurrencyLimit(ctx, 3))

	httpctx.SetContextKey(ctx, constant.ContextKeyBypassModelRequestLimits, true)
	require.Zero(t, marketplaceUserConcurrencyLimit(ctx, 3))
}
