package runtime

import (
	"fmt"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
	"github.com/sh2001sh/new-api/constant"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	"github.com/sh2001sh/new-api/internal/platform/cachex"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
)

const (
	userStreamFailureThreshold = 3
	userStreamFailureWindow    = 90 * time.Second
	userStreamFailureCooldown  = 15 * time.Second
	userStreamFailureTTL       = 5 * time.Minute
)

const ginKeyUserStreamFailureCircuit = "user_stream_failure_circuit"

// UserStreamFailureCircuit is a user-scoped guard for repeated incomplete streams.
// Its fingerprint is always a one-way hash and never stores request content.
type UserStreamFailureCircuit struct {
	Fingerprint         string    `json:"fingerprint"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	CoolingUntil        time.Time `json:"cooling_until"`
	LastFailureAt       time.Time `json:"last_failure_at"`
	LastFailureRequest  string    `json:"last_failure_request"`
}

// UserStreamFailureCircuitAudit contains the non-sensitive state recorded with an error log.
type UserStreamFailureCircuitAudit struct {
	Fingerprint         string `json:"fingerprint"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	Opened              bool   `json:"opened"`
	RetryAfterSeconds   int    `json:"retry_after_seconds,omitempty"`
}

var (
	userStreamFailureCacheOnce sync.Once
	userStreamFailureCache     *cachex.HybridCache[UserStreamFailureCircuit]
	userStreamFailureLocks     [64]sync.Mutex
)

func getUserStreamFailureCache() *cachex.HybridCache[UserStreamFailureCircuit] {
	userStreamFailureCacheOnce.Do(func() {
		userStreamFailureCache = cachex.NewHybridCache[UserStreamFailureCircuit](cachex.HybridCacheConfig[UserStreamFailureCircuit]{
			Namespace:  cachex.Namespace("new-api:user_stream_failure:v1"),
			Redis:      platformcache.RDB,
			RedisCodec: cachex.JSONCodec[UserStreamFailureCircuit]{},
			RedisEnabled: func() bool {
				return platformcache.RedisEnabled && platformcache.RDB != nil
			},
			Memory: func() *hot.HotCache[string, UserStreamFailureCircuit] {
				return hot.NewHotCache[string, UserStreamFailureCircuit](hot.LRU, 100_000).
					WithTTL(userStreamFailureTTL).
					WithJanitor().
					Build()
			},
		})
	})
	return userStreamFailureCache
}

func userStreamFailureLock(userID int) *sync.Mutex {
	if userID < 0 {
		userID = -userID
	}
	return &userStreamFailureLocks[userID%len(userStreamFailureLocks)]
}

func userStreamFailureFingerprint(c *gin.Context) string {
	if meta, ok := getChannelAffinityMeta(c); ok && meta.KeyFingerprint != "" {
		return "affinity-" + meta.KeyFingerprint
	}

	storage, err := platformhttpx.GetBodyStorage(c)
	if err != nil {
		return ""
	}
	body, err := storage.Bytes()
	if err != nil || len(body) == 0 {
		return ""
	}
	hash := platformsecurity.Sha1(body)
	if len(hash) > 16 {
		hash = hash[:16]
	}
	return "body-" + hash
}

func userStreamFailureKey(c *gin.Context, modelName string) (string, int, string, bool) {
	if c == nil || modelName == "" {
		return "", 0, "", false
	}
	userID := c.GetInt("id")
	if userID <= 0 {
		return "", 0, "", false
	}
	fingerprint := userStreamFailureFingerprint(c)
	if fingerprint == "" {
		return "", 0, "", false
	}
	path := ""
	if c.Request != nil && c.Request.URL != nil {
		path = c.Request.URL.Path
	}
	return fmt.Sprintf("%d:%s:%s:%s", userID, modelName, path, fingerprint), userID, fingerprint, true
}

