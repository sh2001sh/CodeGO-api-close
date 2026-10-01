package auxiliary

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func failure(status int, code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: status, Type: "api_error", Code: code, Message: message}
}

func writeError(w http.ResponseWriter, err error) {
	var upstream *gateway.UpstreamError
	if !errors.As(err, &upstream) {
		upstream = failure(502, "upstream_failure", "upstream request failed")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(upstream.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"type": upstream.Type, "code": upstream.Code, "message": upstream.Message}})
}

func classify(status int) gateway.AttemptResult {
	result := gateway.AttemptResult{Status: status, Err: failure(status, "invalid_request", http.StatusText(status)), Scope: gateway.ScopeRequest}
	switch {
	case status == 429:
		result.Retryable, result.Scope = true, gateway.ScopeCredential
		result.Err.Code = "upstream_rate_limited"
	case status == 401 || status == 403 || status == 402:
		result.Retryable, result.Scope = true, gateway.ScopeCredential
		result.Err = failure(502, "upstream_auth_failed", "upstream authentication failed")
	case status == 404:
		result.Retryable, result.Scope = true, gateway.ScopeModel
		result.Err = failure(502, "model_unavailable", "upstream model unavailable")
	case status >= 500:
		result.Retryable, result.Scope = true, gateway.ScopeCredential
		result.Err = failure(502, "upstream_unavailable", "upstream temporarily unavailable")
	}
	return result
}

func contextOutcome(ctx context.Context, delivered bool, usage gateway.Usage, err *gateway.UpstreamError) gateway.Outcome {
	observation := gateway.Observation{Delivered: delivered, Estimate: usage, Err: err, ClientCanceled: clientGone(ctx) || errors.Is(ctx.Err(), context.Canceled), TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded), Empty: err != nil && err.Code == "empty_response"}
	if delivered || hasUsage(usage) {
		observation.Usage = &usage
	}
	return gateway.Decide(observation)
}

func hasUsage(u gateway.Usage) bool {
	return u.PromptTokens > 0 || u.CompletionTokens > 0 || u.ImageCount > 0 || u.AudioCharacters > 0 || u.AudioDurationMicros > 0 || len(u.ToolCalls) > 0
}

func retryAfter(header http.Header) time.Duration {
	value := header.Get("Retry-After")
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if stamp, err := http.ParseTime(value); err == nil {
		return max(time.Until(stamp), 0)
	}
	return 0
}

func copyHeaders(dst, src http.Header) {
	for _, name := range []string{"Content-Type", "Content-Disposition", "Cache-Control", "Retry-After", "X-Codex-Turn-State"} {
		if value := src.Get(name); value != "" {
			dst.Set(name, value)
		}
	}
}

func clientAddress(r *http.Request) string {
	address, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return address
}
