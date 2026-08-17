package runtime

import "sync"

var channelConcurrency = struct {
	sync.RWMutex
	active map[int]int
}{active: make(map[int]int)}

// BeginChannelRequest tracks one process-local in-flight upstream request.
func BeginChannelRequest(channelID int) func() {
	if channelID <= 0 {
		return func() {}
	}
	channelConcurrency.Lock()
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
	}
}

// ActiveChannelRequests returns the current process-local in-flight count.
func ActiveChannelRequests(channelID int) int {
	channelConcurrency.RLock()
	defer channelConcurrency.RUnlock()
	return channelConcurrency.active[channelID]
}
