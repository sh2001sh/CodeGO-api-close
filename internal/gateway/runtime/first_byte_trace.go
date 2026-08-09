package runtime

import (
	"sync"
	"time"
)

// FirstByteTrace separates gateway work from the wait for the first upstream
// stream event. It deliberately stores no request content or upstream identity.
type FirstByteTrace struct {
	mu                  sync.RWMutex
	startedAt           time.Time
	requestValidAt      time.Time
	admittedAt          time.Time
	relayInfoReadyAt    time.Time
	preflightDoneAt     time.Time
	routeSelectedAt     time.Time
	upstreamStartAt     time.Time
	firstEventAt        time.Time
	firstSemanticReadAt time.Time
	firstSemanticAt     time.Time
	firstTextReadAt     time.Time
	firstTextAt         time.Time
	firstSemanticIsText bool
	semanticKindMarked  bool
}

func NewFirstByteTrace(startedAt time.Time) *FirstByteTrace {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	return &FirstByteTrace{startedAt: startedAt}
}

func (t *FirstByteTrace) MarkRequestValidated() { t.mark(&t.requestValidAt) }
func (t *FirstByteTrace) MarkAdmitted()         { t.mark(&t.admittedAt) }
func (t *FirstByteTrace) MarkRelayInfoReady()   { t.mark(&t.relayInfoReadyAt) }
func (t *FirstByteTrace) MarkPreflightDone()    { t.mark(&t.preflightDoneAt) }
func (t *FirstByteTrace) MarkRouteSelected()    { t.mark(&t.routeSelectedAt) }
func (t *FirstByteTrace) MarkUpstreamStart()    { t.mark(&t.upstreamStartAt) }
func (t *FirstByteTrace) MarkFirstEvent()       { t.mark(&t.firstEventAt) }
func (t *FirstByteTrace) MarkFirstSemanticEvent() {
	t.mark(&t.firstSemanticAt)
}

// MarkFirstSemanticReadAt records when the scanner received the first
// model-visible SSE frame, before handler scheduling and JSON decoding.
func (t *FirstByteTrace) MarkFirstSemanticReadAt(receivedAt time.Time, isText bool) {
	if t == nil || receivedAt.IsZero() {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.firstSemanticReadAt.IsZero() {
		t.firstSemanticReadAt = receivedAt
		t.firstSemanticIsText = isText
		t.semanticKindMarked = true
	}
}

// MarkFirstTextReadAt records when the scanner received the first visible
// output-text delta, excluding reasoning and lifecycle frames.
func (t *FirstByteTrace) MarkFirstTextReadAt(receivedAt time.Time) {
	if t == nil || receivedAt.IsZero() {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.firstTextReadAt.IsZero() {
		t.firstTextReadAt = receivedAt
	}
}

func (t *FirstByteTrace) MarkFirstTextEvent() {
	t.mark(&t.firstTextAt)
}

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
	if t.firstSemanticAt.IsZero() {
		return nil
	}
	snapshot := map[string]int64{
		"ingress_ms":                       durationMilliseconds(t.startedAt, t.relayInfoReadyAt),
		"request_validation_ms":            durationMilliseconds(t.startedAt, t.requestValidAt),
		"admission_ms":                     durationMilliseconds(t.requestValidAt, t.admittedAt),
		"relay_info_ms":                    durationMilliseconds(t.admittedAt, t.relayInfoReadyAt),
		"preflight_ms":                     durationMilliseconds(t.relayInfoReadyAt, t.preflightDoneAt),
		"route_selection_ms":               durationMilliseconds(t.preflightDoneAt, t.routeSelectedAt),
		"dispatch_ms":                      durationMilliseconds(t.routeSelectedAt, t.upstreamStartAt),
		"upstream_first_event_ms":          durationMilliseconds(t.upstreamStartAt, t.firstEventAt),
		"event_to_semantic_ms":             durationMilliseconds(t.firstEventAt, t.firstSemanticAt),
		"upstream_first_semantic_read_ms":  durationMilliseconds(t.upstreamStartAt, t.firstSemanticReadAt),
		"semantic_read_to_handler_ms":      durationMilliseconds(t.firstSemanticReadAt, t.firstSemanticAt),
		"upstream_first_semantic_event_ms": durationMilliseconds(t.upstreamStartAt, t.firstSemanticAt),
		"total_raw_event_ms":               durationMilliseconds(t.startedAt, t.firstEventAt),
		"total_ms":                         durationMilliseconds(t.startedAt, t.firstSemanticAt),
	}
	if t.semanticKindMarked {
		if t.firstSemanticIsText {
			snapshot["first_semantic_is_text"] = 1
		} else {
			snapshot["first_semantic_is_text"] = 0
		}
	}
	if !t.firstTextReadAt.IsZero() {
		snapshot["upstream_first_text_read_ms"] = durationMilliseconds(t.upstreamStartAt, t.firstTextReadAt)
		snapshot["text_read_to_handler_ms"] = durationMilliseconds(t.firstTextReadAt, t.firstTextAt)
	}
	return snapshot
}

func durationMilliseconds(start, end time.Time) int64 {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}
