package runtime

import (
	"sync"
	"time"
)

// RecordChannelSuccess requires two successful half-open probes before restoring
// normal traffic. Normal healthy routes continue to recover immediately.
func RecordChannelSuccess(channelID int, model string, ttft time.Duration, requestTypes ...RequestType) {
	if channelID <= 0 || model == "" {
		return
	}
	lock := channelHealthLock(channelID)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	_ = getChannelHealthCache().UpdateWithTTL(channelHealthKey(channelID, model, requestTypes...), channelHealthTTL, func(state ChannelHealth, _ bool) (ChannelHealth, error) {
		state.ChannelID = channelID
		state.Model = model
		state.RequestType = normalizedRequestType(requestTypes...)
		state.LastSuccessAt = now
		recordChannelHealthWindow(&state, now, true)
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
		}
		state.ConsecutiveRetryableFailures = 0
		state.RecoveryProbeSuccesses = 0
		state.RecoveryProbeSlots = 0
		state.State = ChannelHealthHealthy
		state.CoolingUntil = time.Time{}
		return state, nil
	})
}

// RecordChannelCacheObservation updates the short-window cache hit ratio used
// by weighted route pools.
func RecordChannelCacheObservation(channelID int, model string, promptTokens, cachedTokens int, requestTypes ...RequestType) {
	if channelID <= 0 || model == "" || promptTokens <= 0 {
		return
	}
	if cachedTokens < 0 {
		cachedTokens = 0
	}
	if cachedTokens > promptTokens {
		cachedTokens = promptTokens
	}
	lock := channelHealthLock(channelID)
	lock.Lock()
	defer lock.Unlock()
	_ = getChannelHealthCache().UpdateWithTTL(channelHealthKey(channelID, model, requestTypes...), channelHealthTTL, func(state ChannelHealth, _ bool) (ChannelHealth, error) {
		rate := float64(cachedTokens) / float64(promptTokens) * 100
		if state.CacheHitRate5m <= 0 {
			state.CacheHitRate5m = rate
		} else {
			state.CacheHitRate5m = state.CacheHitRate5m*0.8 + rate*0.2
		}
		return state, nil
	})
}

func resetChannelHealthForTest() error {
	if channelHealthCache != nil {
		if err := channelHealthCache.Purge(); err != nil {
			return err
		}
	}
	channelHealthCacheOnce = sync.Once{}
	channelHealthCache = nil
	return nil
}
