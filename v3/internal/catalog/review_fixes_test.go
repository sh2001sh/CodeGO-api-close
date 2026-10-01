//go:build pgintegration

package catalog

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Regression tests for defects found in acceptance review (2026-09-30).

// proxy_url and a NULL expires_at must round-trip through Compile.
func TestCompileProxyURLAndNeverExpiring(t *testing.T) {
	pool := testPool(t)
	dec, enc := testDecrypter(t)
	seedCatalog(t, pool, enc)
	mustExec(t, pool, `UPDATE v3_catalog.channels SET proxy_url = 'socks5://10.0.0.1:1080' WHERE id = 1`)

	snap, err := Compile(context.Background(), pool, dec)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Channels[1].ProxyURL; got != "socks5://10.0.0.1:1080" {
		t.Fatalf("ProxyURL = %q", got)
	}
	if exp := snap.Channels[1].Credentials[0].ExpiresAt; !exp.IsZero() {
		t.Fatalf("NULL expires_at became %v; want zero time (never expires)", exp)
	}
}

// A wildcard pool must also serve models no channel lists explicitly.
func TestCompileWildcardPoolServesUnlistedModels(t *testing.T) {
	pool := testPool(t)
	dec, enc := testDecrypter(t)
	seedCatalog(t, pool, enc)
	mustExec(t, pool, `INSERT INTO v3_catalog.route_pools (id, group_name, model, strategy)
		OVERRIDING SYSTEM VALUE VALUES (1, 'default', '*', 'fill_first')`)
	mustExec(t, pool, `INSERT INTO v3_catalog.route_pool_members (pool_id, channel_id) VALUES (1, 2)`)

	snap, err := Compile(context.Background(), pool, dec)
	if err != nil {
		t.Fatal(err)
	}
	star := snap.Routes["default"]["*"]
	if len(star) != 1 || star[0].ChannelID != 2 {
		t.Fatalf(`Routes["default"]["*"] = %+v; want the wildcard pool`, star)
	}
}

// Concurrent publishers must leave the highest version holding the newest
// catalog state, never a snapshot compiled before the last write.
func TestConcurrentPublishIsMonotonic(t *testing.T) {
	pool := testPool(t)
	rdb := testRedis(t)
	dec, enc := testDecrypter(t)
	seedCatalog(t, pool, enc)
	pubs := []*Publisher{NewPublisher(pool, rdb, dec, enc, nil), NewPublisher(pool, rdb, dec, enc, nil)}

	ctx := context.Background()
	var wg sync.WaitGroup
	for round := 0; round < 10; round++ {
		mustExec(t, pool, `UPDATE v3_catalog.channels SET name = $1 WHERE id = 1`, "rev-"+string(rune('a'+round)))
		for _, p := range pubs {
			wg.Add(1)
			go func(p *Publisher) {
				defer wg.Done()
				if err := p.PublishNow(ctx); err != nil {
					t.Error(err)
				}
			}(p)
		}
		wg.Wait()
	}

	store := NewStore(pool, rdb, dec, nil)
	if err := store.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if store.Version() != 20 {
		t.Fatalf("version = %d; want 20 publishes", store.Version())
	}
	if got := store.Current().Channels[1].Name; got != "rev-j" {
		t.Fatalf("latest version holds %q; want the last write rev-j", got)
	}
}

// A gateway must still start when Redis lost the blob (flush or restart).
func TestStoreFallsBackToCompileWhenBlobMissing(t *testing.T) {
	pool := testPool(t)
	rdb := testRedis(t)
	dec, enc := testDecrypter(t)
	seedCatalog(t, pool, enc)
	ctx := context.Background()
	if err := NewPublisher(pool, rdb, dec, enc, nil).PublishNow(ctx); err != nil {
		t.Fatal(err)
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}

	store := NewStore(pool, rdb, dec, nil)
	loadCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := store.Load(loadCtx); err != nil {
		t.Fatalf("Load after redis flush: %v", err)
	}
	if store.Version() != 1 || len(store.Current().Channels) != 2 {
		t.Fatalf("fallback snapshot = v%d with %d channels", store.Version(), len(store.Current().Channels))
	}
}
