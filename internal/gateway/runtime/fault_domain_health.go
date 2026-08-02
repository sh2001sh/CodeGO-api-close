package runtime

import (
	"hash/fnv"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const faultDomainHealthKeyPrefix = "fault-domain\x00"

// ChannelFaultDomain derives a shared transient-failure boundary without
// relying on channel names, keys, or user-visible configuration.
func ChannelFaultDomain(channelType int, baseURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	return strconv.Itoa(channelType) + ":" + strings.ToLower(parsed.Hostname())
}

// GetFaultDomainHealth returns transient health shared by channels using the
// same provider type and upstream host for one model.
func GetFaultDomainHealth(domain, model string) (ChannelHealth, bool) {
	if domain == "" || model == "" {
		return ChannelHealth{}, false
	}
	state, found, err := getChannelHealthCache().Get(faultDomainHealthKey(domain, model))
	return state, found && err == nil
}

// IsFaultDomainCooling reports whether a domain must be excluded from normal
// routing. Expired cooling states are deliberately left available for one probe.
func IsFaultDomainCooling(domain, model string) bool {
	state, found := GetFaultDomainHealth(domain, model)
	if !found {
		return false
	}
	now := time.Now()
	return (state.State == ChannelHealthCooling && state.CoolingUntil.After(now)) ||
		(state.State == ChannelHealthHalfOpen && state.RecoveryProbeUntil.After(now))
}

// TryStartFaultDomainRecoveryProbe reserves one expired domain circuit for a
// real request, preventing same-domain channels from probing concurrently.
func TryStartFaultDomainRecoveryProbe(domain, model string) bool {
	if domain == "" || model == "" {
		return false
	}
	lock := faultDomainHealthLock(domain)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	started := false
	_ = getChannelHealthCache().UpdateWithTTL(faultDomainHealthKey(domain, model), channelHealthTTL, func(state ChannelHealth, found bool) (ChannelHealth, error) {
		if !found || (state.State != ChannelHealthCooling && state.State != ChannelHealthHalfOpen) || state.CoolingUntil.After(now) || state.RecoveryProbeUntil.After(now) {
			return state, nil
		}
		state.State = ChannelHealthHalfOpen
		state.RecoveryProbeUntil = now.Add(channelHealthProbeLeaseDuration)
		started = true
		return state, nil
	})
	return started
}

// RecordFaultDomainRetryableFailure cools all same-domain model routes after a
// transient failure. Failures while the circuit is already closed do not extend
// its timer; a failed half-open probe increases the adaptive backoff instead.
func RecordFaultDomainRetryableFailure(domain, model string, cooldown time.Duration) {
	if domain == "" || model == "" {
		return
	}
	lock := faultDomainHealthLock(domain)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	_ = getChannelHealthCache().UpdateWithTTL(faultDomainHealthKey(domain, model), channelHealthTTL, func(state ChannelHealth, _ bool) (ChannelHealth, error) {
		state.Model = model
		state.LastFailureAt = now
		state.ConsecutiveRetryableFailures++
		state.RecoveryProbeSuccesses = 0
		state.RecoveryProbeUntil = time.Time{}
		if state.CoolingUntil.After(now) && state.State != ChannelHealthHalfOpen {
			if state.ConsecutiveRetryableFailures >= channelHealthFailureThreshold {
				state.CoolingUntil = now.Add(adaptiveChannelCooldown(cooldown, state.ConsecutiveRetryableFailures))
			}
			return state, nil
		}
		state.CoolingUntil = now.Add(adaptiveChannelCooldown(cooldown, state.ConsecutiveRetryableFailures))
		state.State = ChannelHealthCooling
		return state, nil
	})
}

// RecordFaultDomainSuccess advances a half-open domain circuit. Two successful
// probes are required before the domain becomes a normal routing candidate.
func RecordFaultDomainSuccess(domain, model string) {
	if domain == "" || model == "" {
		return
	}
	lock := faultDomainHealthLock(domain)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	_ = getChannelHealthCache().UpdateWithTTL(faultDomainHealthKey(domain, model), channelHealthTTL, func(state ChannelHealth, _ bool) (ChannelHealth, error) {
		state.Model = model
		state.LastSuccessAt = now
		if state.State == ChannelHealthCooling && state.CoolingUntil.After(now) {
			return state, nil
		}
		if state.State == ChannelHealthHalfOpen || state.State == ChannelHealthCooling {
			state.RecoveryProbeSuccesses++
			state.RecoveryProbeUntil = time.Time{}
			if state.RecoveryProbeSuccesses < 2 {
				state.State = ChannelHealthCooling
				state.CoolingUntil = now
				return state, nil
			}
		}
		state.ConsecutiveRetryableFailures = 0
		state.RecoveryProbeSuccesses = 0
		state.RecoveryProbeUntil = time.Time{}
		state.State = ChannelHealthHealthy
		state.CoolingUntil = time.Time{}
		return state, nil
	})
}

func faultDomainHealthKey(domain, model string) string {
	return faultDomainHealthKeyPrefix + domain + "\x00" + model
}

func faultDomainHealthLock(domain string) *sync.Mutex {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(domain))
	return &channelHealthLocks[int(hash.Sum32())%len(channelHealthLocks)]
}
