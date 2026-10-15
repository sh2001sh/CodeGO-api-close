package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	allCoolingFallbackSlots = 3
	allCoolingFallbackWait  = 200 * time.Millisecond
)

const allCoolingFallbackLeasesContextKey = "all_cooling_fallback_leases"

type allCoolingFallbackState struct {
	active  int
	waiters int
	changed chan struct{}
}

var allCoolingFallbacks = struct {
	sync.Mutex
	states map[string]*allCoolingFallbackState
}{states: make(map[string]*allCoolingFallbackState)}

// AcquireAllCoolingFallback reserves one bounded process-local slot for a
// request that must use a still-cooling route because no healthy route remains.
// It waits at most 200 ms so a genuine upstream outage cannot grow a long queue.
func AcquireAllCoolingFallback(c *gin.Context, group, model string, requestTypes ...RequestType) bool {
	if c == nil || model == "" {
		return true
	}
	key := allCoolingFallbackKey(group, model, normalizedRequestType(requestTypes...))
	if hasAllCoolingFallbackLease(c, key) {
		return true
	}
	ctx := context.Background()
	if c.Request != nil {
		ctx = c.Request.Context()
	}
	state, acquired := acquireAllCoolingFallback(ctx, key)
	if !acquired {
		return false
	}
	registerAllCoolingFallbackLease(c, key, func() { releaseAllCoolingFallback(key, state) })
	return true
}

// ReleaseAllCoolingFallbacks releases every fallback slot reserved by a request.
// It is safe to call from both middleware and a relay cleanup defer.
func ReleaseAllCoolingFallbacks(c *gin.Context) {
	if c == nil {
		return
	}
	value, found := c.Get(allCoolingFallbackLeasesContextKey)
	if !found {
		return
	}
	leases, ok := value.(map[string]func())
	if !ok {
		return
	}
	c.Set(allCoolingFallbackLeasesContextKey, map[string]func(){})
	for _, release := range leases {
		release()
	}
}

func acquireAllCoolingFallback(ctx context.Context, key string) (*allCoolingFallbackState, bool) {
	timer := time.NewTimer(allCoolingFallbackWait)
	defer timer.Stop()
	for {
		allCoolingFallbacks.Lock()
		state := allCoolingFallbacks.states[key]
		if state == nil {
			state = &allCoolingFallbackState{changed: make(chan struct{})}
			allCoolingFallbacks.states[key] = state
		}
		if state.active < allCoolingFallbackSlots {
			state.active++
			allCoolingFallbacks.Unlock()
			return state, true
		}
		state.waiters++
		changed := state.changed
		allCoolingFallbacks.Unlock()

		select {
		case <-changed:
		case <-timer.C:
			releaseAllCoolingFallbackWaiter(key, state)
			return nil, false
		case <-ctx.Done():
			releaseAllCoolingFallbackWaiter(key, state)
			return nil, false
		}
		releaseAllCoolingFallbackWaiter(key, state)
	}
}

func releaseAllCoolingFallback(key string, state *allCoolingFallbackState) {
	allCoolingFallbacks.Lock()
	defer allCoolingFallbacks.Unlock()
	if state == nil || state.active == 0 {
		return
	}
	state.active--
	close(state.changed)
	state.changed = make(chan struct{})
	removeAllCoolingFallbackState(key, state)
}

func releaseAllCoolingFallbackWaiter(key string, state *allCoolingFallbackState) {
	allCoolingFallbacks.Lock()
	defer allCoolingFallbacks.Unlock()
	if state == nil || state.waiters == 0 {
		return
	}
	state.waiters--
	removeAllCoolingFallbackState(key, state)
}

func removeAllCoolingFallbackState(key string, state *allCoolingFallbackState) {
	if state.active == 0 && state.waiters == 0 && allCoolingFallbacks.states[key] == state {
		delete(allCoolingFallbacks.states, key)
	}
}

func allCoolingFallbackKey(group, model string, requestType RequestType) string {
	return group + "\x00" + model + "\x00" + string(requestType)
}

func hasAllCoolingFallbackLease(c *gin.Context, key string) bool {
	value, found := c.Get(allCoolingFallbackLeasesContextKey)
	if !found {
		return false
	}
	leases, ok := value.(map[string]func())
	if !ok {
		return false
	}
	_, found = leases[key]
	return found
}

func registerAllCoolingFallbackLease(c *gin.Context, key string, release func()) {
	value, found := c.Get(allCoolingFallbackLeasesContextKey)
	leases, _ := value.(map[string]func())
	if !found || leases == nil {
		leases = make(map[string]func())
		c.Set(allCoolingFallbackLeasesContextKey, leases)
	}
	leases[key] = release
}