func userStreamFailureRetryAfter(coolingUntil time.Time, now time.Time) int {
	remaining := coolingUntil.Sub(now)
	if remaining <= 0 {
		return 0
	}
	seconds := int(remaining.Round(time.Second).Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
}

// UserStreamFailureRetryAfter returns a temporary retry delay for repeated, incomplete streams.
func UserStreamFailureRetryAfter(c *gin.Context, modelName string) (int, bool) {
	key, _, _, ok := userStreamFailureKey(c, modelName)
	if !ok {
		return 0, false
	}
	state, found, err := getUserStreamFailureCache().Get(key)
	if err != nil || !found || !state.CoolingUntil.After(time.Now().UTC()) {
		return 0, false
	}
	return userStreamFailureRetryAfter(state.CoolingUntil, time.Now().UTC()), true
}

// RecordUserIncompleteStreamFailure updates the circuit after an upstream stream misses its terminal event.
func RecordUserIncompleteStreamFailure(c *gin.Context, modelName string) UserStreamFailureCircuitAudit {
	key, userID, fingerprint, ok := userStreamFailureKey(c, modelName)
	if !ok {
		return UserStreamFailureCircuitAudit{}
	}

	lock := userStreamFailureLock(userID)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	requestID := c.GetString(constant.RequestIdKey)
	result := UserStreamFailureCircuitAudit{Fingerprint: fingerprint}
	_ = getUserStreamFailureCache().UpdateWithTTL(key, userStreamFailureTTL, func(state UserStreamFailureCircuit, _ bool) (UserStreamFailureCircuit, error) {
		state.Fingerprint = fingerprint
		if state.CoolingUntil.After(now) {
			result.ConsecutiveFailures = state.ConsecutiveFailures
			result.RetryAfterSeconds = userStreamFailureRetryAfter(state.CoolingUntil, now)
			return state, nil
		}
		if requestID != "" && state.LastFailureRequest == requestID {
			result.ConsecutiveFailures = state.ConsecutiveFailures
			return state, nil
		}
		if state.LastFailureAt.IsZero() || now.Sub(state.LastFailureAt) > userStreamFailureWindow {
			state.ConsecutiveFailures = 0
		}
		state.LastFailureAt = now
		state.LastFailureRequest = requestID
		state.ConsecutiveFailures++
		result.ConsecutiveFailures = state.ConsecutiveFailures
		if state.ConsecutiveFailures >= userStreamFailureThreshold {
			state.CoolingUntil = now.Add(userStreamFailureCooldown)
			result.Opened = true
			result.RetryAfterSeconds = int(userStreamFailureCooldown.Seconds())
			state.ConsecutiveFailures = 0
		}
		return state, nil
	})
	if c != nil && result.Fingerprint != "" {
		c.Set(ginKeyUserStreamFailureCircuit, result)
	}
	return result
}

// ClearUserStreamFailureCircuit removes failure history after a completed stream.
func ClearUserStreamFailureCircuit(c *gin.Context, modelName string) {
	key, _, _, ok := userStreamFailureKey(c, modelName)
	if !ok {
		return
	}
	_, _ = getUserStreamFailureCache().DeleteMany([]string{key})
}

// UserStreamFailureCircuitAuditFromContext returns guard diagnostics for internal error logs.
func UserStreamFailureCircuitAuditFromContext(c *gin.Context) (UserStreamFailureCircuitAudit, bool) {
	if c == nil {
		return UserStreamFailureCircuitAudit{}, false
	}
	value, ok := c.Get(ginKeyUserStreamFailureCircuit)
	if !ok {
		return UserStreamFailureCircuitAudit{}, false
	}
	audit, ok := value.(UserStreamFailureCircuitAudit)
	return audit, ok && audit.Fingerprint != ""
}

func resetUserStreamFailureCircuitForTest() error {
	if userStreamFailureCache != nil {
		if err := userStreamFailureCache.Purge(); err != nil {
			return err
		}
	}
	userStreamFailureCacheOnce = sync.Once{}
	userStreamFailureCache = nil
	return nil
}
