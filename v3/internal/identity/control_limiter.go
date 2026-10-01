package identity

import (
	"sync"
	"time"
)

type authAttempts struct {
	until time.Time
	n     int
}
type attemptLimiter struct {
	mu      sync.Mutex
	entries map[string]authAttempts
	now     func() time.Time
}

func newAttemptLimiter(now func() time.Time) *attemptLimiter {
	return &attemptLimiter{entries: make(map[string]authAttempts), now: now}
}
func (l *attemptLimiter) allow(address string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	v, ok := l.entries[address]
	if !ok || !now.Before(v.until) {
		if len(l.entries) >= 4096 {
			for key, value := range l.entries {
				if !now.Before(value.until) {
					delete(l.entries, key)
				}
			}
			if len(l.entries) >= 4096 {
				return false
			}
		}
		v = authAttempts{until: now.Add(time.Minute)}
	}
	if v.n >= 10 {
		return false
	}
	v.n++
	l.entries[address] = v
	return true
}
