package http

import (
	"errors"
	"github.com/gin-gonic/gin"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformtext "github.com/sh2001sh/new-api/internal/platform/textx"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

type relayErrorEnvelope struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func TestFinalizeRelayErrorMasksChineseUpstreamQuotaLeak(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	apiErr := types.NewOpenAIError(
		errors.New("用户额度不足, 剩余额度: -＄0.038392 (request id: upstream)"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	finalizeRelayError(ctx, types.RelayFormatOpenAI, nil, apiErr, "downstream")

	var response relayErrorEnvelope
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, platformtext.UpstreamQuotaGenericMessage, response.Error.Message)
}

func TestFinalizeRelayErrorKeepsLocalQuotaMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	apiErr := types.NewErrorWithStatusCode(
		errors.New("用户额度不足, 剩余额度: ＄0.002290"),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusForbidden,
	)

	finalizeRelayError(ctx, types.RelayFormatOpenAI, nil, apiErr, "downstream")

	var response relayErrorEnvelope
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &response))
	require.Contains(t, response.Error.Message, "用户额度不足, 剩余额度: ＄0.002290")
	require.NotEqual(t, platformtext.UpstreamQuotaGenericMessage, response.Error.Message)
}

func TestRelayNotImplementedReturnsOpenAIStyleError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/files", nil)

	RelayNotImplemented(ctx)

	require.Equal(t, http.StatusNotImplemented, recorder.Code)
	var response relayErrorEnvelope
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "API not implemented", response.Error.Message)
	require.Equal(t, "new_api_error", response.Error.Type)
	require.Equal(t, "api_not_implemented", response.Error.Code)
}

func TestRelayNotFoundIncludesMethodAndPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/unknown", nil)

	RelayNotFound(ctx)

	require.Equal(t, http.StatusNotFound, recorder.Code)
	var response relayErrorEnvelope
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "Invalid URL (GET /v1/unknown)", response.Error.Message)
	require.Equal(t, "invalid_request_error", response.Error.Type)
}

func TestPlaygroundRejectsAccessToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/chat/completions", nil)
	ctx.Set("use_access_token", true)

	Playground(ctx)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	var response relayErrorEnvelope
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "暂不支持使用 access token", response.Error.Message)
}

func TestPlaygroundImageRejectsAccessToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
	ctx.Set("use_access_token", true)

	PlaygroundImage(ctx)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	var response relayErrorEnvelope
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "暂不支持使用 access token", response.Error.Message)
}
