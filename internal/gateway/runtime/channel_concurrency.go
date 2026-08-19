package runtime

import "sync"

var channelConcurrency = struct {
	sync.RWMutex
	active map[int]int
	users  map[int]map[int]int
}{
	active: make(map[int]int),
	users:  make(map[int]map[int]int),
}

var loadSharedChannelConcurrency = sharedActiveChannelRequestsForChannels
var reserveSharedChannelConcurrency = reserveRedisChannelConcurrency

// BeginChannelRequest tracks one process-local in-flight upstream request.
func BeginChannelRequest(channelID int) func() {
	release, _ := TryBeginChannelRequest(channelID, 0)
	return release
}

// TryBeginChannelRequest reserves one process-local channel slot. A positive
// limit is enforced atomically; zero disables the per-channel gate for legacy
// and official channels.
func TryBeginChannelRequest(channelID, limit int) (func(), bool) {
	return TryBeginChannelRequestForUser(channelID, 0, limit, 0)
}

// TryBeginChannelRequestForUser atomically enforces both the channel-wide and
// per-user Marketplace limits across Gateway instances. Zero disables the
// corresponding limit.
func TryBeginChannelRequestForUser(channelID, userID, totalLimit, userLimit int) (func(), bool) {
	if channelID <= 0 {
		return func() {}, true
	}
	sharedRelease, sharedEnforced, sharedAdmitted := reserveSharedChannelConcurrency(
		channelID,
		userID,
		totalLimit,
		userLimit,
	)
	if !sharedAdmitted {
		return func() {}, false
	}
	channelConcurrency.Lock()
	if !sharedEnforced && totalLimit > 0 && channelConcurrency.active[channelID] >= totalLimit {
		channelConcurrency.Unlock()
		return func() {}, false
	}
	userActive := channelConcurrency.users[channelID]
	if !sharedEnforced && userLimit > 0 && userActive[userID] >= userLimit {
		channelConcurrency.Unlock()
		return func() {}, false
	}
	if userActive == nil {
		userActive = make(map[int]int)
		channelConcurrency.users[channelID] = userActive
	}
	channelConcurrency.active[channelID]++
	userActive[userID]++
	channelConcurrency.Unlock()
	notifyChannelConcurrencyChanged()
	var once sync.Once
	return func() {
		once.Do(func() {
			channelConcurrency.Lock()
			if channelConcurrency.active[channelID] <= 1 {
				delete(channelConcurrency.active, channelID)
			} else {
				channelConcurrency.active[channelID]--
			}
			if userActive := channelConcurrency.users[channelID]; userActive != nil {
				if userActive[userID] <= 1 {
					delete(userActive, userID)
				} else {
					userActive[userID]--
				}
				if len(userActive) == 0 {
					delete(channelConcurrency.users, channelID)
				}
			}
			channelConcurrency.Unlock()
			notifyChannelConcurrencyChanged()
			sharedRelease()
		})
	}, true
}

// ActiveChannelUserRequests returns this process's in-flight count for one
// user on one channel. Hard admission uses this value under the same lock.
func ActiveChannelUserRequests(channelID, userID int) int {
	channelConcurrency.RLock()
	defer channelConcurrency.RUnlock()
	return channelConcurrency.users[channelID][userID]
}

// ActiveChannelRequests returns the cross-process in-flight count when shared
// telemetry is available, otherwise it falls back to this process.
func ActiveChannelRequests(channelID int) int {
	return ActiveChannelRequestsForChannels([]int{channelID})[channelID]
}

// ActiveChannelRequestsForChannels returns one aggregated snapshot for the
// requested channels without issuing one Redis query per Marketplace group.
func ActiveChannelRequestsForChannels(channelIDs []int) map[int]int {
	local := localActiveChannelRequestsForChannels(channelIDs)
	if shared, ok := loadSharedChannelConcurrency(channelIDs, local); ok {
		return shared
	}
	return local
}

func localActiveChannelRequestsForChannels(channelIDs []int) map[int]int {
	result := make(map[int]int, len(channelIDs))
	channelConcurrency.RLock()
	defer channelConcurrency.RUnlock()
	for _, channelID := range channelIDs {
		if channelID > 0 {
			result[channelID] = channelConcurrency.active[channelID]
		}
	}
	return result
}

func snapshotLocalChannelConcurrency() map[int]int {
	channelConcurrency.RLock()
	defer channelConcurrency.RUnlock()
	result := make(map[int]int, len(channelConcurrency.active))
	for channelID, active := range channelConcurrency.active {
		if channelID > 0 && active > 0 {
			result[channelID] = active
		}
	}
	return result
}
