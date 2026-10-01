package limits

import (
	"container/list"
	"sync"
	"time"
)

type FailureConfig struct {
	MaxFailures  int
	Window       time.Duration
	MaxAddresses int
	Now          func() time.Time
}

type failure struct {
	address string
	count   int
	expires time.Time
}

// AuthFailures limits failed authentication by client address before another
// expensive authorization lookup. Its bounded LRU tracks failures only.
type AuthFailures struct {
	mu      sync.Mutex
	cfg     FailureConfig
	entries map[string]*list.Element
	order   *list.List
}

func NewAuthFailures(cfg FailureConfig) *AuthFailures {
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = 30
	}
	if cfg.Window <= 0 {
		cfg.Window = time.Minute
	}
	if cfg.MaxAddresses <= 0 {
		cfg.MaxAddresses = 16384
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &AuthFailures{cfg: cfg, entries: make(map[string]*list.Element), order: list.New()}
}

func (a *AuthFailures) Blocked(address string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	e := a.entries[address]
	if e == nil {
		return false
	}
	f := e.Value.(*failure)
	if !a.cfg.Now().Before(f.expires) {
		delete(a.entries, address)
		a.order.Remove(e)
		return false
	}
	return f.count >= a.cfg.MaxFailures
}

func (a *AuthFailures) Failed(address string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.cfg.Now()
	if e := a.entries[address]; e != nil {
		f := e.Value.(*failure)
		if !now.Before(f.expires) {
			f.count = 0
			f.expires = now.Add(a.cfg.Window)
		}
		f.count++
		a.order.MoveToFront(e)
		return
	}
	if len(a.entries) >= a.cfg.MaxAddresses {
		e := a.order.Back()
		delete(a.entries, e.Value.(*failure).address)
		a.order.Remove(e)
	}
	e := a.order.PushFront(&failure{address: address, count: 1, expires: now.Add(a.cfg.Window)})
	a.entries[address] = e
}
