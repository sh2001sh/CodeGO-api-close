package identity

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

var ctx = context.Background()

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func alice() KeyProfile { return KeyProfile{KeyID: 10, UserID: 1, Group: "default"} }

func TestL1HitSkipsLowerTiers(t *testing.T) {
	a, _, db, _ := newTestAuthorizer(Config{})
	db.add("sk-a", alice())
	for i := 0; i < 5; i++ {
		p, err := a.Authorize(ctx, "sk-a")
		if err != nil || p.UserID != 1 || p.KeyID != 10 || p.Group != "default" {
			t.Fatalf("Authorize = %+v, %v", p, err)
		}
	}
	if n := db.loads.Load(); n != 1 {
		t.Fatalf("db loads = %d; want 1", n)
	}
}

func TestL2HitAvoidsDatabase(t *testing.T) {
	a, r, db, _ := newTestAuthorizer(Config{})
	p := alice()
	r.entries[HashKey("sk-a")] = &p
	if _, err := a.Authorize(ctx, "sk-a"); err != nil {
		t.Fatal(err)
	}
	if db.loads.Load() != 0 {
		t.Fatal("L2 hit still loaded from postgres")
	}
}

func TestNegativeCacheStaysLocal(t *testing.T) {
	a, r, db, c := newTestAuthorizer(Config{})
	for i := 0; i < 1000; i++ {
		if _, err := a.Authorize(ctx, "sk-random-"+strconv.Itoa(i%10)); !errors.Is(err, gateway.ErrInvalidKey) {
			t.Fatalf("err = %v", err)
		}
	}
	if db.loads.Load() != 10 || r.putCount() != 0 {
		t.Fatalf("loads=%d puts=%d; want 10 loads and 0 redis writes", db.loads.Load(), r.putCount())
	}
	c.advance(31 * time.Second) // negative TTL passed
	_, _ = a.Authorize(ctx, "sk-random-0")
	if db.loads.Load() != 11 {
		t.Fatal("negative entry did not expire")
	}
}

func TestSingleflightCollapsesMisses(t *testing.T) {
	a, _, db, _ := newTestAuthorizer(Config{})
	db.add("sk-a", alice())
	db.gate = make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := a.Authorize(ctx, "sk-a")
			errs <- err
		}()
	}
	time.Sleep(50 * time.Millisecond) // let every goroutine join the flight
	close(db.gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := db.loads.Load(); n != 1 {
		t.Fatalf("100 concurrent misses caused %d loads; want 1", n)
	}
}

// holdSlot occupies the only database slot until the returned release is called.
func holdSlot(t *testing.T, a *Authorizer, db *fakeDB) (release func()) {
	t.Helper()
	db.add("sk-hold", alice())
	db.gate = make(chan struct{})
	done := make(chan struct{})
	go func() { _, _ = a.Authorize(ctx, "sk-hold"); close(done) }()
	for db.loads.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	return func() { close(db.gate); <-done }
}

func TestSemaphoreFailsFastWhenQueueFull(t *testing.T) {
	a, _, db, _ := newTestAuthorizer(Config{MaxDBConcurrency: 1, MaxDBWaiters: 1, DBWait: 5 * time.Second})
	release := holdSlot(t, a, db)
	defer release()
	a.waiters.Store(1) // the queue is already full
	start := time.Now()
	_, err := a.Authorize(ctx, "sk-b")
	if !errors.Is(err, gateway.ErrAuthUnavailable) || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("full queue = %v after %v; want fast ErrAuthUnavailable", err, time.Since(start))
	}
	a.waiters.Store(0)
}

func TestSemaphoreWaitsBrieflyForSlot(t *testing.T) {
	a, _, db, _ := newTestAuthorizer(Config{MaxDBConcurrency: 1, DBWait: 2 * time.Second})
	release := holdSlot(t, a, db)
	db.add("sk-b", KeyProfile{KeyID: 11, UserID: 2})
	time.AfterFunc(50*time.Millisecond, release)
	if _, err := a.Authorize(ctx, "sk-b"); err != nil {
		t.Fatalf("queued lookup failed instead of waiting for the slot: %v", err)
	}
	if a.waiters.Load() != 0 {
		t.Fatalf("waiters leaked: %d", a.waiters.Load())
	}
}

