package runtime

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
)

const userRouteHealthKeyPrefix = "user-route\x00"

const autoRouteRequestContextKey = "gateway_auto_route_request"

var userRouteHealthLocks [128]sync.Mutex

func userRouteHealthScope(c *gin.Context) (int, bool) {
	if c == nil {
		return 0, false
	}
	userID := httpctx.GetContextKeyInt(c, constant.ContextKeyUserId)
	return userID, userID > 0
}

// IsAutoRouteRequest reports whether transient route health must be isolated
// to the authenticated user instead of mutating the shared route circuit.
func IsAutoRouteRequest(c *gin.Context) bool {
	if c == nil {
		return false
	}
	if httpctx.GetContextKeyString(c, constant.ContextKeyTokenGroup) == "auto" {
		return true
	}
	if c.GetBool(autoRouteRequestContextKey) {
		return true
	}
	_, unifiedAuto := c.Get(string(constant.ContextKeyUnifiedAutoBindings))
	return unifiedAuto
}

// MarkAutoRouteRequest preserves Auto routing intent after an internal group
// binding replaces the token's original group in the request context.
func MarkAutoRouteRequest(c *gin.Context) {
	if c != nil {
		c.Set(autoRouteRequestContextKey, true)
	}
}

func userChannelHealthKey(c *gin.Context, channelID int, model string, requestTypes ...RequestType) (string, bool) {
	userID, ok := userRouteHealthScope(c)
	if !ok || channelID <= 0 || model == "" {
		return "", false
	}
	return userRouteHealthKey(userID, "channel", strconv.Itoa(channelID), model, requestTypes...), true
}

func userFaultDomainHealthKey(c *gin.Context, domain, model string, requestTypes ...RequestType) (string, bool) {
	userID, ok := userRouteHealthScope(c)
	if !ok || domain == "" || model == "" {
		return "", false
	}
	return userRouteHealthKey(userID, "domain", domain, model, requestTypes...), true
}

func userRouteHealthKey(userID int, kind, target, model string, requestTypes ...RequestType) string {
	return fmt.Sprintf("%s%d\x00%s\x00%s\x00%s\x00%s", userRouteHealthKeyPrefix, userID, kind, target, model, normalizedRequestType(requestTypes...))
}

func userRouteHealthLock(key string) *sync.Mutex {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(key))
	return &userRouteHealthLocks[int(hash.Sum32())%len(userRouteHealthLocks)]
}

// GetUserChannelHealth returns transient channel health isolated to one user.
func GetUserChannelHealth(c *gin.Context, channelID int, model string, requestTypes ...RequestType) (ChannelHealth, bool) {
	key, ok := userChannelHealthKey(c, channelID, model, requestTypes...)
	if !ok {
		return ChannelHealth{}, false
	}
	return getUserRouteHealth(key)
}

// GetUserFaultDomainHealth returns transient provider-domain health isolated to one user.
func GetUserFaultDomainHealth(c *gin.Context, domain, model string, requestTypes ...RequestType) (ChannelHealth, bool) {
	key, ok := userFaultDomainHealthKey(c, domain, model, requestTypes...)
	if !ok {
		return ChannelHealth{}, false
	}
	return getUserRouteHealth(key)
}

func getUserRouteHealth(key string) (ChannelHealth, bool) {
	state, found, err := getChannelHealthCache().Get(key)
	return state, found && err == nil && !userRouteHealthEmpty(state)
}

func userRouteHealthEmpty(state ChannelHealth) bool {
	return state.State == "" && state.ConsecutiveRetryableFailures == 0 && state.RecoveryProbeSuccesses == 0 &&
		state.RecoveryProbeSlots == 0 && state.CoolingUntil.IsZero() && state.RecoveryProbeUntil.IsZero() &&
		state.LastSuccessAt.IsZero() && state.LastFailureAt.IsZero()
}

// RecordUserChannelGatewayFailureForRequest records a fast gateway failure for one user.
func RecordUserChannelGatewayFailureForRequest(c *gin.Context, channelID int, model, requestID string, statusCode int, requestTypes ...RequestType) {
	key, ok := userChannelHealthKey(c, channelID, model, requestTypes...)
	if !ok {
		return
	}
	recordUserRouteFailure(key, channelID, model, requestID, RetryableFailureCooldown(statusCode), channelHealthGatewayFailureThreshold, requestTypes...)
}

