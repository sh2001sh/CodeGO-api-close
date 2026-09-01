package concurrency

import (
	"sync"
	"sync/atomic"
)

// RelayAdmissionStats describes the process-local relay capacity state.
type RelayAdmissionStats struct {
	Active   int64
	Capacity int
	Rejected uint64
}

var relayAdmission struct {
	mu       sync.RWMutex
	slots    chan struct{}
	capacity int
	active   atomic.Int64
	rejected atomic.Uint64
}

var relayUploadAdmission struct {
	mu       sync.RWMutex
	slots    chan struct{}
	capacity int
	active   atomic.Int64
	rejected atomic.Uint64
}

// ConfigureRelayAdmission configures the maximum number of active relay
// requests. A non-positive capacity disables admission control.
func ConfigureRelayAdmission(capacity int) {
	relayAdmission.mu.Lock()
	defer relayAdmission.mu.Unlock()

	relayAdmission.capacity = capacity
	if capacity > 0 {
		relayAdmission.slots = make(chan struct{}, capacity)
	} else {
		relayAdmission.slots = nil
	}
	relayAdmission.active.Store(0)
	relayAdmission.rejected.Store(0)
}

// ConfigureRelayUploadAdmission configures the smaller gate used while a
// client request body is being received. It is intentionally independent from
// upstream relay capacity because slow uploads must not consume upstream slots.
func ConfigureRelayUploadAdmission(capacity int) {
	relayUploadAdmission.mu.Lock()
	defer relayUploadAdmission.mu.Unlock()
	relayUploadAdmission.capacity = capacity
	if capacity > 0 {
		relayUploadAdmission.slots = make(chan struct{}, capacity)
	} else {
		relayUploadAdmission.slots = nil
	}
	relayUploadAdmission.active.Store(0)
	relayUploadAdmission.rejected.Store(0)
}

func TryAcquireRelayUploadSlot() (release func(), admitted bool, stats RelayAdmissionStats) {
	relayUploadAdmission.mu.RLock()
	slots, capacity := relayUploadAdmission.slots, relayUploadAdmission.capacity
	relayUploadAdmission.mu.RUnlock()
	if slots == nil {
		// Preserve the historical behavior for callers/tests that configure only
		// the relay gate. Production bootstrap always configures this gate.
		if capacity == 0 {
			return TryAcquireRelaySlot()
		}
		return func() {}, true, RelayAdmissionStats{Capacity: capacity}
	}
	select {
	case slots <- struct{}{}:
		active := relayUploadAdmission.active.Add(1)
		var once sync.Once
		return func() { once.Do(func() { <-slots; relayUploadAdmission.active.Add(-1) }) }, true,
			RelayAdmissionStats{Active: active, Capacity: capacity, Rejected: relayUploadAdmission.rejected.Load()}
	default:
		rejected := relayUploadAdmission.rejected.Add(1)
		return nil, false, RelayAdmissionStats{Active: relayUploadAdmission.active.Load(), Capacity: capacity, Rejected: rejected}
	}
}

// TryAcquireRelaySlot admits one relay request without queueing. The returned
// release function is safe to call more than once.
func TryAcquireRelaySlot() (release func(), admitted bool, stats RelayAdmissionStats) {
	relayAdmission.mu.RLock()
	slots := relayAdmission.slots
	capacity := relayAdmission.capacity
	relayAdmission.mu.RUnlock()

	if slots == nil {
		return func() {}, true, RelayAdmissionStats{Capacity: capacity}
	}

	select {
	case slots <- struct{}{}:
		active := relayAdmission.active.Add(1)
		var once sync.Once
		return func() {
			once.Do(func() {
				<-slots
				relayAdmission.active.Add(-1)
			})
		}, true, RelayAdmissionStats{Active: active, Capacity: capacity, Rejected: relayAdmission.rejected.Load()}
	default:
		rejected := relayAdmission.rejected.Add(1)
		return nil, false, RelayAdmissionStats{Active: relayAdmission.active.Load(), Capacity: capacity, Rejected: rejected}
	}
}

func RelayAdmissionSnapshot() RelayAdmissionStats {
	relayAdmission.mu.RLock()
	capacity := relayAdmission.capacity
	relayAdmission.mu.RUnlock()
	return RelayAdmissionStats{
		Active:   relayAdmission.active.Load(),
		Capacity: capacity,
		Rejected: relayAdmission.rejected.Load(),
	}
}
