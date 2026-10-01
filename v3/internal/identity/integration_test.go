//go:build pgintegration

package identity

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/migrations"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// V3_TEST_PG_DSN=... V3_TEST_REDIS_ADDR=... go test -tags=pgintegration ./internal/identity/
func testDeps(t *testing.T) (*pgxpool.Pool, *redisx.Client) {
	t.Helper()
	dsn, addr := os.Getenv("V3_TEST_PG_DSN"), os.Getenv("V3_TEST_REDIS_ADDR")
	if dsn == "" || addr == "" {
		t.Skip("V3_TEST_PG_DSN / V3_TEST_REDIS_ADDR not set")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	mustExec(t, pool, `DO $$ DECLARE s record; BEGIN
		FOR s IN SELECT nspname FROM pg_namespace WHERE left(nspname,3)='v3_' LOOP
			EXECUTE format('DROP SCHEMA %I CASCADE', s.nspname);
		END LOOP;
	END $$`)
	names, _ := migrations.Files()
	for _, n := range names {
		sql, _ := migrations.Read(n)
		mustExec(t, pool, sql)
	}
	rdb, err := redisx.Connect(redisx.Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	return pool, rdb
}

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("%s: %v", strings.SplitN(sql, "\n", 2)[0], err)
	}
}

// seedKey creates user uid with one active key and returns its plaintext.
func seedKey(t *testing.T, pool *pgxpool.Pool, uid, kid int64) string {
	t.Helper()
	mustExec(t, pool, `INSERT INTO v3_identity.users (id, username) VALUES ($1, $2) ON CONFLICT DO NOTHING`, uid, "u"+strconv.FormatInt(uid, 10))
	key, hash, prefix, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, `INSERT INTO v3_identity.api_keys (id, user_id, key_hash, key_prefix, key_ciphertext)
		VALUES ($1, $2, $3, $4, '\x00')`, kid, uid, hash[:], prefix)
	return key
}

func TestAuthorizeAgainstPostgres(t *testing.T) {
	pool, rdb := testDeps(t)
	key := seedKey(t, pool, 1, 10)
	mustExec(t, pool, `UPDATE v3_identity.api_keys SET allowed_models = '{gpt-4o}', allowed_cidrs = '{10.0.0.0/8}' WHERE id = 10`)
	off := seedKey(t, pool, 2, 20)
	mustExec(t, pool, `UPDATE v3_identity.users SET status = 'disabled' WHERE id = 2`)
	expired := seedKey(t, pool, 3, 30)
	mustExec(t, pool, `UPDATE v3_identity.api_keys SET expires_at = now() - interval '1 minute' WHERE id = 30`)

	a := New(pool, rdb, Config{}, discardLogger())
	p, err := a.Profile(ctx, key)
	if err != nil || p.UserID != 1 || p.Group != "default" || !p.AllowsModel("gpt-4o") || p.AllowsModel("o3") || len(p.AllowedCIDRs) != 1 {
		t.Fatalf("profile = %+v, %v", p, err)
	}
	for _, k := range []string{off, expired, "sk-unknown"} {
		if _, err := a.Authorize(ctx, k); !errors.Is(err, gateway.ErrInvalidKey) {
			t.Fatalf("key %q... = %v; want ErrInvalidKey", k[:6], err)
		}
	}
	// A second instance is served from L2 without a database round trip.
	b := New(pool, rdb, Config{}, discardLogger())
	b.db = failingLoader{}
	if _, err := b.Authorize(ctx, key); err != nil {
		t.Fatalf("second instance did not hit L2: %v", err)
	}
}

type failingLoader struct{}

func (failingLoader) load(context.Context, [32]byte) (*KeyProfile, error) {
	return nil, errors.New("postgres must not be called")
}

