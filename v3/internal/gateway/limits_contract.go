package gateway

import (
	"context"
	"errors"
)

// LeaseController admits one upstream attempt and frees its concurrency slots.
// Implementations count RPM once per Request.ID, including across retries.
type LeaseController interface {
	Acquire(context.Context, *Request, Target) error
	Release(context.Context, *Request, Target) error
}

// AuthFailureController bounds repeated failed key lookups by address.
type AuthFailureController interface {
	Blocked(address string) bool
	Failed(address string)
}

var (
	ErrRateLimited       = errors.New("gateway: user request limit reached")
	ErrTargetBusy        = errors.New("gateway: target concurrency limit reached")
	ErrLimitsUnavailable = errors.New("gateway: request limits temporarily unavailable")
)
