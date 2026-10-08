//go:build pgintegration

package community

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func ratingRelayTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("CODEGO_RATING_RELAY_TEST_DSN")
	if dsn == "" {
		t.Skip("CODEGO_RATING_RELAY_TEST_DSN not set")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	// This suite inserts and removes only its own outbox rows. Never point it at
	// the application's database or reuse the destructive migration test helper.
	if cfg.ConnConfig.Database != "community_sync_test" || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 55497 {
		t.Fatal("rating relay tests require the dedicated localhost:55497/community_sync_test database")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(context.Background(), `CREATE SCHEMA IF NOT EXISTS v3_community;
CREATE TABLE IF NOT EXISTS v3_community.rating_event_outbox (
id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,channel_id text NOT NULL,owner_sub text NOT NULL,
created_at timestamptz NOT NULL DEFAULT now(),available_at timestamptz NOT NULL DEFAULT now(),
attempts integer NOT NULL DEFAULT 0,delivered_at timestamptz);
CREATE INDEX IF NOT EXISTS rating_event_pending_idx ON v3_community.rating_event_outbox(available_at,id) WHERE delivered_at IS NULL;`)
	if err != nil {
		t.Fatal(err)
	}
	var pending int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM v3_community.rating_event_outbox WHERE delivered_at IS NULL`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("dedicated database contains %d unrelated pending events; refusing to consume them", pending)
	}
	return pool
}

func insertRelayTestEvents(t *testing.T, pool *pgxpool.Pool, count int) []int64 {
	t.Helper()
	ids := make([]int64, 0, count)
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM v3_community.rating_event_outbox WHERE id=ANY($1::bigint[])`, ids); err != nil {
			t.Errorf("remove own test rows: %v", err)
		}
	})
	for i := 0; i < count; i++ {
		var id int64
		if err := pool.QueryRow(context.Background(), `INSERT INTO v3_community.rating_event_outbox(channel_id,owner_sub) VALUES($1,'ABC234') RETURNING id`, "relay-test-"+t.Name()).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestRatingRelayPGDeliveryRetryConcurrentAndIdle(t *testing.T) {
	pool := ratingRelayTestPool(t)
	ctx := context.Background()
	t.Run("empty_and_disabled_do_not_send", func(t *testing.T) {
		var calls atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			calls.Add(1)
			_, _ = io.WriteString(w, `{"success":true}`)
		}))
		defer server.Close()
		relay, err := NewRatingRelay(pool, server.URL, relayTestSecret, relayTestLog())
		if err != nil {
			t.Fatal(err)
		}
		if err := relay.DeliverPending(ctx); err != nil {
			t.Fatal(err)
		}
		ids := insertRelayTestEvents(t, pool, 1)
		disabled, err := NewRatingRelay(pool, "", "", relayTestLog())
		if err != nil {
			t.Fatal(err)
		}
		if err := disabled.DeliverPending(ctx); err != nil {
			t.Fatal(err)
		}
		var untouched bool
		if err := pool.QueryRow(ctx, `SELECT delivered_at IS NULL AND attempts=0 FROM v3_community.rating_event_outbox WHERE id=$1`, ids[0]).Scan(&untouched); err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 0 || !untouched {
			t.Fatalf("calls=%d event untouched=%v", calls.Load(), untouched)
		}
	})
	t.Run("failed_delivery_is_durable_and_retried", func(t *testing.T) {
		ids := insertRelayTestEvents(t, pool, 1)
		var calls atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(503)
				return
			}
			_, _ = io.WriteString(w, `{"success":true}`)
		}))
		defer server.Close()
		relay, err := NewRatingRelay(pool, server.URL, relayTestSecret, relayTestLog())
		if err != nil {
			t.Fatal(err)
		}
		if err := relay.DeliverPending(ctx); err != nil {
			t.Fatal(err)
		}
		var pending, delayed bool
		var attempts int
		if err := pool.QueryRow(ctx, `SELECT delivered_at IS NULL,available_at>now(),attempts FROM v3_community.rating_event_outbox WHERE id=$1`, ids[0]).Scan(&pending, &delayed, &attempts); err != nil {
			t.Fatal(err)
		}
		if !pending || !delayed || attempts != 1 {
			t.Fatalf("pending=%v delayed=%v attempts=%d", pending, delayed, attempts)
		}
		if err := relay.DeliverPending(ctx); err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 1 {
			t.Fatal("retry backoff ignored")
		}
		if _, err := pool.Exec(ctx, `UPDATE v3_community.rating_event_outbox SET available_at=now()-interval '1 second' WHERE id=$1`, ids[0]); err != nil {
			t.Fatal(err)
		}
		if err := relay.DeliverPending(ctx); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT delivered_at IS NULL,attempts FROM v3_community.rating_event_outbox WHERE id=$1`, ids[0]).Scan(&pending, &attempts); err != nil {
			t.Fatal(err)
		}
		if pending || attempts != 1 || calls.Load() != 2 {
			t.Fatalf("pending=%v attempts=%d calls=%d", pending, attempts, calls.Load())
		}
	})
	t.Run("multiple_workers_claim_each_event_once", func(t *testing.T) {
		ids := insertRelayTestEvents(t, pool, 40)
		var mu sync.Mutex
		seen := make(map[string]int)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			var event ratingEvent
			if err := json.NewDecoder(req.Body).Decode(&event); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			mu.Lock()
			seen[event.Version]++
			mu.Unlock()
			_, _ = io.WriteString(w, `{"success":true}`)
		}))
		defer server.Close()
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			relay, err := NewRatingRelay(pool, server.URL, relayTestSecret, relayTestLog())
			if err != nil {
				t.Fatal(err)
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := relay.DeliverPending(ctx); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		var delivered int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_community.rating_event_outbox WHERE id=ANY($1::bigint[]) AND delivered_at IS NOT NULL AND attempts=0`, ids).Scan(&delivered); err != nil {
			t.Fatal(err)
		}
		if delivered != len(ids) || len(seen) != len(ids) {
			t.Fatalf("delivered=%d distinct=%d", delivered, len(seen))
		}
		for version, count := range seen {
			if count != 1 {
				t.Fatalf("version %s sent %d times", version, count)
			}
		}
	})
	t.Run("listener_wakes_on_notify_then_waits_idle_until_cancelled", func(t *testing.T) {
		var calls atomic.Int64
		sent := make(chan struct{}, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			calls.Add(1)
			_, _ = io.WriteString(w, `{"success":true}`)
			select {
			case sent <- struct{}{}:
			default:
			}
		}))
		defer server.Close()
		relay, err := NewRatingRelay(pool, server.URL, relayTestSecret, relayTestLog())
		if err != nil {
			t.Fatal(err)
		}
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		finished := make(chan error, 1)
		go func() { finished <- relay.Run(runCtx) }()
		deadline := time.Now().Add(2 * time.Second)
		for {
			var listening bool
			if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND query='LISTEN v3_market_rating_changed' AND state='idle')`).Scan(&listening); err != nil {
				t.Fatal(err)
			}
			if listening {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("relay did not register its LISTEN connection")
			}
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(30 * time.Millisecond)
		if calls.Load() != 0 {
			t.Fatal("empty listener made an HTTP request")
		}
		insertRelayTestEvents(t, pool, 1)
		if _, err := pool.Exec(ctx, `SELECT pg_notify('v3_market_rating_changed','test-event')`); err != nil {
			t.Fatal(err)
		}
		select {
		case <-sent:
		case <-time.After(2 * time.Second):
			t.Fatal("NOTIFY did not wake event delivery")
		}
		time.Sleep(150 * time.Millisecond)
		if calls.Load() != 1 {
			t.Fatalf("idle requests=%d", calls.Load())
		}
		cancel()
		select {
		case err := <-finished:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel result %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("listener did not cancel")
		}
	})
}
