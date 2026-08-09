package runtime

import (
	"sync"
	"time"
)

// FirstByteTrace separates gateway work from the wait for the first upstream
// stream event. It deliberately stores no request content or upstream identity.
type FirstByteTrace struct {
	mu               sync.RWMutex
	startedAt        time.Time
	relayInfoReadyAt time.Time
	preflightDoneAt  time.Time
	routeSelectedAt  time.Time
	upstreamStartAt  time.Time
	firstEventAt     time.Time
}

func NewFirstByteTrace(startedAt time.Time) *FirstByteTrace {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	return &FirstByteTrace{startedAt: startedAt}
}

func (t *FirstByteTrace) MarkRelayInfoReady() { t.mark(&t.relayInfoReadyAt) }
func (t *FirstByteTrace) MarkPreflightDone()  { t.mark(&t.preflightDoneAt) }
func (t *FirstByteTrace) MarkRouteSelected()  { t.mark(&t.routeSelectedAt) }
func (t *FirstByteTrace) MarkUpstreamStart()  { t.mark(&t.upstreamStartAt) }
func (t *FirstByteTrace) MarkFirstEvent()     { t.mark(&t.firstEventAt) }

func (t *FirstByteTrace) mark(target *time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if target.IsZero() {
		*target = time.Now()
	}
}

// Snapshot returns millisecond durations suitable for operational logs.
func (t *FirstByteTrace) Snapshot() map[string]int64 {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.firstEventAt.IsZero() {
		return nil
	}
	return map[string]int64{
		"ingress_ms":              durationMilliseconds(t.startedAt, t.relayInfoReadyAt),
		"preflight_ms":            durationMilliseconds(t.relayInfoReadyAt, t.preflightDoneAt),
		"route_selection_ms":      durationMilliseconds(t.preflightDoneAt, t.routeSelectedAt),
		"dispatch_ms":             durationMilliseconds(t.routeSelectedAt, t.upstreamStartAt),
		"upstream_first_event_ms": durationMilliseconds(t.upstreamStartAt, t.firstEventAt),
		"total_ms":                durationMilliseconds(t.startedAt, t.firstEventAt),
	}
}

func durationMilliseconds(start, end time.Time) int64 {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}
