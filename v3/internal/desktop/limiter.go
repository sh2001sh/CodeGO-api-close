package desktop

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type attempt struct {
	at    time.Time
	count int
}
type limiter struct {
	mu      sync.Mutex
	entries map[string]attempt
}

func (l *limiter) allow(r *http.Request, now time.Time) bool {
	addr, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		addr = r.RemoteAddr
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = map[string]attempt{}
	}
	entry := l.entries[addr]
	if now.Sub(entry.at) >= time.Minute {
		entry = attempt{at: now}
	}
	if len(l.entries) >= 4096 {
		for key, value := range l.entries {
			if now.Sub(value.at) >= time.Minute {
				delete(l.entries, key)
			}
		}
		if _, exists := l.entries[addr]; !exists && len(l.entries) >= 4096 {
			return false
		}
	}
	if entry.count >= 60 {
		return false
	}
	entry.count++
	l.entries[addr] = entry
	return true
}
