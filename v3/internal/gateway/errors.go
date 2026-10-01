package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Scope says what an upstream failure implicates (plan §3, from CLIProxyAPI).
type Scope uint8

const (
	ScopeNone       Scope = iota
	ScopeRequest          // the request itself is bad: stop, do not fail over
	ScopeCredential       // this credential is unusable now: fail over, cool it down
	ScopeModel            // this credential cannot serve this model: fail over, cool the pair
)

// AttemptResult is the typed outcome of one upstream try.
type AttemptResult struct {
	OK        bool
	Retryable bool // another candidate may succeed
	Scope     Scope
	Status    int // upstream HTTP status, 0 for transport errors
	Err       *UpstreamError
	// Measured per upstream attempt; estimates and full relay duration never
	// enter scored routing health observations.
	TTFT         time.Duration
	PromptTokens int64
	CachedTokens int64
	// RetryAfter is the upstream's own cooldown hint (Retry-After header),
	// zero if absent. The scheduler prefers it over its backoff curve.
	RetryAfter time.Duration
}

// parseRetryAfter reads a Retry-After header in seconds or HTTP-date form.
func parseRetryAfter(v string, now time.Time) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return 0
}

// UpstreamError is an upstream failure in a client-safe form.
type UpstreamError struct {
	Status  int    // HTTP status to report when headers are not sent yet
	Type    string // OpenAI-style error type
	Code    string
	Message string
	Body    []byte // raw upstream body for ScopeRequest errors, passed through
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream %d %s: %s", e.Status, e.Code, e.Message)
}

// classifyStatus turns a non-2xx upstream response into an AttemptResult.
func classifyStatus(status int, body []byte) AttemptResult {
	err := &UpstreamError{Status: status, Type: "upstream_error", Message: http.StatusText(status), Body: body}
	res := AttemptResult{Status: status, Err: err}
	switch {
	case status == http.StatusTooManyRequests:
		res.Retryable, res.Scope = true, ScopeCredential
		err.Type, err.Code = "rate_limit_error", "rate_limited"
	case status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusPaymentRequired:
		res.Retryable, res.Scope = true, ScopeCredential
		err.Code = "upstream_auth_failed"
		err.Status = http.StatusBadGateway // the caller's key is fine; ours is not
		err.Body = nil                     // never leak our credential's error details
	case status == http.StatusNotFound:
		res.Retryable, res.Scope = true, ScopeModel
		err.Code = "model_unavailable"
		err.Body = nil
	case status >= 500:
		res.Retryable, res.Scope = true, ScopeCredential
		err.Code = "upstream_unavailable"
		err.Status = http.StatusBadGateway
		err.Body = nil
	default: // other 4xx: the request is invalid for any upstream
		res.Scope = ScopeRequest
		err.Code = "invalid_request"
	}
	return res
}

func transportFailure(err error) AttemptResult {
	status, code := http.StatusBadGateway, "upstream_unreachable"
	if errors.Is(err, errHeaderTimeout) || errors.Is(err, context.DeadlineExceeded) {
		status, code = http.StatusGatewayTimeout, "upstream_timeout"
	}
	return AttemptResult{
		Retryable: true,
		Scope:     ScopeCredential,
		Err:       &UpstreamError{Status: status, Type: "upstream_error", Code: code, Message: "upstream request failed"},
	}
}

func streamFailure(err *UpstreamError) AttemptResult {
	return AttemptResult{Retryable: true, Scope: ScopeCredential, Status: http.StatusOK, Err: err}
}

var errHeaderTimeout = errors.New("gateway: upstream header timeout")

// clientError is a pipeline failure before any upstream call.
type clientError struct {
	status  int
	typ     string
	code    string
	message string
}

func (e *clientError) Error() string { return e.message }

var (
	errMissingKey   = &clientError{http.StatusUnauthorized, "authentication_error", "missing_api_key", "missing API key"}
	errInvalidKey   = &clientError{http.StatusUnauthorized, "authentication_error", "invalid_api_key", "invalid API key"}
	errBadBody      = &clientError{http.StatusBadRequest, "invalid_request_error", "invalid_body", "request body must be a JSON object with a model"}
	errBodyTooLarge = &clientError{http.StatusRequestEntityTooLarge, "invalid_request_error", "body_too_large", "request body too large"}
	errNoRoute      = &clientError{http.StatusServiceUnavailable, "upstream_error", "no_available_channel", "no channel available for this model"}
)

// ErrInvalidKey is returned by Authorizers for unknown or disabled keys.
var ErrInvalidKey = errors.New("gateway: invalid api key")

// ErrAuthUnavailable is returned by Authorizers when the key cannot be
// checked right now (backing store overloaded or down). It maps to 503, not
// 401, so clients retry instead of treating their key as revoked.
var ErrAuthUnavailable = errors.New("gateway: authorization temporarily unavailable")

var errAuthDown = &clientError{http.StatusServiceUnavailable, "api_error", "auth_unavailable", "authorization is temporarily unavailable"}

// ErrInsufficientCredits is returned by Settlers when a reservation cannot be made.
var ErrInsufficientCredits = errors.New("gateway: insufficient credits")

// ErrBillingUnavailable is returned by Settlers that refuse a request because
// billing is degraded (for example a Redis outage). It is an expected state,
// reported as 503 without an error log per request.
var ErrBillingUnavailable = errors.New("gateway: billing temporarily unavailable")
