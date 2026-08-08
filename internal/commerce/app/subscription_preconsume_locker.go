package app

import "sync"

var subscriptionPreConsumeLocks = newUserLockManager()

type userLockManager struct {
	mu      sync.Mutex
	entries map[int]*userLockEntry
}

type userLockEntry struct {
	mu   sync.Mutex
	refs int
}

func newUserLockManager() *userLockManager {
	return &userLockManager{entries: make(map[int]*userLockEntry)}
}

// Lock serializes subscription pre-consume transactions for one user only.
// Entries are removed when no caller holds or waits for their lock.
func (manager *userLockManager) Lock(userID int) func() {
	manager.mu.Lock()
	entry := manager.entries[userID]
	if entry == nil {
		entry = &userLockEntry{}
		manager.entries[userID] = entry
	}
	entry.refs++
	manager.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()

		manager.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(manager.entries, userID)
		}
		manager.mu.Unlock()
	}
}