// RecordUserChannelRetryableFailureForRequest records a transient channel failure for one user.
func RecordUserChannelRetryableFailureForRequest(c *gin.Context, channelID int, model, requestID string, cooldown time.Duration, requestTypes ...RequestType) {
	key, ok := userChannelHealthKey(c, channelID, model, requestTypes...)
	if !ok {
		return
	}
	recordUserRouteFailure(key, channelID, model, requestID, cooldown, channelHealthRetryableFailureThreshold, requestTypes...)
}

// RecordUserFaultDomainFailure records a transient provider-domain failure for one user.
func RecordUserFaultDomainFailure(c *gin.Context, domain, model, requestID string, cooldown time.Duration, requestTypes ...RequestType) {
	key, ok := userFaultDomainHealthKey(c, domain, model, requestTypes...)
	if !ok {
		return
	}
	recordUserRouteFailure(key, 0, model, requestID, cooldown, faultDomainFailureThreshold, requestTypes...)
}

func recordUserRouteFailure(key string, channelID int, model, requestID string, cooldown time.Duration, threshold int, requestTypes ...RequestType) {
	if cooldown <= 0 {
		cooldown = channelHealthShortCooldown
	}
	lock := userRouteHealthLock(key)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	_ = getChannelHealthCache().UpdateWithTTL(key, channelHealthTTL, func(state ChannelHealth, _ bool) (ChannelHealth, error) {
		if requestID != "" && state.LastFailureRequestID == requestID {
			return state, nil
		}
		state.ChannelID = channelID
		state.Model = model
		state.RequestType = normalizedRequestType(requestTypes...)
		state.LastFailureAt = now
		state.LastFailureRequestID = requestID
		state.ConsecutiveRetryableFailures++
		state.RecoveryProbeSuccesses = 0
		state.RecoveryProbeUntil = time.Time{}
		state.RecoveryProbeSlots = 0
		backoffLevel := retryableFailureBackoffLevel(state.ConsecutiveRetryableFailures, threshold)
		if state.ConsecutiveRetryableFailures < threshold {
			state.State = ChannelHealthDegraded
			state.CoolingUntil = time.Time{}
			return state, nil
		}
		state.State = ChannelHealthCooling
		state.CoolingUntil = now.Add(adaptiveChannelCooldown(cooldown, backoffLevel))
		return state, nil
	})
}

// RecordUserChannelSuccess advances a user channel circuit without changing other users.
func RecordUserChannelSuccess(c *gin.Context, channelID int, model string, ttft time.Duration, requestTypes ...RequestType) {
	key, ok := userChannelHealthKey(c, channelID, model, requestTypes...)
	if !ok {
		return
	}
	recordUserRouteSuccess(key, channelID, model, ttft, requestTypes...)
}

// RecordUserFaultDomainSuccess advances a user provider-domain circuit.
func RecordUserFaultDomainSuccess(c *gin.Context, domain, model string, requestTypes ...RequestType) {
	key, ok := userFaultDomainHealthKey(c, domain, model, requestTypes...)
	if !ok {
		return
	}
	recordUserRouteSuccess(key, 0, model, 0, requestTypes...)
}

func recordUserRouteSuccess(key string, channelID int, model string, ttft time.Duration, requestTypes ...RequestType) {
	lock := userRouteHealthLock(key)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	_ = getChannelHealthCache().UpdateWithTTL(key, channelHealthTTL, func(state ChannelHealth, found bool) (ChannelHealth, error) {
		if !found {
			return state, nil
		}
		state.ChannelID = channelID
		state.Model = model
		state.RequestType = normalizedRequestType(requestTypes...)
		state.LastSuccessAt = now
		if ttft > 0 {
			recordChannelTTFT(&state, float64(ttft.Milliseconds()))
		}
		if state.State == ChannelHealthCooling && state.CoolingUntil.After(now) {
			state.RecoveryProbeUntil = time.Time{}
			state.RecoveryProbeSlots = 0
			return state, nil
		}
		if state.State == ChannelHealthHalfOpen || state.State == ChannelHealthCooling {
			state.RecoveryProbeSuccesses++
			state.RecoveryProbeUntil = time.Time{}
			state.RecoveryProbeSlots = 0
			if state.RecoveryProbeSuccesses < 2 {
				state.State = ChannelHealthCooling
				state.CoolingUntil = now
				return state, nil
			}
			return ChannelHealth{}, nil
		}
		if state.ConsecutiveRetryableFailures > 0 {
			state.ConsecutiveRetryableFailures--
		}
		if state.ConsecutiveRetryableFailures > 0 {
			state.State = ChannelHealthDegraded
			return state, nil
		}
		return ChannelHealth{}, nil
	})
}
