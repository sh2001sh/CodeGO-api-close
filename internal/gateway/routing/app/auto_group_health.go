package app

import (
	"fmt"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
	"github.com/sh2001sh/new-api/constant"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	"github.com/sh2001sh/new-api/internal/platform/cachex"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformhttpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
)

const (
	autoGroupFailureThreshold        = 3
	autoGroupFailureWindow           = 2 * time.Minute
	autoGroupCooldownBase            = 15 * time.Second
	autoGroupCooldownMax             = 2 * time.Minute
	autoGroupRecoveryProbeLease      = 10 * time.Second
	autoGroupRecoverySuccessRequired = 2
	autoGroupCircuitTTL              = 20 * time.Minute
)

const autoGroupRecoveryProbeContextKey = "auto_group_recovery_probe"

type autoGroupCircuit struct {
	ConsecutiveFailures    int                       `json:"consecutive_failures"`
	RecoveryProbeSuccesses int                       `json:"recovery_probe_successes"`
	BackoffLevel           int                       `json:"backoff_level"`
	CoolingUntil           time.Time                 `json:"cooling_until"`
	RecoveryProbeUntil     time.Time                 `json:"recovery_probe_until"`
	LastFailureAt          time.Time                 `json:"last_failure_at"`
	LastFailureRequestID   string                    `json:"last_failure_request_id"`
	PendingAttempts        []autoGroupPendingAttempt `json:"pending_attempts,omitempty"`
}

type autoGroupHealthScope struct {
	UserID      int
	RequestType gatewayruntime.RequestType
}

var (
	autoGroupCircuitCacheOnce sync.Once
	autoGroupCircuitCache     *cachex.HybridCache[autoGroupCircuit]
	autoGroupCircuitLocks     [64]sync.Mutex
)

func autoGroupScopeFromContext(c *gin.Context) (autoGroupHealthScope, bool) {
	if c == nil {
		return autoGroupHealthScope{}, false
	}
	userID := platformhttpctx.GetContextKeyInt(c, constant.ContextKeyUserId)
	if userID <= 0 {
		return autoGroupHealthScope{}, false
	}
	return autoGroupHealthScope{
		UserID:      userID,
		RequestType: gatewayruntime.RequestTypeFromContext(c),
	}, true
}

func autoGroupCircuitKey(scope autoGroupHealthScope, group, model string) string {
	return fmt.Sprintf("%d\x00%s\x00%s\x00%s", scope.UserID, scope.RequestType, group, model)
}

func getAutoGroupCircuitCache() *cachex.HybridCache[autoGroupCircuit] {
	autoGroupCircuitCacheOnce.Do(func() {
		autoGroupCircuitCache = cachex.NewHybridCache[autoGroupCircuit](cachex.HybridCacheConfig[autoGroupCircuit]{
			Namespace:  cachex.Namespace("new-api:auto_group_user_circuit:v1"),
			Redis:      platformcache.RDB,
			RedisCodec: cachex.JSONCodec[autoGroupCircuit]{},
			RedisEnabled: func() bool {
				return platformcache.RedisEnabled && platformcache.RDB != nil
			},
			Memory: func() *hot.HotCache[string, autoGroupCircuit] {
				return hot.NewHotCache[string, autoGroupCircuit](hot.LRU, 100_000).
					WithTTL(autoGroupCircuitTTL).
					WithJanitor().
					Build()
			},
		})
	})
	return autoGroupCircuitCache
}

func autoGroupCircuitLock(key string) *sync.Mutex {
	index := 0
	for _, char := range key {
		index = (index*31 + int(char)) % len(autoGroupCircuitLocks)
	}
	return &autoGroupCircuitLocks[index]
}

func getAutoGroupCircuit(c *gin.Context, group, model string) (autoGroupCircuit, bool) {
	scope, ok := autoGroupScopeFromContext(c)
	if !ok || group == "" || model == "" {
		return autoGroupCircuit{}, false
	}
	state, found, err := getAutoGroupCircuitCache().Get(autoGroupCircuitKey(scope, group, model))
	if err != nil {
		logger.LogError(c, "read user Auto-group health failed: "+err.Error())
		return autoGroupCircuit{}, false
	}
	return state, found && !autoGroupCircuitEmpty(state)
}

func autoGroupCircuitEmpty(state autoGroupCircuit) bool {
	return state.ConsecutiveFailures == 0 && state.RecoveryProbeSuccesses == 0 && state.BackoffLevel == 0 &&
		state.CoolingUntil.IsZero() && state.RecoveryProbeUntil.IsZero() && state.LastFailureAt.IsZero() &&
		state.LastFailureRequestID == "" && len(state.PendingAttempts) == 0
}

func isAutoGroupCooling(c *gin.Context, group, model string, now time.Time) bool {
	scope, scoped := autoGroupScopeFromContext(c)
	state, found := getAutoGroupCircuit(c, group, model)
	if !scoped || !found {
		return false
	}
	return state.CoolingUntil.After(now) || state.RecoveryProbeUntil.After(now) ||
		autoGroupHasStalledPending(state, scope.RequestType, now)
}

func autoGroupNeedsRecoveryProbe(c *gin.Context, group, model string, now time.Time) bool {
	state, found := getAutoGroupCircuit(c, group, model)
	return found && !state.CoolingUntil.IsZero() && !state.CoolingUntil.After(now) &&
		!state.RecoveryProbeUntil.After(now)
}

