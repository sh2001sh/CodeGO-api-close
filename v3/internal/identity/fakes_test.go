package identity

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// fakeRemote mirrors the L2 contract, including the tombstone fence: clock()
// is a logical counter, invalidations record it, and put rejects any load
// that started at or before the latest invalidation of its key or user.
type fakeRemote struct {
	mu       sync.Mutex
	entries  map[[32]byte]*KeyProfile
	puts     int
	fenced   bool // force put to report "invalidated while loading"
	down     bool
	keyInv   []int64
	userInv  []int64
	tick     int64
	keyTomb  map[int64]int64
	userTomb map[int64]int64

	onInvalidate func()
}

func newRemote() *fakeRemote {
	return &fakeRemote{entries: map[[32]byte]*KeyProfile{}, keyTomb: map[int64]int64{}, userTomb: map[int64]int64{}}
}

var errDown = errors.New("redis down")

func (r *fakeRemote) get(_ context.Context, h [32]byte) (*KeyProfile, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.down {
		return nil, false, errDown
	}
	p, ok := r.entries[h]
	return p, ok, nil
}

func (r *fakeRemote) put(_ context.Context, h [32]byte, p *KeyProfile, loadStart int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.puts++
	if r.fenced || r.keyTomb[p.KeyID] >= loadStart || r.userTomb[p.UserID] >= loadStart {
		return false, nil
	}
	r.entries[h] = p
	return true, nil
}

func (r *fakeRemote) clock(context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.down {
		return 0, errDown
	}
	r.tick++
	return r.tick, nil
}

func (r *fakeRemote) invalidateKey(_ context.Context, id int64) error {
	r.mu.Lock()
	r.tick++
	r.keyTomb[id] = r.tick
	r.keyInv = append(r.keyInv, id)
	for h, p := range r.entries {
		if p.KeyID == id {
			delete(r.entries, h)
		}
	}
	r.mu.Unlock()
	return nil
}

func (r *fakeRemote) invalidateUser(_ context.Context, id int64) error {
	if r.onInvalidate != nil {
		r.onInvalidate() // runs before L2 is cleared, like a concurrent lookup
	}
	r.mu.Lock()
	r.tick++
	r.userTomb[id] = r.tick
	r.userInv = append(r.userInv, id)
	for h, p := range r.entries {
		if p.UserID == id {
			delete(r.entries, h)
		}
	}
	r.mu.Unlock()
	return nil
}

func (r *fakeRemote) putCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.puts
}

// fakeDB serves profiles by plaintext key; gate, when set, blocks every load.
type fakeDB struct {
	mu     sync.Mutex
	byHash map[[32]byte]KeyProfile
	loads  atomic.Int64
	gate   chan struct{}
	during func() // runs inside load, e.g. to race an invalidation
}

func newDB() *fakeDB { return &fakeDB{byHash: map[[32]byte]KeyProfile{}} }

func (d *fakeDB) add(key string, p KeyProfile) {
	d.mu.Lock()
	d.byHash[HashKey(key)] = p
	d.mu.Unlock()
}

func (d *fakeDB) load(ctx context.Context, h [32]byte) (*KeyProfile, error) {
	d.loads.Add(1)
	if d.gate != nil {
		select {
		case <-d.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if d.during != nil {
		d.during()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	p, ok := d.byHash[h]
	if !ok {
		return nil, nil
	}
	return &p, nil
}

type clock struct{ ns atomic.Int64 }

func newClock() *clock {
	c := &clock{}
	c.ns.Store(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC).UnixNano())
	return c
}

func (c *clock) now() time.Time          { return time.Unix(0, c.ns.Load()) }
func (c *clock) advance(d time.Duration) { c.ns.Add(int64(d)) }

func newTestAuthorizer(cfg Config) (*Authorizer, *fakeRemote, *fakeDB, *clock) {
	c := newClock()
	cfg.Now = c.now
	r, db := newRemote(), newDB()
	return newAuthorizer(cfg.withDefaults(), r, db, discardLogger()), r, db, c
}
