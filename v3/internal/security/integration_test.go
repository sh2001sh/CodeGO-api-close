//go:build pgintegration

package security

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/migrations"
)

func TestPersistentGuardRealDependencies(t *testing.T) {
	dsn, addr := os.Getenv("V3_TEST_PG_DSN"), os.Getenv("V3_TEST_REDIS_ADDR")
	if dsn == "" || addr == "" {
		t.Skip("dedicated V3_TEST_PG_DSN and V3_TEST_REDIS_ADDR required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("security_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer func() { _ = rdb.Close() }()
	var keys []string
	for user := int64(1); user <= 100; user++ {
		keys = append(keys, stateKey(user), fmt.Sprintf("v3:request-abuse:rpm:%d", user))
	}
	if err = rdb.Del(ctx, keys...).Err(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if e := rdb.Del(ctx, keys...).Err(); e != nil {
			t.Error(e)
		}
	}()
	// Each test run uses the complete native migration set in a new database.
	names, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		sql, e := migrations.Read(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, sql); e != nil {
			t.Fatalf("%s: %v", name, e)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username,role,status) SELECT i,'security-test-user-'||i,CASE WHEN i=99 THEN 'admin' ELSE 'user' END,'active' FROM generate_series(1,100) i`); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1800000000, 0)
	g, err := New(pool, rdb, Config{Enabled: true, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	run := func(fields map[string]string, rollback bool) error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err = g.RecordUsageTx(ctx, tx, fields); err != nil {
			return err
		}
		if rollback {
			return tx.Rollback(ctx)
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		return g.processUsage(ctx, 1000)
	}
	fields := func(user int64) map[string]string {
		return map[string]string{billing.FieldUserID: fmt.Sprint(user), billing.FieldChannelID: "42", billing.FieldModel: "known-model", billing.FieldPromptTokens: "100", billing.FieldCachedTokens: "0", billing.FieldEstimated: "0", billing.FieldTerminal: "completed", billing.FieldRequestID: fmt.Sprintf("%d-%d", user, now.Unix())}
	}
	hash := sha256.Sum256([]byte("known-model"))
	modelHash := hex.EncodeToString(hash[:])
	seed := func(user, n, short, input, cache, unknown int64) {
		t.Helper()
		_, err := pool.Exec(ctx, `INSERT INTO v3_security.usage_samples VALUES($1,$2,$3,$4,$5,$6,$7,$8),($1,$2,$3+1,$4,$5,$6,$7,$8)`, user, modelHash, now.Unix()/60-2, n, short, input, cache, unknown)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_security.cache_support VALUES(42,$1,$2)`, modelHash, now.Unix()+86400); err != nil {
		t.Fatal(err)
	}
	testQueueContracts(t, g, pool)
	t.Run("only completed real positive hits teach 24h cache support", func(t *testing.T) {
		f := fields(7)
		f[billing.FieldChannelID], f[billing.FieldCachedTokens] = "43", "10"
		if err = run(f, false); err != nil {
			t.Fatal(err)
		}
		var expiry int64
		if err = pool.QueryRow(ctx, `SELECT expires_at FROM v3_security.cache_support WHERE channel_id=43 AND model_hash=$1`, modelHash).Scan(&expiry); err != nil || expiry != now.Unix()+86400 {
			t.Fatalf("positive support %d %v", expiry, err)
		}
		f[billing.FieldChannelID], f[billing.FieldEstimated] = "44", "1"
		if err = run(f, false); err != nil {
			t.Fatal(err)
		}
		f[billing.FieldEstimated], f[billing.FieldTerminal] = "0", "upstream_error_before_output"
		if err = run(f, false); err != nil {
			t.Fatal(err)
		}
		var taught bool
		if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_security.cache_support WHERE channel_id=44)`).Scan(&taught); err != nil || taught {
			t.Fatalf("non-completed/estimated support %v %v", taught, err)
		}
	})
	t.Run("two complete minutes boundaries estimates and unknown", func(t *testing.T) {
		seed(1, 60, 54, 6000, 299, 0)
		if err = run(fields(1), false); err != nil {
			t.Fatal(err)
		}
		s, err := readState(ctx, pool, 1)
		if err != nil || s.Strikes != 1 || s.RestrictedUntil != now.Unix()+86400 || s.Blocked {
			t.Fatalf("first episode %+v %v", s, err)
		}
		for _, c := range []struct{ user, n, short, input, cache, unknown int64 }{{2, 59, 59, 5900, 0, 0}, {3, 60, 53, 6000, 0, 0}, {4, 60, 60, 6000, 300, 0}, {5, 60, 60, 6000, 0, 1}} {
			seed(c.user, c.n, c.short, c.input, c.cache, c.unknown)
			if err = run(fields(c.user), false); err != nil {
				t.Fatal(err)
			}
			s, err = readState(ctx, pool, c.user)
			if err != nil || s.Strikes != 0 {
				t.Fatalf("boundary %d restricted %+v %v", c.user, s, err)
			}
		}
		seed(6, 60, 60, 6000, 0, 0)
		f := fields(6)
		f[billing.FieldEstimated] = "1"
		if err = run(f, false); err != nil {
			t.Fatal(err)
		}
		s, _ = readState(ctx, pool, 6)
		if s.Strikes != 0 {
			t.Fatal("estimated usage restricted user")
		}
		seed(99, 60, 60, 6000, 0, 0)
		if err = run(fields(99), false); err != nil {
			t.Fatal(err)
		}
		s, _ = readState(ctx, pool, 99)
		if s.Strikes != 0 {
			t.Fatal("administrator restricted")
		}
	})
	t.Run("replay concurrency recovery and rollback", func(t *testing.T) {
		var wg sync.WaitGroup
		errs := make(chan error, 12)
		for range 12 {
			wg.Add(1)
			go func() { defer wg.Done(); errs <- run(fields(1), false) }()
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		s, _ := readState(ctx, pool, 1)
		if s.Strikes != 1 {
			t.Fatalf("one episode counted %d", s.Strikes)
		}
		now = time.Unix(s.RestrictedUntil+120, 0)
		seed(1, 60, 60, 6000, 0, 0)
		if err = run(fields(1), false); err != nil {
			t.Fatal(err)
		}
		s, _ = readState(ctx, pool, 1)
		if s.Strikes != 1 {
			t.Fatal("window overlaps recovery")
		}
		now = now.Add(3 * time.Minute)
		seed(1, 60, 60, 6000, 0, 0)
		if err = run(fields(1), true); err != nil {
			t.Fatal(err)
		}
		s, _ = readState(ctx, pool, 1)
		if s.Blocked {
			t.Fatal("rolled back episode persisted")
		}
		if err = run(fields(1), false); err != nil {
			t.Fatal(err)
		}
		s, _ = readState(ctx, pool, 1)
		if !s.Blocked || s.Strikes != 2 {
			t.Fatalf("second episode %+v", s)
		}
		var status string
		if err = pool.QueryRow(ctx, `SELECT status FROM v3_identity.users WHERE id=1`).Scan(&status); err != nil || status != "disabled" {
			t.Fatalf("status %s %v", status, err)
		}
	})
	t.Run("persistent imported state ten admissions shared account and fail closed", func(t *testing.T) {
		if _, err = pool.Exec(ctx, `INSERT INTO v3_security.account_request_abuse_states VALUES(70,1,$1,123,false,'privileged evidence',123),(71,2,0,123,true,'privileged evidence',123)`, now.Unix()+86400); err != nil {
			t.Fatal(err)
		}
		for range 10 {
			if err = g.Check(ctx, 70, "same-user-controlled-id"); err != nil {
				t.Fatal(err)
			}
		}
		var e *gateway.UpstreamError
		err = g.Check(ctx, 70, "new-key")
		if !errors.As(err, &e) || e.Status != 429 {
			t.Fatalf("eleventh %v", err)
		}
		err = g.Check(ctx, 71, "blocked")
		if !errors.As(err, &e) || e.Status != 403 {
			t.Fatalf("blocked %v", err)
		}
		if _, err = g.FlushStateCache(ctx, 100); err != nil {
			t.Fatal(err)
		}
		err = g.Check(ctx, 1, "blocked-native")
		if !errors.As(err, &e) || e.Status != 403 {
			t.Fatalf("fresh block %v", err)
		}
		badRedis := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 100 * time.Millisecond})
		defer func() { _ = badRedis.Close() }()
		bad, _ := New(pool, badRedis, Config{Enabled: true})
		err = bad.Check(ctx, 70, "dependency")
		if !errors.As(err, &e) || e.Status != 503 {
			t.Fatalf("redis fail open %v", err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO v3_security.state_cache_refresh VALUES(80)`); err != nil {
			t.Fatal(err)
		}
		if _, err = bad.FlushStateCache(ctx, 100); err == nil {
			t.Fatal("outbox refresh swallowed Redis failure")
		}
		var pending bool
		if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_security.state_cache_refresh WHERE user_id=80)`).Scan(&pending); err != nil || !pending {
			t.Fatal("failed refresh acknowledged durable outbox")
		}
		closed, _ := pgxpool.New(ctx, dsn)
		closed.Close()
		bad, _ = New(closed, rdb, Config{Enabled: true})
		err = bad.Check(ctx, 80, "cold-db")
		if !errors.As(err, &e) || e.Status != 503 {
			t.Fatalf("database fail open %v", err)
		}
		// A warm unrestricted cache performs no database queries while PostgreSQL is unavailable.
		if err = g.Check(ctx, 81, "warm"); err != nil {
			t.Fatal(err)
		}
		bad, _ = New(closed, rdb, Config{Enabled: true})
		if err = bad.Check(ctx, 81, "warm-without-db"); err != nil {
			t.Fatal(err)
		}
		now = now.Add(24 * time.Hour)
		if err = g.Check(ctx, 70, "exact-recovery-boundary"); err != nil {
			t.Fatalf("restriction did not expire at boundary: %v", err)
		}
	})
	testAuditAccess(t, g, pool)
}
