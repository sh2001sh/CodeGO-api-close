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
