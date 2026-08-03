package runtime

import "time"

// TryStartChannelLastResortProbe reserves a cooling channel for one real
// request when a route pool has no healthy candidate left. The normal recovery
// path waits for CoolingUntil; this path retains the same short lease but may
// probe earlier, preventing an all-cooling pool from becoming a hard outage.
func TryStartChannelLastResortProbe(channelID int, model string) bool {
	if channelID <= 0 || model == "" {
		return false
	}
	lock := channelHealthLock(channelID)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	started := false
	_ = getChannelHealthCache().UpdateWithTTL(channelHealthKey(channelID, model), channelHealthTTL, func(state ChannelHealth, found bool) (ChannelHealth, error) {
		if !found || (state.State != ChannelHealthCooling && state.State != ChannelHealthHalfOpen) || state.RecoveryProbeUntil.After(now) {
			return state, nil
		}
		state.State = ChannelHealthHalfOpen
		state.RecoveryProbeUntil = now.Add(channelHealthProbeLeaseDuration)
		started = true
		return state, nil
	})
	return started
}