// Acceptance: a group change reaches every instance within 1 s.
func TestGroupChangeConvergesAcrossInstances(t *testing.T) {
	pool, rdb := testDeps(t)
	key := seedKey(t, pool, 1, 10)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	instances := []*Authorizer{New(pool, rdb, Config{}, discardLogger()), New(pool, rdb, Config{}, discardLogger())}
	for _, a := range instances {
		if _, err := a.Authorize(ctx, key); err != nil {
			t.Fatal(err)
		}
		go func(a *Authorizer) { _ = a.Run(runCtx) }(a)
	}
	for i, a := range instances {
		select {
		case <-a.Ready():
		case <-time.After(5 * time.Second):
			t.Fatalf("instance %d subscription not active after 5 s", i)
		}
	}

	mustExec(t, pool, `UPDATE v3_identity.users SET group_name = 'vip' WHERE id = 1`)
	start := time.Now()
	// The outbox relay (internal/catalog) publishes this; simulate it here.
	if err := rdb.Publish(ctx, redisx.ChannelInvalidate, "user:1").Err(); err != nil {
		t.Fatal(err)
	}
	for i, a := range instances {
		for {
			p, err := a.Authorize(ctx, key)
			if err == nil && p.Group == "vip" {
				break
			}
			if time.Since(start) > time.Second {
				t.Fatalf("instance %d still sees %+v after 1 s", i, p)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	t.Logf("both instances converged in %v", time.Since(start))
}

// Acceptance: 10,000 random invalid keys cause zero Redis writes.
func TestRandomKeyScanWritesNothingToRedis(t *testing.T) {
	pool, rdb := testDeps(t)
	a := New(pool, rdb, Config{MaxDBConcurrency: 1024}, discardLogger())
	if err := rdb.ConfigResetStat(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10_000; i++ {
		if _, err := a.Authorize(ctx, "sk-scan-"+strconv.Itoa(i)); !errors.Is(err, gateway.ErrInvalidKey) {
			t.Fatalf("scan key %d = %v", i, err)
		}
	}
	stats, err := rdb.Info(ctx, "commandstats").Result()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(stats, "\n") {
		for _, write := range []string{"cmdstat_set:", "cmdstat_setex:", "cmdstat_sadd:", "cmdstat_eval:", "cmdstat_evalsha:", "cmdstat_del:"} {
			if strings.HasPrefix(line, write) {
				t.Fatalf("random keys caused Redis writes: %s", strings.TrimSpace(line))
			}
		}
	}
	if n, _ := rdb.DBSize(ctx).Result(); n != 0 {
		t.Fatalf("redis holds %d keys after a scan of invalid keys", n)
	}
}

// The real Lua fence: a load that started before an invalidation is rejected.
func TestL2FenceRejectsStaleFill(t *testing.T) {
	_, rdb := testDeps(t)
	c := &l2{rdb: rdb, ttl: time.Minute, tomb: 3 * time.Minute}
	p := &KeyProfile{KeyID: 10, UserID: 1, Group: "default"}
	hash := HashKey("sk-fence")

	start, err := c.clock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.invalidateUser(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if ok, err := c.put(ctx, hash, p, start); err != nil || ok {
		t.Fatalf("stale fill after user invalidation written=%v err=%v", ok, err)
	}
	time.Sleep(2 * time.Millisecond)
	fresh, _ := c.clock(ctx)
	if ok, err := c.put(ctx, hash, p, fresh); err != nil || !ok {
		t.Fatalf("fresh fill rejected: written=%v err=%v", ok, err)
	}
	if err := c.invalidateKey(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := c.get(ctx, hash); found {
		t.Fatal("key invalidation left the L2 entry in place")
	}
}

func TestTouchIsThrottled(t *testing.T) {
	pool, _ := testDeps(t)
	seedKey(t, pool, 1, 10)
	clk := newClock()
	tc := newToucher(pool, 30*time.Second, clk.now, discardLogger())
	lastUsed := func() *time.Time {
		var ts *time.Time
		if err := pool.QueryRow(ctx, `SELECT last_used_at FROM v3_identity.api_keys WHERE id = 10`).Scan(&ts); err != nil {
			t.Fatal(err)
		}
		return ts
	}
	tc.touch(10)
	if err := tc.flush(ctx); err != nil || lastUsed() == nil {
		t.Fatalf("first touch not written: %v", err)
	}
	mustExec(t, pool, `UPDATE v3_identity.api_keys SET last_used_at = NULL WHERE id = 10`)
	clk.advance(10 * time.Second)
	for i := 0; i < 100; i++ {
		tc.touch(10)
	}
	if err := tc.flush(ctx); err != nil || lastUsed() != nil {
		t.Fatal("touch inside the window was written again")
	}
	clk.advance(25 * time.Second)
	tc.touch(10)
	if err := tc.flush(ctx); err != nil || lastUsed() == nil {
		t.Fatal("touch after the window was not written")
	}
}
