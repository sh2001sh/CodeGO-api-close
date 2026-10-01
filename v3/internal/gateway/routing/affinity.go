package routing

import (
	"container/list"
	"encoding/binary"
	"hash/maphash"
	"sync"

	"github.com/tidwall/gjson"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// affinity remembers which credential served a session, so follow-up turns
// land on the same upstream account and keep its prompt cache warm. It is a
// bounded LRU with a TTL; entries are preferences, never pins.
type affinity struct {
	mu   sync.Mutex
	ttl  int64
	max  int
	ll   *list.List
	m    map[uint64]*list.Element
	seed maphash.Seed
}

type affinityEntry struct {
	key     uint64
	cred    int64
	expires int64
}

func newAffinity(maxEntries int, ttl int64) *affinity {
	return &affinity{ttl: ttl, max: maxEntries, ll: list.New(), m: make(map[uint64]*list.Element), seed: maphash.MakeSeed()}
}

// sessionKey derives the session identity from the body, scoped to the API
// key so two tenants reusing the same user string never share an entry.
func (a *affinity) sessionKey(req *gateway.Request) (uint64, bool) {
	res := gjson.GetManyBytes(req.Body, "metadata.user_id", "user", "prompt_cache_key")
	for _, r := range res {
		if s := r.String(); s != "" {
			var h maphash.Hash
			h.SetSeed(a.seed)
			var id [8]byte
			binary.LittleEndian.PutUint64(id[:], uint64(req.Principal.KeyID))
			_, _ = h.Write(id[:])
			_, _ = h.WriteString(s)
			return h.Sum64(), true
		}
	}
	return 0, false
}

func (a *affinity) get(key uint64, now int64) (int64, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	el, ok := a.m[key]
	if !ok {
		return 0, false
	}
	e := el.Value.(*affinityEntry)
	if e.expires <= now {
		a.ll.Remove(el)
		delete(a.m, key)
		return 0, false
	}
	a.ll.MoveToFront(el)
	return e.cred, true
}

func (a *affinity) put(key uint64, cred int64, now int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if el, ok := a.m[key]; ok {
		e := el.Value.(*affinityEntry)
		e.cred, e.expires = cred, now+a.ttl
		a.ll.MoveToFront(el)
		return
	}
	a.m[key] = a.ll.PushFront(&affinityEntry{key: key, cred: cred, expires: now + a.ttl})
	for a.ll.Len() > a.max {
		oldest := a.ll.Back()
		a.ll.Remove(oldest)
		delete(a.m, oldest.Value.(*affinityEntry).key)
	}
}