func TestSemaphoreGivesUpAfterWait(t *testing.T) {
	a, _, db, _ := newTestAuthorizer(Config{MaxDBConcurrency: 1, DBWait: 30 * time.Millisecond})
	release := holdSlot(t, a, db)
	defer release()
	db.add("sk-b", KeyProfile{KeyID: 11, UserID: 2})
	start := time.Now()
	_, err := a.Authorize(ctx, "sk-b")
	if !errors.Is(err, gateway.ErrAuthUnavailable) || time.Since(start) > time.Second {
		t.Fatalf("saturated lookup = %v after %v; want ErrAuthUnavailable after ~30ms", err, time.Since(start))
	}
}

func TestExpiryAndRevocationCheckedAtRead(t *testing.T) {
	a, _, db, c := newTestAuthorizer(Config{L1TTL: time.Hour})
	p := alice()
	p.ExpiresAt = c.now().Add(time.Minute)
	db.add("sk-a", p)
	db.add("sk-off", KeyProfile{KeyID: 12, UserID: 3, Revoked: true})
	if _, err := a.Authorize(ctx, "sk-a"); err != nil {
		t.Fatal(err)
	}
	c.advance(2 * time.Minute)
	if _, err := a.Authorize(ctx, "sk-a"); !errors.Is(err, gateway.ErrInvalidKey) {
		t.Fatalf("expired cached key = %v", err)
	}
	if db.loads.Load() != 1 {
		t.Fatal("expiry should be decided from the cached profile, not a reload")
	}
	if _, err := a.Authorize(ctx, "sk-off"); !errors.Is(err, gateway.ErrInvalidKey) {
		t.Fatalf("revoked key = %v", err)
	}
}

