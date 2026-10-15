package runtime

import (
	"time"

	"github.com/gin-gonic/gin"
)

// TryStartUserChannelRecoveryProbe reserves one expired user channel circuit.
func TryStartUserChannelRecoveryProbe(c *gin.Context, channelID int, model string, requestTypes ...RequestType) bool {
	key, ok := userChannelHealthKey(c, channelID, model, requestTypes...)
	return ok && tryStartUserRouteProbe(key, 1, true)
}

// TryStartUserChannelEmergencyProbe admits one bounded retry probe.
func TryStartUserChannelEmergencyProbe(c *gin.Context, channelID int, model string, requestTypes ...RequestType) bool {
	key, ok := userChannelHealthKey(c, channelID, model, requestTypes...)
	return ok && tryStartUserRouteProbe(key, channelHealthEmergencyProbeSlots, false)
}

// TryStartUserChannelLastResortProbe keeps one route available during cooling.
func TryStartUserChannelLastResortProbe(c *gin.Context, channelID int, model string, requestTypes ...RequestType) bool {
	key, ok := userChannelHealthKey(c, channelID, model, requestTypes...)
	return ok && tryStartUserRouteProbe(key, 1, false)
}

// TryStartUserFaultDomainRecoveryProbe reserves one expired user domain circuit.
func TryStartUserFaultDomainRecoveryProbe(c *gin.Context, domain, model string, requestTypes ...RequestType) bool {
	key, ok := userFaultDomainHealthKey(c, domain, model, requestTypes...)
	return ok && tryStartUserRouteProbe(key, 1, true)
}

// TryStartUserFaultDomainEmergencyProbe admits one bounded domain retry probe.
func TryStartUserFaultDomainEmergencyProbe(c *gin.Context, domain, model string, requestTypes ...RequestType) bool {
	key, ok := userFaultDomainHealthKey(c, domain, model, requestTypes...)
	return ok && tryStartUserRouteProbe(key, channelHealthEmergencyProbeSlots, false)
}

// TryStartUserFaultDomainLastResortProbe keeps one domain available during cooling.
func TryStartUserFaultDomainLastResortProbe(c *gin.Context, domain, model string, requestTypes ...RequestType) bool {
	key, ok := userFaultDomainHealthKey(c, domain, model, requestTypes...)
	return ok && tryStartUserRouteProbe(key, 1, false)
}

func tryStartUserRouteProbe(key string, maxSlots int, requireExpired bool) bool {
	lock := userRouteHealthLock(key)
	if !lock.TryLock() {
		return false
	}
	defer lock.Unlock()

	now := time.Now().UTC()
	started := false
	err := getChannelHealthCache().UpdateWithTTL(key, channelHealthTTL, func(state ChannelHealth, found bool) (ChannelHealth, error) {
		started = false
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
	return err == nil && started
}

// ReleaseUserChannelProbe releases a partially acquired channel lease.
func ReleaseUserChannelProbe(c *gin.Context, channelID int, model string, requestTypes ...RequestType) {
	key, ok := userChannelHealthKey(c, channelID, model, requestTypes...)
	if ok {
		releaseUserRouteProbe(key)
	}
}

// ReleaseUserFaultDomainProbe releases a partially acquired domain lease.
func ReleaseUserFaultDomainProbe(c *gin.Context, domain, model string, requestTypes ...RequestType) {
	key, ok := userFaultDomainHealthKey(c, domain, model, requestTypes...)
	if ok {
		releaseUserRouteProbe(key)
	}
}

func releaseUserRouteProbe(key string) {
	lock := userRouteHealthLock(key)
	lock.Lock()
	defer lock.Unlock()
	_ = getChannelHealthCache().UpdateWithTTL(key, channelHealthTTL, func(state ChannelHealth, found bool) (ChannelHealth, error) {
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
