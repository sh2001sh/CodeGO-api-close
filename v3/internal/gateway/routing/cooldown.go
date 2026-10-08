package routing

import (
	"sync"
	"sync/atomic"
	"time"
)

// coolKey scopes global cooldowns to a credential or credential/model pair.
// A nonempty pool instead scopes a user-configured cooldown to that pool's
// channel/model pair; hard marks auth/model-unavailable failures that cannot be probed.
type coolKey struct {
	cred  int64
	model string
	pool  string
	hard  bool
}

type coolState struct {
	until  int64 // unix nanos
	streak int   // consecutive failures, drives exponential backoff
}

const coolShards = 64

// cooldowns is a sharded map of active cooldowns. latest is the furthest
// expiry ever set: while now is past it no cooldown can be active, so the
// common healthy path skips every lock.
type cooldowns struct {
	shards [coolShards]struct {
		mu sync.Mutex
		m  map[coolKey]*coolState
	}
	latest atomic.Int64
}

func newCooldowns() *cooldowns {
	c := &cooldowns{}
	for i := range c.shards {
		c.shards[i].m = make(map[coolKey]*coolState)
	}
	return c
}

func (c *cooldowns) shard(k coolKey) *struct {
	mu sync.Mutex
	m  map[coolKey]*coolState
} {
	return &c.shards[uint64(k.cred)%coolShards]
}

// until returns when k stops cooling, or 0 if it is usable at now.
func (c *cooldowns) until(k coolKey, now int64) int64 {
	if now >= c.latest.Load() {
		return 0
	}
	s := c.shard(k)
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.m[k]; st != nil && st.until > now {
		return st.until
	}
	return 0
}

// fail extends k's cooldown by the duration chosen for the new streak. A
// cooldown never shrinks, so a late report cannot shorten a newer one.
func (c *cooldowns) fail(k coolKey, now int64, duration func(streak int) time.Duration) {
	s := c.shard(k)
	s.mu.Lock()
	st := s.m[k]
	if st == nil {
		st = &coolState{}
		s.m[k] = st
	}
	st.streak++
	until := now + int64(duration(st.streak))
	if until > st.until {
		st.until = until
	}
	until = st.until
	s.mu.Unlock()

	for {
		cur := c.latest.Load()
		if until <= cur || c.latest.CompareAndSwap(cur, until) {
			return
		}
	}
}

// success resets the failure streak. It deliberately leaves an active
// cooldown in place: a success that started before a newer failure must not
// resurrect the credential early; cooldowns end by time.
func (c *cooldowns) success(k coolKey) {
	s := c.shard(k)
	s.mu.Lock()
	if st := s.m[k]; st != nil {
		st.streak = 0
	}
	s.mu.Unlock()
}
