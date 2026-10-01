package identity

import "sync"

// index maps key and user ids to the L1 hashes they own, so an invalidation
// that carries only an id can drop the right entries. It is kept in step with
// the LRU through the eviction callback.
type index struct {
	mu     sync.Mutex
	byKey  map[int64]string
	byUser map[int64]map[string]struct{}
}

func newIndex() *index {
	return &index{byKey: make(map[int64]string), byUser: make(map[int64]map[string]struct{})}
}

func (x *index) add(hash string, p *KeyProfile) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.byKey[p.KeyID] = hash
	set := x.byUser[p.UserID]
	if set == nil {
		set = make(map[string]struct{})
		x.byUser[p.UserID] = set
	}
	set[hash] = struct{}{}
}

func (x *index) drop(hash string, p *KeyProfile) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.byKey[p.KeyID] == hash {
		delete(x.byKey, p.KeyID)
	}
	if set := x.byUser[p.UserID]; set != nil {
		delete(set, hash)
		if len(set) == 0 {
			delete(x.byUser, p.UserID)
		}
	}
}

func (x *index) key(keyID int64) (string, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	h, ok := x.byKey[keyID]
	return h, ok
}

func (x *index) user(userID int64) []string {
	x.mu.Lock()
	defer x.mu.Unlock()
	hashes := make([]string, 0, len(x.byUser[userID]))
	for h := range x.byUser[userID] {
		hashes = append(hashes, h)
	}
	return hashes
}
