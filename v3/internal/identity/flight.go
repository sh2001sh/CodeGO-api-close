package identity

import "sync"

// flight collapses concurrent loads of the same key into one call. It is a
// minimal singleflight; golang.org/x/sync is not a v3 dependency.
type flight struct {
	mu    sync.Mutex
	calls map[string]*call
}

type call struct {
	done    chan struct{}
	profile *KeyProfile
	err     error
}

// do runs fn once per key among concurrent callers and shares its result.
func (f *flight) do(key string, fn func() (*KeyProfile, error)) (*KeyProfile, error) {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = make(map[string]*call)
	}
	if c, ok := f.calls[key]; ok {
		f.mu.Unlock()
		<-c.done
		return c.profile, c.err
	}
	c := &call{done: make(chan struct{})}
	f.calls[key] = c
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		delete(f.calls, key)
		f.mu.Unlock()
		close(c.done)
	}()
	c.profile, c.err = fn()
	return c.profile, c.err
}
