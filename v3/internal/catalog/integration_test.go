//go:build pgintegration

package catalog

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

func TestCompileIntegration(t *testing.T) {
	pool := testPool(t)
	dec, enc := testDecrypter(t)
	seedCatalog(t, pool, enc)

	snap, err := Compile(context.Background(), pool, dec)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if len(snap.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(snap.Channels))
	}
	if got := snap.Channels[1].Credentials[0].Secret; got != "sk-alpha" {
		t.Fatalf("channel 1 credential = %q, want sk-alpha", got)
	}
	routes := snap.Routes["default"]["gpt-4"]
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes for default/gpt-4, got %+v", routes)
	}
	// Deterministic order: by channel id.
	if routes[0].ChannelID != 1 || routes[1].ChannelID != 2 {
		t.Fatalf("expected routes ordered by channel id, got %+v", routes)
	}
	if routes[1].Weight != 3 {
		t.Fatalf("channel 2 weight = %d, want 3", routes[1].Weight)
	}
	if routes[0].Weight != 1 { // channel 1's weight 0 clamps to 1
		t.Fatalf("channel 1 weight = %d, want 1 (clamped)", routes[0].Weight)
	}
}

func TestCompileRespectsRoutePool(t *testing.T) {
	pool := testPool(t)
	dec, enc := testDecrypter(t)
	seedCatalog(t, pool, enc)

	var poolID int64
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO v3_catalog.route_pools (group_name, model, strategy) VALUES ('default', 'gpt-4', 'round_robin')
		RETURNING id`).Scan(&poolID); err != nil {
		t.Fatalf("insert route_pool: %v", err)
	}
	mustExec(t, pool, `INSERT INTO v3_catalog.route_pool_members (pool_id, channel_id, priority, weight) VALUES ($1, 2, 1, 1)`, poolID)

	snap, err := Compile(context.Background(), pool, dec)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	routes := snap.Routes["default"]["gpt-4"]
	if len(routes) != 1 || routes[0].ChannelID != 2 || routes[0].Strategy != "round_robin" {
		t.Fatalf("expected the pool to fully replace plain membership, got %+v", routes)
	}
}

func TestSnapshotConvergenceAcrossStores(t *testing.T) {
	pool := testPool(t)
	rdb := testRedis(t)
	dec, enc := testDecrypter(t)
	seedCatalog(t, pool, enc)

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	publisher := NewPublisher(pool, rdb, dec, enc, log)

	storeA := NewStore(pool, rdb, dec, log)
	storeB := NewStore(pool, rdb, dec, log)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = storeA.Run(ctx) }()
	go func() { _ = storeB.Run(ctx) }()

	if err := publisher.PublishNow(context.Background()); err != nil {
		t.Fatalf("PublishNow: %v", err)
	}

	// A control-plane change (a channel's priority) must reach every gateway
	// instance's Store within 1 s of a fresh publish (plan §11 acceptance).
	mustExec(t, pool, `UPDATE v3_catalog.channels SET priority = 99 WHERE id = 1`)
	if err := publisher.PublishNow(context.Background()); err != nil {
		t.Fatalf("PublishNow after update: %v", err)
	}

	deadline := time.Now().Add(1 * time.Second)
	for {
		a, b := storeA.Current(), storeB.Current()
		if a != nil && b != nil && a.Version == b.Version && a.Channels[1].Priority == 99 && b.Channels[1].Priority == 99 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stores did not converge within 1s: a=%+v b=%+v", a, b)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestOutboxRelayDrivesSnapshotOnCatalogChange(t *testing.T) {
	pool := testPool(t)
	rdb := testRedis(t)
	dec, enc := testDecrypter(t)
	seedCatalog(t, pool, enc)

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	publisher := NewPublisher(pool, rdb, dec, enc, log)
	relay := NewOutboxRelay(pool, rdb, publisher, log)
	store := NewStore(pool, rdb, dec, log)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = relay.Run(ctx) }()
	go func() { _ = store.Run(ctx) }()

	// The channels insert during seedCatalog already enqueued outbox rows;
	// give the relay a moment then check a version was published.
	deadline := time.Now().Add(2 * time.Second)
	for store.Version() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("outbox relay did not trigger a snapshot publish within 2s")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestOutboxRelayNoDoubleDelivery runs two relay instances against the same
// outbox concurrently. FOR UPDATE SKIP LOCKED must give each row to exactly
// one relay, so the total number of invalidations delivered must equal the
// number of rows enqueued, not more (a duplicate delivery) and not fewer (a
// row leased by both, published by neither, then deleted by one).
func TestOutboxRelayNoDoubleDelivery(t *testing.T) {
	pool := testPool(t)
	rdb := testRedis(t)
	_, enc := testDecrypter(t)
	seedCatalog(t, pool, enc) // enqueues several outbox rows via triggers

	var wantCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM v3_platform.cache_invalidation_outbox`).Scan(&wantCount); err != nil {
		t.Fatalf("count outbox rows: %v", err)
	}
	if wantCount == 0 {
		t.Fatal("expected seedCatalog to enqueue at least one outbox row")
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	relayA := NewOutboxRelay(pool, rdb, nil, log)
	relayB := NewOutboxRelay(pool, rdb, nil, log)

	sub := rdb.Subscribe(context.Background(), redisx.ChannelInvalidate)
	defer func() { _ = sub.Close() }()
	msgs := sub.Channel()

	seen := make(map[string]int)
	done := make(chan struct{})
	go func() {
		defer close(done)
		timeout := time.After(2 * time.Second)
		for {
			select {
			case m := <-msgs:
				seen[m.Payload]++
			case <-timeout:
				return
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = relayA.Run(ctx) }()
	go func() { _ = relayB.Run(ctx) }()
	<-done
	cancel()

	// Distinct outbox rows may share the same "entity:entity_id" payload text
	// (e.g. a channel insert and its channel_groups row both say "catalog:1"),
	// so uniqueness is checked at the row level (below), not the payload
	// level: the total delivered must equal the total enqueued.
	var received int
	for _, count := range seen {
		received += count
	}
	if received != wantCount {
		t.Fatalf("received %d invalidations, want exactly %d (no duplicates, no drops)", received, wantCount)
	}

	var remaining int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM v3_platform.cache_invalidation_outbox`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining outbox rows: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected all outbox rows to be deleted after delivery, %d remain", remaining)
	}
}
