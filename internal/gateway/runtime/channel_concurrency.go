package runtime

import "sync"

var channelConcurrency = struct {
	sync.RWMutex
	active map[int]int
}{active: make(map[int]int)}

// BeginChannelRequest tracks one process-local in-flight upstream request.
func BeginChannelRequest(channelID int) func() {
	release, _ := TryBeginChannelRequest(channelID, 0)
	return release
}

// TryBeginChannelRequest reserves one process-local channel slot. A positive
// limit is enforced atomically; zero disables the per-channel gate for legacy
// and official channels.
func TryBeginChannelRequest(channelID, limit int) (func(), bool) {
	if channelID <= 0 {
		return func() {}, true
	}
	channelConcurrency.Lock()
	if limit > 0 && channelConcurrency.active[channelID] >= limit {
		channelConcurrency.Unlock()
		return func() {}, false
	}
	channelConcurrency.active[channelID]++
	channelConcurrency.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			channelConcurrency.Lock()
			if channelConcurrency.active[channelID] <= 1 {
				delete(channelConcurrency.active, channelID)
			} else {
				channelConcurrency.active[channelID]--
			}
			channelConcurrency.Unlock()
		})
	}, true
}

// ActiveChannelRequests returns the current process-local in-flight count.
func ActiveChannelRequests(channelID int) int {
	channelConcurrency.RLock()
	defer channelConcurrency.RUnlock()
	return channelConcurrency.active[channelID]
}
