package runtime

import "time"

// TryStartFaultDomainLastResortProbe reserves one cooling fault domain for a
// real request when every route-pool candidate is cooling. The lease keeps a
// shared upstream from receiving a concurrent recovery burst.
func TryStartFaultDomainLastResortProbe(domain, model string) bool {
	if domain == "" || model == "" {
		return false
	}
	lock := faultDomainHealthLock(domain)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	started := false
	_ = getChannelHealthCache().UpdateWithTTL(faultDomainHealthKey(domain, model), channelHealthTTL, func(state ChannelHealth, found bool) (ChannelHealth, error) {
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
