package tencent

import (
	"encoding/json"
	"errors"
	"math"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func parseUsage(native *nativeUsage) (*gateway.Usage, error) {
	if native == nil {
		return nil, nil
	}
	for _, value := range []*int64{native.PromptTokens, native.CompletionTokens, native.TotalTokens} {
		if value != nil && *value < 0 {
			return nil, errors.New("tencent: invalid token usage")
		}
	}
	if native.PromptTokens == nil || native.CompletionTokens == nil {
		return nil, nil
	}
	if *native.PromptTokens > math.MaxInt64-*native.CompletionTokens {
		return nil, errors.New("tencent: token usage overflow")
	}
	return &gateway.Usage{PromptTokens: *native.PromptTokens, CompletionTokens: *native.CompletionTokens}, nil
}

func responseError(native *nativeError) *gateway.UpstreamError {
	if native == nil {
		return nil
	}
	code := string(native.Code)
	if len(native.Code) > 0 && native.Code[0] == '"' {
		if json.Unmarshal(native.Code, &code) != nil {
			return upstream("invalid_response", "Tencent returned an invalid error code")
		}
	}
	if code == "0" || code == "" || code == "null" {
		if native.Message == "" {
			return nil
		}
		code = "upstream_error"
	}
	// Signed authentication failures may reflect credentials or authorization
	// headers in Message. Preserve the stable error code, never reflected text.
	return upstream(code, "Tencent request failed")
}
