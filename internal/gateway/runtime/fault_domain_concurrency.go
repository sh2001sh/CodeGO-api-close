package runtime

import (
	"strconv"
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
	active int
	users  map[string]*faultDomainUserConcurrencyState
}

type faultDomainUserConcurrencyState struct {
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
	Domain     string
	Scope      string
	Model      string
	Active     int
	Limit      int // deprecated shared limit; always zero because admission is per user
	UserActive int
	UserLimit  int
}

// TryAcquireFaultDomainSlot reserves one in-flight upstream request. The
// lease must be released after the upstream attempt completes. This legacy
// wrapper uses one shared anonymous user key; relay requests should call
// TryAcquireFaultDomainSlotForUser instead.
func TryAcquireFaultDomainSlot(domain, model string, requestTypes ...RequestType) (release func(success bool, statusCode int), acquired bool, snapshot FaultDomainConcurrencySnapshot) {
	return TryAcquireFaultDomainSlotForUser(domain, domain, model, 0, requestTypes...)
}

// TryAcquireFaultDomainSlotForUser reserves one in-flight upstream request.
// Admission and adaptive capacity are tracked per user. A provider fault
// domain is still used for health/cooldown scope, but never as a shared
// concurrency ceiling.
func TryAcquireFaultDomainSlotForUser(domain, scope, model string, userID int, requestTypes ...RequestType) (release func(success bool, statusCode int), acquired bool, snapshot FaultDomainConcurrencySnapshot) {
	domain = normalizeFaultDomain(domain)
	scope = normalizeFaultDomain(scope)
	if domain == "" || model == "" {
		return func(bool, int) {}, true, FaultDomainConcurrencySnapshot{Domain: domain, Scope: scope, Model: model}
	}
	if scope == "" {
		scope = domain
	}
	key := scope + "\x00" + domain + "\x00" + model + "\x00" + string(normalizedRequestType(requestTypes...))
	userKey := strconv.Itoa(userID)
	if userID <= 0 {
		userKey = "anonymous"
	}
	faultDomainConcurrency.Lock()
	state := faultDomainConcurrency.states[key]
	if state == nil {
		state = &faultDomainConcurrencyState{
			users: make(map[string]*faultDomainUserConcurrencyState),
		}
		faultDomainConcurrency.states[key] = state
	}
	userState := state.users[userKey]
	if userState == nil {
		userState = &faultDomainUserConcurrencyState{limit: faultDomainInitialConcurrency}
		state.users[userKey] = userState
	}
	snapshot = FaultDomainConcurrencySnapshot{
		Domain:     domain,
		Scope:      scope,
		Model:      model,
		Active:     state.active,
		UserActive: userState.active,
		UserLimit:  userState.limit,
	}
	if userState.active >= userState.limit {
		faultDomainConcurrency.Unlock()
		return func(bool, int) {}, false, snapshot
	}
	state.active++
	userState.active++
	snapshot.Active = state.active
	snapshot.UserActive = userState.active
	faultDomainConcurrency.Unlock()

	var once sync.Once
	return func(success bool, statusCode int) {
		once.Do(func() {
			finishFaultDomainSlot(key, userKey, success, statusCode)
		})
	}, true, snapshot
}

func finishFaultDomainSlot(key, userKey string, success bool, statusCode int) {
	faultDomainConcurrency.Lock()
	defer faultDomainConcurrency.Unlock()
	state := faultDomainConcurrency.states[key]
	if state == nil {
		return
	}
	if state.active > 0 {
		state.active--
	}
	userState := state.users[userKey]
	if userState == nil {
		return
	}
	if userState.active > 0 {
		userState.active--
	}
	now := time.Now()
	cutoff := now.Add(-faultDomainConcurrencyFailureWindow)
	filtered := userState.failures[:0]
	for _, occurredAt := range userState.failures {
		if occurredAt.After(cutoff) {
			filtered = append(filtered, occurredAt)
		}
	}
	userState.failures = filtered

	if success || !isAdaptiveCapacityFailure(statusCode) {
		userState.successes++
		if userState.successes >= faultDomainSuccessStep && userState.limit < faultDomainMaxConcurrency {
			userState.limit += faultDomainRecoveryStep
			if userState.limit > faultDomainMaxConcurrency {
				userState.limit = faultDomainMaxConcurrency
			}
			userState.successes = 0
			userState.lastAdjustment = now
		}
		return
	}

	userState.successes = 0
	userState.failures = append(userState.failures, now)
	if len(userState.failures) < faultDomainConcurrencyFailureThreshold {
		return
	}
	userState.limit /= 2
	if userState.limit < faultDomainMinConcurrency {
		userState.limit = faultDomainMinConcurrency
	}
	userState.failures = nil
	userState.lastAdjustment = now
}

func isAdaptiveCapacityFailure(statusCode int) bool {
	return IsAdaptiveFaultDomainFailureStatus(statusCode)
}

// IsAdaptiveFaultDomainFailureStatus identifies failures that reliably signal
// shared upstream admission pressure. A 502 stream close or a 524 may be a
// request-local transport interruption after the upstream has accepted work;
// shrinking the shared admission window for those failures creates a 503 wave
// for otherwise unrelated requests.
func IsAdaptiveFaultDomainFailureStatus(statusCode int) bool {
	return statusCode == 429 || statusCode == 503 || statusCode == 504
}

func normalizeFaultDomain(domain string) string {
	return domain
}
