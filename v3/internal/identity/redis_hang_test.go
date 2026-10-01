package identity

import (
	"context"
	"testing"
	"time"
)

// hangingRemote behaves like a paused Redis: every call blocks until its
// context expires.
type hangingRemote struct{ *fakeRemote }

func (hangingRemote) get(ctx context.Context, _ [32]byte) (*KeyProfile, bool, error) {
	<-ctx.Done()
	return nil, false, ctx.Err()
}

func (hangingRemote) clock(ctx context.Context) (int64, error) {
	<-ctx.Done()
	return 0, ctx.Err()
}

func (hangingRemote) put(ctx context.Context, _ [32]byte, _ *KeyProfile, _ int64) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}

// Regression (60 s Redis pause e2e, 2026-09-30): a hung L2 read used the
// whole LoadTimeout, so the PostgreSQL fallback started with an expired
// context and every L1 miss failed with 503.
func TestHungRedisStillFallsBackToPostgres(t *testing.T) {
	c := newClock()
	cfg := Config{Now: c.now}.withDefaults()
	db := newDB()
	db.add("sk-a", alice())
	db.add("sk-b", KeyProfile{KeyID: 11, UserID: 2, Group: "default"})
	a := newAuthorizer(cfg, hangingRemote{newRemote()}, db, discardLogger())

	start := time.Now()
	if _, err := a.Authorize(ctx, "sk-a"); err != nil {
		t.Fatalf("first lookup with hung redis = %v; want postgres fallback", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("first lookup took %v; want about one RedisTimeout (250ms)", d)
	}

	// Within the backoff window Redis is skipped entirely.
	start = time.Now()
	if _, err := a.Authorize(ctx, "sk-b"); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("lookup inside backoff took %v; redis should have been skipped", d)
	}

	// After the backoff Redis is probed again (and fails fast again).
	c.advance(2 * time.Second)
	a.l1.remove(string(func() []byte { h := HashKey("sk-a"); return h[:] }()))
	if _, err := a.Authorize(ctx, "sk-a"); err != nil {
		t.Fatalf("lookup after backoff = %v", err)
	}
	if n := db.loads.Load(); n != 3 {
		t.Fatalf("postgres loads = %d; want 3", n)
	}
}
