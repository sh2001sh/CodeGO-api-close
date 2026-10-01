package gateway

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/metrics"
)

func (g *Gateway) acquireAttempt(ctx context.Context, req *Request, target Target) error {
	if g.limits == nil {
		return nil
	}
	err := g.limits.Acquire(ctx, req, target)
	if err != nil && !errors.Is(err, ErrTargetBusy) && !errors.Is(err, ErrRateLimited) && !errors.Is(err, ErrLimitsUnavailable) {
		g.log.Error("acquire concurrency lease failed", "request_id", req.ID, "err", err)
	}
	req.Timeline.Mark(metrics.StageLimits, time.Now())
	return err
}

func (g *Gateway) releaseAttempt(req *Request, target Target) {
	if g.limits == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), g.cfg.FinalizeTimeout)
	defer cancel()
	if err := g.limits.Release(ctx, req, target); err != nil {
		if errors.Is(err, ErrLimitsUnavailable) {
			g.log.Warn("concurrency lease will expire after Redis outage", "request_id", req.ID, "err", err)
		} else {
			g.log.Error("release concurrency lease failed", "request_id", req.ID, "err", err)
		}
	}
}

func limitFailure(err error) AttemptResult {
	if errors.Is(err, ErrTargetBusy) {
		return AttemptResult{Retryable: true, Scope: ScopeNone, Err: &UpstreamError{
			Status: http.StatusTooManyRequests, Type: "rate_limit_error", Code: "channel_concurrency_limited",
			Message: "channel concurrency limit reached"}}
	}
	if errors.Is(err, ErrRateLimited) {
		return AttemptResult{Err: &UpstreamError{Status: http.StatusTooManyRequests,
			Type: "rate_limit_error", Code: "user_rate_limited", Message: "user request limit reached"}}
	}
	return AttemptResult{Err: &UpstreamError{Status: http.StatusServiceUnavailable,
		Type: "api_error", Code: "limits_unavailable", Message: "request limits are temporarily unavailable"}}
}

// Resolve a forwarded address only through a configured trusted proxy chain.
// Direct callers cannot reset an IP limit by supplying arbitrary headers.
func (g *Gateway) clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !g.trustedProxy(peer) {
		return host
	}
	forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	if len(forwarded) == 1 && strings.TrimSpace(forwarded[0]) == "" {
		forwarded[0] = r.Header.Get("X-Real-IP")
	}
	for i := len(forwarded) - 1; i >= 0; i-- {
		address, err := netip.ParseAddr(strings.TrimSpace(forwarded[i]))
		if err != nil {
			return host
		}
		if !g.trustedProxy(address) || i == 0 {
			return address.Unmap().String()
		}
	}
	return host
}

func (g *Gateway) trustedProxy(address netip.Addr) bool {
	for _, prefix := range g.cfg.TrustedProxies {
		if prefix.Contains(address.Unmap()) {
			return true
		}
	}
	return false
}

var errAuthLimited = &clientError{http.StatusTooManyRequests, "rate_limit_error", "auth_rate_limited", "too many failed authentication attempts"}
