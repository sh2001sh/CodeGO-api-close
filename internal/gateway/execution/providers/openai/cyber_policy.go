package openai

import (
	"strings"

	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/types"
)

func cyberPolicyAPIError(payload []byte, statusCode int, contentType string) *types.NewAPIError {
	cyber, ok := platformhttpx.DetectCyberPolicyError(payload)
	if !ok {
		return nil
	}
	message := strings.TrimSpace(cyber.Message)
	if message == "" {
		message = "This content was flagged for possible cybersecurity risk"
	}
	errorType := strings.TrimSpace(cyber.Type)
	if errorType == "" {
		errorType = "invalid_request_error"
	}
	err := types.WithOpenAIError(types.OpenAIError{
		Message: message,
		Type:    errorType,
		Code:    types.ErrorCodeCyberPolicy,
	}, statusCode, types.ErrOptionWithSkipRetry())
	err.SetRawResponse(payload, contentType)
	return err
}
