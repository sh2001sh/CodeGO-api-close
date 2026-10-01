package credentials

import (
	"container/heap"
	"context"
	"errors"
	"time"
)

var ErrChanged = errors.New("credentials: credential changed or disabled")

type scheduled struct {
	credential Credential
	due        time.Time
	failures   int
	index      int
}

type schedule []*scheduled

func (h schedule) Len() int { return len(h) }
func (h schedule) Less(i, j int) bool {
	if h[i].due.Equal(h[j].due) {
		return h[i].credential.ID < h[j].credential.ID
	}
	return h[i].due.Before(h[j].due)
}
func (h schedule) Swap(i, j int) { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }
func (h *schedule) Push(v any)   { n := v.(*scheduled); n.index = len(*h); *h = append(*h, n) }
func (h *schedule) Pop() any {
	a := *h
	n := a[len(a)-1]
	a[len(a)-1] = nil
	*h = a[:len(a)-1]
	n.index = -1
	return n
}

// merge keeps retries and cooldowns for unchanged records while promptly
// replacing admin updates and removing credentials disabled since last reload.
func (h *schedule) merge(creds []Credential, before time.Duration) {
	current := make(map[int64]*scheduled, len(*h))
	for _, n := range *h {
		current[n.credential.ID] = n
	}
	next := make(schedule, 0, len(creds))
	for _, c := range creds {
		if c.ExpiresAt.IsZero() {
			continue
		}
		n, ok := current[c.ID]
		if !ok || !n.credential.UpdatedAt.Equal(c.UpdatedAt) {
			n = &scheduled{credential: c, due: c.ExpiresAt.Add(-before)}
		}
		n.index = len(next)
		next = append(next, n)
	}
	*h = next
	heap.Init(h)
}

// schedulerWait computes how long to sleep before the next due item in
// queue, clamped to not run before nextAllowed (QPS pacing) or circuitUntil
// (circuit breaker cooldown). Returns time.Hour when the queue is empty.
func schedulerWait(queue schedule, nextAllowed, circuitUntil time.Time) time.Duration {
	if len(queue) == 0 {
		return time.Hour
	}
	due := queue[0].due
	if nextAllowed.After(due) {
		due = nextAllowed
	}
	if circuitUntil.After(due) {
		due = circuitUntil
	}
	wait := time.Until(due)
	if wait < 0 {
		wait = 0
	}
	return wait
}

// recordRefreshFailure updates n's retry state and the circuit breaker after
// a failed refresh attempt, logging the failure (identifiers only; endpoint
// error details can contain tokens).
func (p *Pool) recordRefreshFailure(n *scheduled, err error, provider string, consecutiveFailures *int, circuitUntil *time.Time) {
	n.failures++
	*consecutiveFailures++
	backoff := p.cfg.RetryBackoff
	for i := 1; i < n.failures && backoff < p.cfg.MaxBackoff; i++ {
		if backoff > p.cfg.MaxBackoff/2 {
			backoff = p.cfg.MaxBackoff
		} else {
			backoff *= 2
		}
	}
	var retry *RetryError
	if errors.As(err, &retry) && retry.After > backoff {
		backoff = retry.After
	}
	n.due = time.Now().Add(backoff)
	if *consecutiveFailures >= p.cfg.CircuitFailures {
		*circuitUntil = time.Now().Add(p.cfg.CircuitCooldown)
		if n.due.After(*circuitUntil) {
			*circuitUntil = n.due
		}
		*consecutiveFailures = 0
	}
	p.log.Warn("credentials: refresh failed", "provider", provider, "credential_id", n.credential.ID, "retry_at", n.due)
}

// recordRefreshSuccess updates n with the refreshed credential and resets
// failure/circuit-breaker state after a successful refresh.
func (p *Pool) recordRefreshSuccess(n *scheduled, fresh Credential, nextAllowed time.Time, consecutiveFailures *int, circuitUntil *time.Time) {
	*consecutiveFailures = 0
	*circuitUntil = time.Time{}
	n.credential = fresh
	n.failures = 0
	n.due = fresh.ExpiresAt.Add(-p.cfg.RefreshBefore)
	// Short lived access tokens must not spin in the heap.
	if n.due.Before(nextAllowed) {
		n.due = nextAllowed
	}
}

func (p *Pool) runProvider(ctx context.Context, provider string, refresher Refresher, updates <-chan []Credential, qps int) {
	var queue schedule
	var nextAllowed, circuitUntil time.Time
	consecutiveFailures := 0
	interval := time.Second / time.Duration(qps)
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		timer.Reset(schedulerWait(queue, nextAllowed, circuitUntil))
		select {
		case <-ctx.Done():
			return
		case creds := <-updates:
			queue.merge(creds, p.cfg.RefreshBefore)
		case <-timer.C:
			if len(queue) == 0 {
				continue
			}
			n := heap.Pop(&queue).(*scheduled)
			nextAllowed = time.Now().Add(interval)
			opCtx, cancel := context.WithTimeout(ctx, p.cfg.RequestTimeout)
			fresh, err := p.store.Refresh(opCtx, n.credential, refresher)
			cancel()
			if ctx.Err() != nil {
				return
			}
			switch {
			case errors.Is(err, ErrChanged):
				continue // next reload obtains current credentials
			case err != nil:
				p.recordRefreshFailure(n, err, provider, &consecutiveFailures, &circuitUntil)
			default:
				p.recordRefreshSuccess(n, fresh, nextAllowed, &consecutiveFailures, &circuitUntil)
			}
			heap.Push(&queue, n)
		}
	}
}