func TestInvalidationDropsKeyAndUser(t *testing.T) {
	a, r, db, _ := newTestAuthorizer(Config{})
	db.add("sk-a", alice())
	db.add("sk-a2", KeyProfile{KeyID: 20, UserID: 1, Group: "default"})
	db.add("sk-b", KeyProfile{KeyID: 30, UserID: 2, Group: "default"})
	for _, k := range []string{"sk-a", "sk-a2", "sk-b"} {
		if _, err := a.Authorize(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	db.add("sk-a", KeyProfile{KeyID: 10, UserID: 1, Group: "vip"})
	if err := a.Invalidate(ctx, "api_key:10"); err != nil {
		t.Fatal(err)
	}
	if p, _ := a.Authorize(ctx, "sk-a"); p.Group != "vip" {
		t.Fatalf("key invalidation not applied: group %q", p.Group)
	}
	before := db.loads.Load()
	if err := a.Invalidate(ctx, "user:1"); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"sk-a", "sk-a2", "sk-b"} {
		_, _ = a.Authorize(ctx, k)
	}
	if got := db.loads.Load() - before; got != 2 {
		t.Fatalf("user invalidation reloaded %d keys; want exactly user 1's 2 keys", got)
	}
	if len(r.keyInv) != 1 || len(r.userInv) != 1 {
		t.Fatalf("L2 invalidations key=%v user=%v", r.keyInv, r.userInv)
	}
	if err := a.Invalidate(ctx, "catalog:5"); err != nil {
		t.Fatal("unrelated entities must be ignored")
	}
}

// Regression (found by -count=10 in acceptance): a lookup that runs while an
// invalidation is clearing L2 must not re-cache the old L2 entry in L1.
func TestLookupDuringInvalidationDoesNotRecacheStaleL2(t *testing.T) {
	a, r, db, _ := newTestAuthorizer(Config{L1TTL: time.Hour})
	old := alice()
	r.entries[HashKey("sk-a")] = &old
	if _, err := a.Authorize(ctx, "sk-a"); err != nil {
		t.Fatal(err)
	}
	db.add("sk-a", KeyProfile{KeyID: 10, UserID: 1, Group: "vip"})

	// Run a lookup inside the L2 invalidation, after L1 would have been
	// cleared by the old ordering but before L2 is.
	hash := HashKey("sk-a")
	r.onInvalidate = func() {
		a.l1.remove(string(hash[:]))
		_, _ = a.Authorize(ctx, "sk-a")
	}
	if err := a.Invalidate(ctx, "user:1"); err != nil {
		t.Fatal(err)
	}
	r.onInvalidate = nil
	if p, _ := a.Authorize(ctx, "sk-a"); p.Group != "vip" {
		t.Fatalf("stale L2 entry re-cached in L1: group %q", p.Group)
	}
}

// A load that races an invalidation must not leave its result in L1.
func TestInvalidationDuringLoadIsNotCached(t *testing.T) {
	a, _, db, _ := newTestAuthorizer(Config{})
	db.add("sk-a", alice())
	db.during = func() { _ = a.Invalidate(ctx, "user:1") }
	if _, err := a.Authorize(ctx, "sk-a"); err != nil {
		t.Fatal(err)
	}
	db.during = nil
	_, _ = a.Authorize(ctx, "sk-a")
	if db.loads.Load() != 2 {
		t.Fatal("result loaded across an invalidation was cached")
	}
}

func TestFencedL2WriteIsNotCachedInL1(t *testing.T) {
	a, r, db, _ := newTestAuthorizer(Config{})
	db.add("sk-a", alice())
	r.fenced = true
	_, _ = a.Authorize(ctx, "sk-a")
	_, _ = a.Authorize(ctx, "sk-a")
	if db.loads.Load() != 2 {
		t.Fatal("fenced load was cached in L1")
	}
}

func TestRedisDownFallsBackToDatabase(t *testing.T) {
	a, r, db, _ := newTestAuthorizer(Config{})
	db.add("sk-a", alice())
	r.down = true
	if _, err := a.Authorize(ctx, "sk-a"); err != nil {
		t.Fatalf("redis outage broke auth: %v", err)
	}
	if _, err := a.Authorize(ctx, "sk-a"); err != nil || db.loads.Load() != 1 {
		t.Fatalf("L1 not used during redis outage: loads=%d err=%v", db.loads.Load(), err)
	}
}

func TestKeyRestrictionsAndHelpers(t *testing.T) {
	p := KeyProfile{AllowedModels: []string{"gpt-4o"}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	if !p.AllowsModel("gpt-4o") || p.AllowsModel("o3") {
		t.Fatal("model allowlist")
	}
	if !p.AllowsAddr(netip.MustParseAddr("::ffff:10.1.2.3")) || p.AllowsAddr(netip.MustParseAddr("192.168.1.1")) {
		t.Fatal("cidr allowlist")
	}
	if open := (KeyProfile{}); !open.AllowsModel("x") || !open.AllowsAddr(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("nil lists must allow everything")
	}

	key, hash, prefix, err := GenerateKey()
	if err != nil || len(key) != 51 || !strings.HasPrefix(key, "sk-") || prefix != key[:7] || hash != HashKey(key) {
		t.Fatalf("GenerateKey = %q %q %v", key, prefix, err)
	}
	for i := 0; i < 1000; i++ {
		if d := jitter(time.Minute); d < 48*time.Second || d > 72*time.Second {
			t.Fatalf("jitter out of ±20%%: %v", d)
		}
	}
}

func TestLRUBoundAndIndexCleanup(t *testing.T) {
	a, _, db, _ := newTestAuthorizer(Config{L1Entries: 32})
	for i := 0; i < 500; i++ {
		db.add("sk-"+strconv.Itoa(i), KeyProfile{KeyID: int64(i), UserID: 1})
		_, _ = a.Authorize(ctx, "sk-"+strconv.Itoa(i))
	}
	if n := len(a.idx.user(1)); n > 32 {
		t.Fatalf("index kept %d hashes for a 32-entry cache", n)
	}
}
