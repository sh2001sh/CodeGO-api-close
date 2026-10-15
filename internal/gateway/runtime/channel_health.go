package runtime

import (
	"strconv"
	"sync"
	"time"

	"github.com/samber/hot"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	"github.com/sh2001sh/new-api/internal/platform/cachex"
)

const (
	ChannelHealthHealthy  = "healthy"
	ChannelHealthDegraded = "degraded"
	ChannelHealthCooling  = "cooling"
	ChannelHealthHalfOpen = "half_open"

	channelHealthFailureThreshold          = 5
	channelHealthRetryableFailureThreshold = 3
	channelHealthGatewayFailureThreshold   = 2
	channelHealthShortCooldown             = 15 * time.Second
	channelHealthBadGatewayCooldown        = 8 * time.Second
	channelHealthGatewayTimeoutCooldown    = 15 * time.Second
	channelHealthIncompleteStreamCooldown  = 15 * time.Second
	channelHealthRateLimitCooldown         = 30 * time.Second
	channelHealthLongContextTimeout        = 45 * time.Second
	channelHealthCooldownDuration          = 2 * time.Minute
	channelHealthProbeLeaseDuration        = 10 * time.Second
	channelHealthEmergencyProbeSlots       = 2
	channelHealthTTL                       = 20 * time.Minute
	// Health is advisory routing metadata. A slow or unavailable Redis instance
	// must never hold a model request for the normal cache operation timeout.
	channelHealthReadTimeout       = 150 * time.Millisecond
	channelModelUnavailableTTL     = 5 * time.Minute
	channelModelUpstreamFailureTTL = 2 * time.Minute
	channelHealthShortWindow       = 2 * time.Minute
	channelHealthShortMinRequests  = 5
	channelHealthShortMaxSuccess   = 40.0
	channelHealthTTFTWindow        = 20
)

// ChannelHealth captures the shared routing health for one channel/model pair.
// It deliberately excludes provider credentials and other user-visible details.
type ChannelHealth struct {
	ChannelID                    int                  `json:"channel_id"`
	Model                        string               `json:"model"`
	RequestType                  RequestType          `json:"request_type"`
	State                        string               `json:"state"`
	ConsecutiveRetryableFailures int                  `json:"consecutive_retryable_failures"`
	RecoveryProbeSuccesses       int                  `json:"recovery_probe_successes"`
	RecoveryProbeUntil           time.Time            `json:"recovery_probe_until"`
	RecoveryProbeSlots           int                  `json:"recovery_probe_slots,omitempty"`
	CoolingUntil                 time.Time            `json:"cooling_until"`
	SuccessRate2m                float64              `json:"success_rate_2m"`
	SuccessRate5m                float64              `json:"success_rate_5m"`
	SuccessRate15m               float64              `json:"success_rate_15m"`
	TTFTEWMAMilliseconds         float64              `json:"ttft_ewma_ms"`
	TTFTSamples                  int                  `json:"ttft_samples"`
	TTFTP50Milliseconds          float64              `json:"ttft_p50_ms"`
	TTFTP95Milliseconds          float64              `json:"ttft_p95_ms"`
	CacheHitRate5m               float64              `json:"cache_hit_rate_5m"`
	TTFTRecentMilliseconds       []int64              `json:"ttft_recent_ms"`
	LastSuccessAt                time.Time            `json:"last_success_at"`
	LastFailureAt                time.Time            `json:"last_failure_at"`
	LastFailureRequestID         string               `json:"last_failure_request_id"`
	FaultDomainFailures          []FaultDomainFailure `json:"fault_domain_failures,omitempty"`

	Window2StartedAt  time.Time `json:"window_2_started_at"`
	Window2Requests   int       `json:"window_2_requests"`
	Window2Successes  int       `json:"window_2_successes"`
	Window5StartedAt  time.Time `json:"window_5_started_at"`
	Window5Requests   int       `json:"window_5_requests"`
	Window5Successes  int       `json:"window_5_successes"`
	Window15StartedAt time.Time `json:"window_15_started_at"`
	Window15Requests  int       `json:"window_15_requests"`
	Window15Successes int       `json:"window_15_successes"`
}

