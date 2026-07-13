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
		fmt.Errorf("用户额度不足, 剩余额度: -＄0.038392 (request id: abc)"),
		ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	require.Equal(t, platformtext.UpstreamQuotaGenericMessage, apiErr.ToOpenAIError().Message)
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