func tryStartAutoGroupRecoveryProbe(c *gin.Context, group, model string, now time.Time) bool {
	scope, ok := autoGroupScopeFromContext(c)
	if !ok || group == "" || model == "" {
		return false
	}
	if reserved := c.GetString(autoGroupRecoveryProbeContextKey); reserved != "" {
		return reserved == group
	}
	key := autoGroupCircuitKey(scope, group, model)
	lock := autoGroupCircuitLock(key)
	if !lock.TryLock() {
		return false
	}
	defer lock.Unlock()

	started := false
	err := getAutoGroupCircuitCache().UpdateWithTTL(key, autoGroupCircuitTTL, func(state autoGroupCircuit, found bool) (autoGroupCircuit, error) {
		started = false
		if !found || state.CoolingUntil.IsZero() || state.CoolingUntil.After(now) || state.RecoveryProbeUntil.After(now) {
			return state, nil
		}
		state.RecoveryProbeUntil = now.Add(autoGroupRecoveryProbeLease)
		started = true
		return state, nil
	})
	if err != nil {
		logger.LogError(c, "reserve user Auto-group recovery probe failed: "+err.Error())
		return false
	}
	if started && c != nil {
		c.Set(autoGroupRecoveryProbeContextKey, group)
	}
	return started
}

func selectedAutoGroupIsRecoveryProbe(c *gin.Context, group string) bool {
	return c != nil && group != "" && c.GetString(autoGroupRecoveryProbeContextKey) == group
}

func recordAutoGroupSuccess(c *gin.Context, group, model string, now time.Time) {
	scope, ok := autoGroupScopeFromContext(c)
	if !ok || group == "" || model == "" {
		return
	}
	key := autoGroupCircuitKey(scope, group, model)
	lock := autoGroupCircuitLock(key)
	lock.Lock()
	defer lock.Unlock()

	probe := selectedAutoGroupIsRecoveryProbe(c, group)
	err := getAutoGroupCircuitCache().UpdateWithTTL(key, autoGroupCircuitTTL, func(state autoGroupCircuit, found bool) (autoGroupCircuit, error) {
		if !found {
			return state, nil
		}
		if probe {
			state.RecoveryProbeUntil = time.Time{}
			state.RecoveryProbeSuccesses++
			if state.RecoveryProbeSuccesses >= autoGroupRecoverySuccessRequired {
				return autoGroupCircuit{}, nil
			}
			state.CoolingUntil = now
			return state, nil
		}
		if state.CoolingUntil.After(now) {
			return state, nil
		}
		if state.ConsecutiveFailures > 0 {
			state.ConsecutiveFailures--
		}
		if state.ConsecutiveFailures == 0 && state.CoolingUntil.IsZero() {
			return autoGroupCircuit{}, nil
		}
		return state, nil
	})
	if err != nil {
		logger.LogError(c, "record user Auto-group success failed: "+err.Error())
	}
}

func recordAutoGroupFailure(c *gin.Context, group, model string, now time.Time) {
	scope, ok := autoGroupScopeFromContext(c)
	if !ok || group == "" || model == "" {
		return
	}
	key := autoGroupCircuitKey(scope, group, model)
	lock := autoGroupCircuitLock(key)
	lock.Lock()
	defer lock.Unlock()

	requestID := c.GetString(constant.RequestIdKey)
	probe := selectedAutoGroupIsRecoveryProbe(c, group)
	err := getAutoGroupCircuitCache().UpdateWithTTL(key, autoGroupCircuitTTL, func(state autoGroupCircuit, _ bool) (autoGroupCircuit, error) {
		if requestID != "" && state.LastFailureRequestID == requestID {
			return state, nil
		}
		if state.LastFailureAt.IsZero() || now.Sub(state.LastFailureAt) > autoGroupFailureWindow {
			state.ConsecutiveFailures = 0
		}
		state.LastFailureAt = now
		state.LastFailureRequestID = requestID
		state.RecoveryProbeUntil = time.Time{}
		state.RecoveryProbeSuccesses = 0
		state.ConsecutiveFailures++
		if probe || state.ConsecutiveFailures >= autoGroupFailureThreshold {
			state.BackoffLevel++
			state.CoolingUntil = now.Add(autoGroupCooldown(state.BackoffLevel))
			state.ConsecutiveFailures = 0
		}
		return state, nil
	})
	if err != nil {
		logger.LogError(c, "record user Auto-group failure failed: "+err.Error())
	}
}

func autoGroupCooldown(level int) time.Duration {
	if level < 1 {
		level = 1
	}
	duration := autoGroupCooldownBase
	for level > 1 && duration < autoGroupCooldownMax {
		duration *= 2
		level--
	}
	if duration > autoGroupCooldownMax {
		return autoGroupCooldownMax
	}
	return duration
}

func logAutoGroupCacheError(c *gin.Context, action string, err error) {
	if err != nil {
		logger.LogError(c, action+" failed: "+err.Error())
	}
}

func resetAutoGroupCircuitCacheForTest() error {
	if autoGroupCircuitCache != nil {
		if err := autoGroupCircuitCache.Purge(); err != nil {
			return err
		}
	}
	autoGroupCircuitCacheOnce = sync.Once{}
	autoGroupCircuitCache = nil
	return nil
}