// FaultDomainFailure records the last transient failure observed for one
// channel inside a provider fault domain. It is only used for scope detection.
type FaultDomainFailure struct {
	ChannelID  int       `json:"channel_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

var (
	channelHealthCacheOnce sync.Once
	channelHealthCache     *cachex.HybridCache[ChannelHealth]
	channelHealthLocks     [64]sync.Mutex
)

func channelHealthKey(channelID int, model string, requestTypes ...RequestType) string {
	return strconv.Itoa(channelID) + "\x00" + model + "\x00" + string(normalizedRequestType(requestTypes...))
}

func getChannelHealthCache() *cachex.HybridCache[ChannelHealth] {
	channelHealthCacheOnce.Do(func() {
		channelHealthCache = cachex.NewHybridCache[ChannelHealth](cachex.HybridCacheConfig[ChannelHealth]{
			Namespace:  cachex.Namespace("new-api:channel_health:v2"),
			Redis:      platformcache.RDB,
			RedisCodec: cachex.JSONCodec[ChannelHealth]{},
			RedisEnabled: func() bool {
				return platformcache.RedisEnabled && platformcache.RDB != nil
			},
			Memory: func() *hot.HotCache[string, ChannelHealth] {
				return hot.NewHotCache[string, ChannelHealth](hot.LRU, 100_000).
					WithTTL(channelHealthTTL).
					WithJanitor().
					Build()
			},
		})
	})
	return channelHealthCache
}

func channelHealthLock(channelID int) *sync.Mutex {
	return &channelHealthLocks[channelID%len(channelHealthLocks)]
}

// GetChannelHealth returns the last shared health state for a channel/model pair.
func GetChannelHealth(channelID int, model string, requestTypes ...RequestType) (ChannelHealth, bool) {
	if channelID <= 0 || model == "" {
		return ChannelHealth{}, false
	}
	state, found, err := getChannelHealthCache().GetWithTimeout(channelHealthKey(channelID, model, requestTypes...), channelHealthReadTimeout)
	return state, found && err == nil
}

// IsChannelCooling reports whether routing must skip the channel/model pair.
func IsChannelCooling(channelID int, model string, requestTypes ...RequestType) bool {
	state, found := GetChannelHealth(channelID, model, requestTypes...)
	return found && state.CoolingUntil.After(time.Now())
}

// RecordChannelModelUnavailable opens the model circuit only after five
// distinct request IDs fail consecutively. Repeated retries of one request
// count once so a single request cannot exhaust the error budget.
func RecordChannelModelUnavailable(channelID int, model string, requestID string, requestTypes ...RequestType) bool {
	if channelID <= 0 || model == "" {
		return false
	}
	lock := channelHealthLock(channelID)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	cooling := false
	err := getChannelHealthCache().UpdateWithTTL(channelHealthKey(channelID, model, requestTypes...), channelHealthTTL, func(state ChannelHealth, _ bool) (ChannelHealth, error) {
		state.ChannelID = channelID
		state.Model = model
		state.RequestType = normalizedRequestType(requestTypes...)
		state.LastFailureAt = now
		recordChannelHealthWindow(&state, now, false)
		if state.CoolingUntil.After(now) {
			cooling = true
			return state, nil
		}
		if requestID != "" && state.LastFailureRequestID == requestID {
			return state, nil
		}
		state.LastFailureRequestID = requestID
		state.ConsecutiveRetryableFailures++
		state.RecoveryProbeSuccesses = 0
		state.RecoveryProbeSlots = 0
		if state.ConsecutiveRetryableFailures >= channelHealthFailureThreshold {
			state.State = ChannelHealthCooling
			state.CoolingUntil = now.Add(channelModelUnavailableTTL)
			cooling = true
		} else {
			state.State = ChannelHealthDegraded
		}
		return state, nil
	})
	return err == nil && cooling
}

// CoolChannelModelForUpstreamFailure immediately isolates one failing model
// route while leaving the channel available for every other model.
func CoolChannelModelForUpstreamFailure(channelID int, model string, requestTypes ...RequestType) bool {
	if channelID <= 0 || model == "" {
		return false
	}
	lock := channelHealthLock(channelID)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	err := getChannelHealthCache().UpdateWithTTL(channelHealthKey(channelID, model, requestTypes...), channelHealthTTL, func(state ChannelHealth, _ bool) (ChannelHealth, error) {
		state.ChannelID = channelID
		state.Model = model
		state.RequestType = normalizedRequestType(requestTypes...)
		state.State = ChannelHealthCooling
		state.ConsecutiveRetryableFailures = 0
		state.RecoveryProbeSuccesses = 0
		state.RecoveryProbeSlots = 0
		state.CoolingUntil = now.Add(channelModelUpstreamFailureTTL)
		state.LastFailureAt = now
		state.RecoveryProbeSuccesses = 0
		recordChannelHealthWindow(&state, now, false)
		return state, nil
	})
	return err == nil
}

// RecordChannelRetryableFailure advances the shared circuit for a retryable upstream error.
func RecordChannelRetryableFailure(channelID int, model string, requestTypes ...RequestType) {
	RecordChannelRetryableFailureWithCooldown(channelID, model, channelHealthShortCooldown, requestTypes...)
}

// RecordChannelRetryableFailureWithCooldown applies a short model-level
// cooldown for a transient failure. Repeated failures in the rolling window
// still escalate to the longer circuit cooldown.
func RecordChannelRetryableFailureWithCooldown(channelID int, model string, shortCooldown time.Duration, requestTypes ...RequestType) {
	recordChannelRetryableFailure(channelID, model, "", shortCooldown, channelHealthRetryableFailureThreshold, requestTypes...)
}

// RecordChannelRetryableFailureForRequest records at most one transient
// failure for a channel/model pair per downstream request. A gateway retry is
// not independent evidence that the route became unhealthy.
func RecordChannelRetryableFailureForRequest(channelID int, model, requestID string, shortCooldown time.Duration, requestTypes ...RequestType) {
	recordChannelRetryableFailure(channelID, model, requestID, shortCooldown, channelHealthRetryableFailureThreshold, requestTypes...)
}

// RecordChannelGatewayFailure rapidly isolates a model route after two
// consecutive gateway failures. A single transient failure remains degraded so
// healthy traffic can demonstrate that the route is still usable.
func RecordChannelGatewayFailure(channelID int, model string, statusCode int, requestTypes ...RequestType) {
	recordChannelRetryableFailure(channelID, model, "", RetryableFailureCooldown(statusCode), channelHealthGatewayFailureThreshold, requestTypes...)
}

// RecordChannelGatewayFailureForRequest applies gateway-failure health
// accounting once per downstream request while retaining status-specific
// cooldowns for genuinely separate failures.
func RecordChannelGatewayFailureForRequest(channelID int, model, requestID string, statusCode int, requestTypes ...RequestType) {
	recordChannelRetryableFailure(channelID, model, requestID, RetryableFailureCooldown(statusCode), channelHealthGatewayFailureThreshold, requestTypes...)
}

func recordChannelRetryableFailure(channelID int, model, requestID string, shortCooldown time.Duration, failureThreshold int, requestTypes ...RequestType) {
	if channelID <= 0 || model == "" {
		return
	}
	if shortCooldown <= 0 {
		shortCooldown = channelHealthShortCooldown
	}
	if failureThreshold <= 0 {
		failureThreshold = channelHealthRetryableFailureThreshold
	}
	lock := channelHealthLock(channelID)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	_ = getChannelHealthCache().UpdateWithTTL(channelHealthKey(channelID, model, requestTypes...), channelHealthTTL, func(state ChannelHealth, _ bool) (ChannelHealth, error) {
		state.ChannelID = channelID
		state.Model = model
		state.RequestType = normalizedRequestType(requestTypes...)
		if requestID != "" && state.LastFailureRequestID == requestID {
			return state, nil
		}
		state.LastFailureAt = now
		if requestID != "" {
			state.LastFailureRequestID = requestID
		}
		recordChannelHealthWindow(&state, now, false)
		state.ConsecutiveRetryableFailures++
		state.RecoveryProbeSuccesses = 0
		state.RecoveryProbeUntil = time.Time{}
		state.RecoveryProbeSlots = 0
		escalated := shouldCoolForShortTermFailureRate(state) || state.ConsecutiveRetryableFailures >= failureThreshold
		backoffLevel := retryableFailureBackoffLevel(state.ConsecutiveRetryableFailures, failureThreshold)
		if state.CoolingUntil.After(now) && state.State != ChannelHealthHalfOpen {
			if escalated {
				state.CoolingUntil = now.Add(adaptiveChannelCooldown(shortCooldown, backoffLevel))
			}
			return state, nil
		}
		if escalated {
			state.CoolingUntil = now.Add(adaptiveChannelCooldown(shortCooldown, backoffLevel))
			state.State = ChannelHealthCooling
			return state, nil
		}
		state.CoolingUntil = time.Time{}
		state.State = ChannelHealthDegraded
		return state, nil
	})
}

func retryableFailureBackoffLevel(failures int, failureThreshold int) int {
	if failures <= failureThreshold {
		return 1
	}
	return failures - failureThreshold + 1
}

// TryStartChannelRecoveryProbe reserves a single expired circuit for a real
// request. The lease prevents a burst of concurrent callers from reopening it.
func TryStartChannelRecoveryProbe(channelID int, model string, requestTypes ...RequestType) bool {
	return tryStartChannelProbe(channelID, model, 1, true, requestTypes...)
}

// TryStartChannelEmergencyRetryProbe admits a second bounded probe slot for a
// retry after a transient upstream failure. Cooling remains in force for
// normal route selection; only the already-failing request can use this slot.
func TryStartChannelEmergencyRetryProbe(channelID int, model string, requestTypes ...RequestType) bool {
	return tryStartChannelProbe(channelID, model, channelHealthEmergencyProbeSlots, false, requestTypes...)
}

func tryStartChannelProbe(channelID int, model string, maxSlots int, requireExpired bool, requestTypes ...RequestType) bool {
	if channelID <= 0 || model == "" {
		return false
	}
	lock := channelHealthLock(channelID)
	// A recovery probe is optional. Waiting behind a busy health update turns
	// an all-cooling pool into a long request queue, so another request can
	// try again after the current update/probe finishes.
	if !lock.TryLock() {
		return false
	}
	defer lock.Unlock()

	now := time.Now().UTC()
	started := false
	_ = getChannelHealthCache().UpdateWithTTL(channelHealthKey(channelID, model, requestTypes...), channelHealthTTL, func(state ChannelHealth, found bool) (ChannelHealth, error) {
		if !found || (state.State != ChannelHealthCooling && state.State != ChannelHealthHalfOpen) {
			return state, nil
		}
		if requireExpired && state.CoolingUntil.After(now) {
			return state, nil
		}
		if !state.RecoveryProbeUntil.After(now) {
			state.RecoveryProbeSlots = 0
		}
		if state.RecoveryProbeSlots >= maxSlots {
			return state, nil
		}
		state.State = ChannelHealthHalfOpen
		state.RecoveryProbeUntil = now.Add(channelHealthProbeLeaseDuration)
		state.RecoveryProbeSlots++
		started = true
		return state, nil
	})
	return started
}

// ReleaseChannelProbe returns a slot when a paired fault-domain probe could
// not be acquired. It prevents partial selection from consuming the retry
// budget until the lease expires.
func ReleaseChannelProbe(channelID int, model string, requestTypes ...RequestType) {
	if channelID <= 0 || model == "" {
		return
	}
	lock := channelHealthLock(channelID)
	lock.Lock()
	defer lock.Unlock()
	_ = getChannelHealthCache().UpdateWithTTL(channelHealthKey(channelID, model, requestTypes...), channelHealthTTL, func(state ChannelHealth, found bool) (ChannelHealth, error) {
		if !found || state.RecoveryProbeSlots <= 0 {
			return state, nil
		}
		state.RecoveryProbeSlots--
		if state.RecoveryProbeSlots == 0 {
			state.RecoveryProbeUntil = time.Time{}
		}
		return state, nil
	})
}

func adaptiveChannelCooldown(base time.Duration, failures int) time.Duration {
	if base <= 0 {
		base = channelHealthShortCooldown
	}
	for failures > 1 && base < channelHealthCooldownDuration {
		base *= 2
		failures--
	}
	if base > channelHealthCooldownDuration {
		return channelHealthCooldownDuration
	}
	return base
}

// RetryableFailureCooldown selects a short circuit duration without exposing
// channel-specific rules. Gateway timeouts need a brief pause, while rate
// limits still recover more slowly than transient upstream failures.
func RetryableFailureCooldown(statusCode int) time.Duration {
	switch statusCode {
	case 429:
		return channelHealthRateLimitCooldown
	case 504, 524:
		return channelHealthGatewayTimeoutCooldown
	case 502:
		return channelHealthBadGatewayCooldown
	default:
		return channelHealthShortCooldown
	}
}

// IncompleteStreamCooldown gives a transient stream failure a short pause before
// it becomes eligible for a protected half-open recovery request.
func IncompleteStreamCooldown() time.Duration {
	return channelHealthIncompleteStreamCooldown
}

// LongContextTimeoutCooldown avoids repeated long-context header timeouts while
// still permitting a prompt half-open recovery attempt.
func LongContextTimeoutCooldown() time.Duration {
	return channelHealthLongContextTimeout
}

func shouldCoolForShortTermFailureRate(state ChannelHealth) bool {
	if state.Window2Requests < channelHealthShortMinRequests {
		return false
	}
	failures := state.Window2Requests - state.Window2Successes
	return failures >= 3 && state.SuccessRate2m <= channelHealthShortMaxSuccess
}
