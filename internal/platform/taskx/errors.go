package taskx

import (
	"fmt"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/dto"
	platformtext "github.com/sh2001sh/new-api/internal/platform/textx"
	"github.com/sh2001sh/new-api/types"
)

func TaskErrorWrapper(err error, code string, statusCode int) *dto.TaskError {
	return taskErrorWrapper(err, code, statusCode, true)
}

func taskErrorWrapper(err error, code string, statusCode int, sanitizeUpstreamQuota bool) *dto.TaskError {
	text := err.Error()
	if statusCode == http.StatusTooManyRequests {
		text = "status_code=429"
	}
	lowerText := strings.ToLower(text)
	if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
		platformobservability.SysLog(fmt.Sprintf("error: %s", text))
		text = platformtext.MaskSensitiveInfo(text)
	}
	if sanitizeUpstreamQuota {
		text = platformtext.SanitizeUpstreamQuotaErrorMessage(text)
	}
	return &dto.TaskError{
		Code:       code,
		Message:    text,
		StatusCode: statusCode,
		Error:      err,
	}
}

func TaskErrorWrapperLocal(err error, code string, statusCode int) *dto.TaskError {
	taskErr := taskErrorWrapper(err, code, statusCode, false)
	taskErr.LocalError = true
	return taskErr
}

func TaskErrorFromAPIError(apiErr *types.NewAPIError) *dto.TaskError {
	if apiErr == nil {
		return nil
	}
	message := apiErr.Err.Error()
	isUpstream := types.IsRemoteProviderError(apiErr)
	if isUpstream {
		message = platformtext.SanitizeUpstreamQuotaErrorMessage(message)
	}
	if apiErr.StatusCode == http.StatusTooManyRequests {
		message = "status_code=429"
	}
	return &dto.TaskError{
		Code:       string(apiErr.GetErrorCode()),
		Message:    message,
		StatusCode: apiErr.StatusCode,
		LocalError: !isUpstream,
		Error:      apiErr.Err,
	}
}
