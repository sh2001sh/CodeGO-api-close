package runtime

import (
	"hash/fnv"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	faultDomainHealthKeyPrefix  = "fault-domain\x00"
	faultDomainFailureThreshold = 3
	faultDomainFailureWindow    = 15 * time.Second
)

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
	if !lock.TryLock() {
		return false
	}
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

// RecordFaultDomainRetryableFailure opens a domain circuit only after three
// distinct transient request failures. A failed half-open probe increases the
// adaptive backoff, while retries of the same request count only once.
func RecordFaultDomainRetryableFailure(domain, model, requestID string, cooldown time.Duration) {
	if domain == "" || model == "" {
		return
	}
	lock := faultDomainHealthLock(domain)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	_ = getChannelHealthCache().UpdateWithTTL(faultDomainHealthKey(domain, model), channelHealthTTL, func(state ChannelHealth, _ bool) (ChannelHealth, error) {
		if requestID != "" && state.LastFailureRequestID == requestID {
			return state, nil
		}
		state.Model = model
		state.LastFailureAt = now
		state.LastFailureRequestID = requestID
		state.ConsecutiveRetryableFailures++
		state.RecoveryProbeSuccesses = 0
		state.RecoveryProbeUntil = time.Time{}
		backoffLevel := faultDomainBackoffLevel(state.ConsecutiveRetryableFailures)
		if state.CoolingUntil.After(now) && state.State != ChannelHealthHalfOpen {
			if state.ConsecutiveRetryableFailures >= faultDomainFailureThreshold {
				state.CoolingUntil = now.Add(adaptiveChannelCooldown(cooldown, backoffLevel))
			}
			return state, nil
		}
		if state.ConsecutiveRetryableFailures < faultDomainFailureThreshold {
			state.CoolingUntil = time.Time{}
			state.State = ChannelHealthDegraded
			return state, nil
		}
		state.CoolingUntil = now.Add(adaptiveChannelCooldown(cooldown, backoffLevel))
		state.State = ChannelHealthCooling
		return state, nil
	})
}

// RecordFaultDomainChannelFailure promotes a transient failure to provider
// scope only after the same model fails on three distinct channels in a short
// window. A single API key or route group therefore cools only itself, while a
// genuine shared-provider incident still prevents a retry storm.
func RecordFaultDomainChannelFailure(domain, model string, channelID int, requestID string, cooldown time.Duration) bool {
	if domain == "" || model == "" || channelID <= 0 {
		return false
	}
	lock := faultDomainHealthLock(domain)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	providerWide := false
	_ = getChannelHealthCache().UpdateWithTTL(faultDomainHealthKey(domain, model), channelHealthTTL, func(state ChannelHealth, _ bool) (ChannelHealth, error) {
		state.Model = model
		state.FaultDomainFailures = recordFaultDomainFailureChannel(state.FaultDomainFailures, channelID, now)
		if len(state.FaultDomainFailures) < faultDomainFailureThreshold {
			return state, nil
		}
		providerWide = true
		if requestID != "" && state.LastFailureRequestID == requestID {
			return state, nil
		}
		state.LastFailureAt = now
		state.LastFailureRequestID = requestID
		state.ConsecutiveRetryableFailures = max(state.ConsecutiveRetryableFailures+1, faultDomainFailureThreshold)
		state.RecoveryProbeSuccesses = 0
		state.RecoveryProbeUntil = time.Time{}
		state.CoolingUntil = now.Add(adaptiveChannelCooldown(cooldown, faultDomainBackoffLevel(state.ConsecutiveRetryableFailures)))
		state.State = ChannelHealthCooling
		return state, nil
	})
	return providerWide
}

func recordFaultDomainFailureChannel(failures []FaultDomainFailure, channelID int, now time.Time) []FaultDomainFailure {
	cutoff := now.Add(-faultDomainFailureWindow)
	filtered := failures[:0]
	matched := false
	for _, failure := range failures {
		if failure.OccurredAt.Before(cutoff) {
			continue
		}
		if failure.ChannelID == channelID {
			if !matched {
				filtered = append(filtered, FaultDomainFailure{ChannelID: channelID, OccurredAt: now})
				matched = true
			}
			continue
		}
		filtered = append(filtered, failure)
	}
	if !matched {
		filtered = append(filtered, FaultDomainFailure{ChannelID: channelID, OccurredAt: now})
	}
	return filtered
}

func faultDomainBackoffLevel(failures int) int {
	if failures <= faultDomainFailureThreshold {
		return 1
	}
	return failures - faultDomainFailureThreshold + 1
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
