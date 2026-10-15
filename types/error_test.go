package types

import (
	"fmt"
	"net/http"
	"testing"

	platformtext "github.com/sh2001sh/new-api/internal/platform/textx"
	"github.com/stretchr/testify/require"
)

func TestNewAPIErrorStatusStringsSanitizeUpstreamQuotaLeak(t *testing.T) {
	t.Parallel()

	apiErr := NewOpenAIError(
		fmt.Errorf("预扣费额度失败, 用户剩余额度: ＄0.002290, 需要预扣费额度: ＄0.005418 (request id: abc)"),
		ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	require.Equal(t, platformtext.UpstreamQuotaGenericMessage, apiErr.ErrorWithStatusCode())
	require.Equal(t, platformtext.UpstreamQuotaGenericMessage, apiErr.MaskSensitiveErrorWithStatusCode())
}

func TestNewAPIErrorStatusStringsSanitizeChineseUpstreamQuotaLeak(t *testing.T) {
	t.Parallel()

	apiErr := NewOpenAIError(
		fmt.Errorf("用户额度不足, 剩余额度: ＄-0.054062 (request id: abc)"),
		ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	require.Equal(t, platformtext.UpstreamQuotaGenericMessage, apiErr.ToOpenAIError().Message)
	require.Equal(t, platformtext.UpstreamQuotaGenericMessage, apiErr.MaskSensitiveErrorWithStatusCode())
}

func TestNewAPIErrorSanitizesEnglishUpstreamBalanceLeak(t *testing.T) {
	t.Parallel()

	apiErr := NewOpenAIError(
		fmt.Errorf("insufficient balance: remaining $0.00, required $3.97"),
		ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	apiErr.SanitizeDownstreamResponse()
	require.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	require.Equal(t, ModelUnavailableMessage, apiErr.ToOpenAIError().Message)
	require.Equal(t, ModelUnavailableMessage, apiErr.MaskSensitiveErrorWithStatusCode())
}

func TestNewAPIErrorMaskSensitiveErrorHidesUpstreamAvailabilityDetails(t *testing.T) {
	t.Parallel()

	apiErr := NewOpenAIError(
		fmt.Errorf("No available channel for model gpt-5.6-luna under group plus高不稳定分组 (request id: upstream)"),
		ErrorCodeBadResponseStatusCode,
		http.StatusServiceUnavailable,
	)

	require.Equal(t, ModelUnavailableMessage, apiErr.MaskSensitiveErrorWithStatusCode())
}

func TestRemoteForbiddenResponseIsSanitizedForDownstream(t *testing.T) {
	t.Parallel()

	apiErr := NewOpenAIError(
		fmt.Errorf("Your request was blocked."),
		ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)
	apiErr.SanitizeDownstreamResponse()

	require.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	require.Equal(t, ErrorCodeGetChannelFailed, apiErr.GetErrorCode())
	require.Equal(t, ModelUnavailableMessage, apiErr.ToOpenAIError().Message)
}

func TestRemoteGatewayFailureIsSanitizedForDownstream(t *testing.T) {
	t.Parallel()

	apiErr := NewOpenAIError(
		fmt.Errorf("responses stream closed before response.completed (upstream request id: hidden)"),
		ErrorCodeBadResponse,
		http.StatusBadGateway,
	)
	apiErr.SanitizeDownstreamResponse()

	require.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	require.Equal(t, ErrorCodeGetChannelFailed, apiErr.GetErrorCode())
	require.Equal(t, ModelUnavailableMessage, apiErr.ToOpenAIError().Message)
}

func TestNewAPIErrorStatusStringsKeepLocalQuotaMessage(t *testing.T) {
	t.Parallel()

	apiErr := NewErrorWithStatusCode(
		fmt.Errorf("用户额度不足, 剩余额度: ＄0.002290"),
		ErrorCodeInsufficientUserQuota,
		http.StatusForbidden,
	)

	require.Equal(t, "status_code=403, 用户额度不足, 剩余额度: ＄0.002290", apiErr.ErrorWithStatusCode())
	require.Equal(t, "status_code=403, 用户额度不足, 剩余额度: ＄0.002290", apiErr.MaskSensitiveErrorWithStatusCode())
}

func TestOwnerVisibleErrorPreservesDiagnosticsAndRedactsCredentials(t *testing.T) {
	t.Parallel()

	apiErr := NewErrorWithStatusCode(
		fmt.Errorf("upstream balance exhausted authorization: Bearer live-secret api_key=sk-1234567890abcdef"),
		ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	message := apiErr.OwnerVisibleErrorWithStatusCode()
	require.Contains(t, message, "status_code=403")
	require.Contains(t, message, "upstream balance exhausted")
	require.NotContains(t, message, "live-secret")
	require.NotContains(t, message, "1234567890abcdef")
}
