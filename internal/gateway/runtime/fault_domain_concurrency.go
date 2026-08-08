package runtime

import (
	"sync"
	"time"
)

const (
	faultDomainInitialConcurrency          = 128
	faultDomainMinConcurrency              = 8
	faultDomainMaxConcurrency              = 256
	faultDomainConcurrencyFailureWindow    = 15 * time.Second
	faultDomainConcurrencyFailureThreshold = 3
	faultDomainRecoveryStep                = 8
	faultDomainSuccessStep                 = 16
)

type faultDomainConcurrencyState struct {
	active         int
	limit          int
	successes      int
	failures       []time.Time
	lastAdjustment time.Time
}

var faultDomainConcurrency = struct {
	sync.Mutex
	states map[string]*faultDomainConcurrencyState
}{states: make(map[string]*faultDomainConcurrencyState)}

// FaultDomainConcurrencySnapshot is internal telemetry for route audits and tests.
type FaultDomainConcurrencySnapshot struct {
	Domain string
	Model  string
	Active int
	Limit  int
}

// TryAcquireFaultDomainSlot reserves one in-flight upstream request. The
// lease must be released after the upstream attempt completes. Capacity is
// process-local and intentionally shared by all channels using one domain.
func TryAcquireFaultDomainSlot(domain, model string, requestTypes ...RequestType) (release func(success bool, statusCode int), acquired bool, snapshot FaultDomainConcurrencySnapshot) {
	domain = normalizeFaultDomain(domain)
	if domain == "" || model == "" {
		return func(bool, int) {}, true, FaultDomainConcurrencySnapshot{Domain: domain, Model: model}
	}
	key := domain + "\x00" + model + "\x00" + string(normalizedRequestType(requestTypes...))
	faultDomainConcurrency.Lock()
	state := faultDomainConcurrency.states[key]
	if state == nil {
		state = &faultDomainConcurrencyState{limit: faultDomainInitialConcurrency}
		faultDomainConcurrency.states[key] = state
	}
	snapshot = FaultDomainConcurrencySnapshot{Domain: domain, Model: model, Active: state.active, Limit: state.limit}
	if state.active >= state.limit {
		faultDomainConcurrency.Unlock()
		return func(bool, int) {}, false, snapshot
	}
	state.active++
	snapshot.Active = state.active
	faultDomainConcurrency.Unlock()

	var once sync.Once
	return func(success bool, statusCode int) {
		once.Do(func() {
			finishFaultDomainSlot(key, success, statusCode)
		})
	}, true, snapshot
}

func finishFaultDomainSlot(key string, success bool, statusCode int) {
	faultDomainConcurrency.Lock()
	defer faultDomainConcurrency.Unlock()
	state := faultDomainConcurrency.states[key]
	if state == nil {
		return
	}
	if state.active > 0 {
		state.active--
	}
	now := time.Now()
	cutoff := now.Add(-faultDomainConcurrencyFailureWindow)
	filtered := state.failures[:0]
	for _, occurredAt := range state.failures {
		if occurredAt.After(cutoff) {
			filtered = append(filtered, occurredAt)
		}
	}
	state.failures = filtered

	if success || !isAdaptiveCapacityFailure(statusCode) {
		state.successes++
		if state.successes >= faultDomainSuccessStep && state.limit < faultDomainMaxConcurrency {
			state.limit += faultDomainRecoveryStep
			if state.limit > faultDomainMaxConcurrency {
				state.limit = faultDomainMaxConcurrency
			}
			state.successes = 0
			state.lastAdjustment = now
		}
		return
	}

	state.successes = 0
	state.failures = append(state.failures, now)
	if len(state.failures) < faultDomainConcurrencyFailureThreshold {
		return
	}
	state.limit /= 2
	if state.limit < faultDomainMinConcurrency {
		state.limit = faultDomainMinConcurrency
	}
	state.failures = nil
	state.lastAdjustment = now
}

func isAdaptiveCapacityFailure(statusCode int) bool {
	return IsAdaptiveFaultDomainFailureStatus(statusCode)
}

// IsAdaptiveFaultDomainFailureStatus identifies failures that indicate shared
// upstream pressure rather than a user, billing, or request validation error.
func IsAdaptiveFaultDomainFailureStatus(statusCode int) bool {
	return statusCode == 429 || statusCode == 502 || statusCode == 503 || statusCode == 504 || statusCode == 524
}

func normalizeFaultDomain(domain string) string {
	return domain
}
