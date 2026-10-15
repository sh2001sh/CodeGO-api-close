package app

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
)

const (
	autoGroupPendingThreshold          = 2
	autoGroupPendingShortFirstByteWait = 30 * time.Second
	autoGroupPendingLongFirstByteWait  = 60 * time.Second
	autoGroupPendingRetention          = 3 * time.Minute
)

const autoGroupPendingContextKey = "auto_group_pending_attempt"

type autoGroupPendingAttempt struct {
	RequestID string    `json:"request_id"`
	StartedAt time.Time `json:"started_at"`
}

type autoGroupPendingRef struct {
	Key       string
	RequestID string
}

// BeginAutoGroupAttempt records an upstream attempt until its first response
// path finishes. The state is scoped by user, model, request type, and group.
func BeginAutoGroupAttempt(c *gin.Context, model string) {
	beginAutoGroupAttemptAt(c, selectedAutoGroup(c), model, time.Now())
}

func beginAutoGroupAttemptAt(c *gin.Context, group, model string, now time.Time) {
	scope, ok := autoGroupScopeFromContext(c)
	if !ok || group == "" || model == "" {
		return
	}
	requestID := c.GetString(constant.RequestIdKey)
	if requestID == "" {
		return
	}
	key := autoGroupCircuitKey(scope, group, model)
	lock := autoGroupCircuitLock(key)
	lock.Lock()
	defer lock.Unlock()

	err := getAutoGroupCircuitCache().UpdateWithTTL(key, autoGroupCircuitTTL, func(state autoGroupCircuit, _ bool) (autoGroupCircuit, error) {
		state.PendingAttempts = pruneAutoGroupPending(state.PendingAttempts, now)
		for index := range state.PendingAttempts {
			if state.PendingAttempts[index].RequestID == requestID {
				state.PendingAttempts[index].StartedAt = now
				return state, nil
			}
		}
		state.PendingAttempts = append(state.PendingAttempts, autoGroupPendingAttempt{RequestID: requestID, StartedAt: now})
		return state, nil
	})
	if err != nil {
		logAutoGroupCacheError(c, "record pending Auto-group attempt", err)
		return
	}
	c.Set(autoGroupPendingContextKey, autoGroupPendingRef{Key: key, RequestID: requestID})
}

// EndAutoGroupAttempt removes the request from pending-first-byte accounting.
func EndAutoGroupAttempt(c *gin.Context) {
	endAutoGroupAttemptAt(c, time.Now())
}

func endAutoGroupAttemptAt(c *gin.Context, now time.Time) {
	if c == nil {
		return
	}
	value, found := c.Get(autoGroupPendingContextKey)
	ref, ok := value.(autoGroupPendingRef)
	if !found || !ok || ref.Key == "" || ref.RequestID == "" {
		return
	}
	lock := autoGroupCircuitLock(ref.Key)
	lock.Lock()
	defer lock.Unlock()

	err := getAutoGroupCircuitCache().UpdateWithTTL(ref.Key, autoGroupCircuitTTL, func(state autoGroupCircuit, found bool) (autoGroupCircuit, error) {
		if !found {
			return state, nil
		}
		pending := pruneAutoGroupPending(state.PendingAttempts, now)
		filtered := pending[:0]
		for _, attempt := range pending {
			if attempt.RequestID != ref.RequestID {
				filtered = append(filtered, attempt)
			}
		}
		state.PendingAttempts = filtered
		return state, nil
	})
	if err != nil {
		logAutoGroupCacheError(c, "clear pending Auto-group attempt", err)
	}
	c.Set(autoGroupPendingContextKey, autoGroupPendingRef{})
}

func autoGroupHasStalledPending(state autoGroupCircuit, requestType gatewayruntime.RequestType, now time.Time) bool {
	wait := autoGroupPendingShortFirstByteWait
	if requestType == gatewayruntime.RequestTypeChatLongStream {
		wait = autoGroupPendingLongFirstByteWait
	}
	stalled := 0
	for _, attempt := range state.PendingAttempts {
		age := now.Sub(attempt.StartedAt)
		if !attempt.StartedAt.IsZero() && age >= wait && age <= autoGroupPendingRetention {
			stalled++
			if stalled >= autoGroupPendingThreshold {
				return true
			}
		}
	}
	return false
}

func pruneAutoGroupPending(pending []autoGroupPendingAttempt, now time.Time) []autoGroupPendingAttempt {
	cutoff := now.Add(-autoGroupPendingRetention)
	filtered := pending[:0]
	for _, attempt := range pending {
		if !attempt.StartedAt.Before(cutoff) {
			filtered = append(filtered, attempt)
		}
	}
	return filtered
}
