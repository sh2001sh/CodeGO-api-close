//go:build pgintegration

package identity

import "testing"

// User limits belong to the cached principal. An update must emit the same
// user invalidation as a status or group change so gateways stop using stale
// concurrency and RPM limits.
func TestUserLimitsLoadAndInvalidate(t *testing.T) {
	pool, rdb := testDeps(t)
	key := seedKey(t, pool, 1, 10)
	mustExec(t, pool, `UPDATE v3_identity.users SET max_concurrency = 3, requests_per_minute = 40 WHERE id = 1`)
	a := New(pool, rdb, Config{}, discardLogger())
	p, err := a.Authorize(ctx, key)
	if err != nil || p.MaxConcurrency != 3 || p.RequestsPerMinute != 40 {
		t.Fatalf("principal limits: %+v, %v", p, err)
	}
	mustExec(t, pool, `DELETE FROM v3_platform.cache_invalidation_outbox`)
	mustExec(t, pool, `UPDATE v3_identity.users SET max_concurrency = 4, requests_per_minute = 50 WHERE id = 1`)
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_platform.cache_invalidation_outbox WHERE entity = 'user' AND entity_id = '1'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("limit update emitted %d invalidations, want 1", n)
	}
	if err := a.Invalidate(ctx, "user:1"); err != nil {
		t.Fatal(err)
	}
	p, err = a.Authorize(ctx, key)
	if err != nil || p.MaxConcurrency != 4 || p.RequestsPerMinute != 50 {
		t.Fatalf("principal after invalidation: %+v, %v", p, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET max_concurrency = -1 WHERE id = 1`); err == nil {
		t.Fatal("negative concurrency accepted")
	}
}
