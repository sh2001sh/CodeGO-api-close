package identity

import (
	"container/list"
	"hash/maphash"
	"sync"
)

const lruShards = 16

// lru is a sharded, bounded cache with per-entry expiry. A nil value is a
// negative entry (the key is known not to exist). onEvict is called, under no
// lock, for positive entries that leave the cache for any reason.
type lru struct {
	seed    maphash.Seed
	shards  [lruShards]lruShard
	onEvict func(hash string, p *KeyProfile)
}

type lruShard struct {
	mu  sync.Mutex
	max int
	ll  *list.List
	m   map[string]*list.Element
}

type lruEntry struct {
	hash    string
	profile *KeyProfile
	expires int64 // unix nanos
}

func newLRU(maxEntries int, onEvict func(string, *KeyProfile)) *lru {
	c := &lru{seed: maphash.MakeSeed(), onEvict: onEvict}
	per := max(1, maxEntries/lruShards)
	for i := range c.shards {
		c.shards[i] = lruShard{max: per, ll: list.New(), m: make(map[string]*list.Element)}
	}
	return c
}

func (c *lru) shard(hash string) *lruShard {
	return &c.shards[maphash.String(c.seed, hash)%lruShards]
}

// get returns the entry and whether it was present and unexpired.
func (c *lru) get(hash string, now int64) (*KeyProfile, bool) {
	s := c.shard(hash)
	s.mu.Lock()
	el, ok := s.m[hash]
	if !ok {
		s.mu.Unlock()
		return nil, false
	}
	e := el.Value.(*lruEntry)
	if e.expires <= now {
		s.ll.Remove(el)
		delete(s.m, hash)
		s.mu.Unlock()
		c.evicted(e)
		return nil, false
	}
	s.ll.MoveToFront(el)
	s.mu.Unlock()
	return e.profile, true
}

func (c *lru) put(hash string, p *KeyProfile, expires int64) {
	s := c.shard(hash)
	s.mu.Lock()
	var dropped []*lruEntry
	if el, ok := s.m[hash]; ok {
		e := el.Value.(*lruEntry)
		if e.profile != nil && e.profile != p {
			dropped = append(dropped, &lruEntry{hash: hash, profile: e.profile})
		}
		e.profile, e.expires = p, expires
		s.ll.MoveToFront(el)
	} else {
		s.m[hash] = s.ll.PushFront(&lruEntry{hash: hash, profile: p, expires: expires})
	}
	for s.ll.Len() > s.max {
		oldest := s.ll.Back()
		s.ll.Remove(oldest)
		e := oldest.Value.(*lruEntry)
		delete(s.m, e.hash)
		dropped = append(dropped, e)
	}
	s.mu.Unlock()
	for _, e := range dropped {
		c.evicted(e)
	}
}

func (c *lru) remove(hash string) {
	s := c.shard(hash)
	s.mu.Lock()
	el, ok := s.m[hash]
	if ok {
		s.ll.Remove(el)
		delete(s.m, hash)
	}
	s.mu.Unlock()
	if ok {
		c.evicted(el.Value.(*lruEntry))
	}
}

func (c *lru) evicted(e *lruEntry) {
	if e.profile != nil && c.onEvict != nil {
		c.onEvict(e.hash, e.profile)
	}
}
