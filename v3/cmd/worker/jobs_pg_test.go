//go:build pgintegration

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/sh2001sh/new-api/v3/migrations"
)

func jobsTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("worker_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize())
		_ = admin.Close(ctx)
	})
	names, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		source, err := migrations.Read(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, source); err != nil {
			t.Fatalf("migration %s: %v", name, err)
		}
	}
	return pool
}

func TestRiverTwoWorkersExecuteUniqueJobOnceAndRecoverFailure(t *testing.T) {
	pool := jobsTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var completed, attempts atomic.Int32
	actions := map[string]func(context.Context) error{
		"unique": func(context.Context) error { completed.Add(1); return nil },
		"retry": func(context.Context) error {
			if attempts.Add(1) == 1 {
				return errors.New("transient dependency failure")
			}
			return nil
		},
	}
	clients := make([]*river.Client[pgx.Tx], 2)
	for i := range clients {
		client, err := newJobs(pool, actions, nil, logger)
		if err != nil {
			t.Fatal(err)
		}
		clients[i] = client
		if err = client.Start(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			stopCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			if err := client.StopAndCancel(stopCtx); err != nil {
				t.Error(err)
			}
		})
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 40)
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := clients[i%2].Insert(ctx, maintenanceArgs{Action: "unique"}, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: time.Hour}})
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	waitJobState(t, ctx, pool, "unique", rivertype.JobStateCompleted)
	if got := completed.Load(); got != 1 {
		t.Fatalf("executed %d times; want 1", got)
	}
	var jobs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_platform.river_job WHERE args->>'action'='unique'`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 {
		t.Fatalf("persisted %d jobs; want 1", jobs)
	}
	_, err := clients[0].Insert(ctx, maintenanceArgs{Action: "retry"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitJobState(t, ctx, pool, "retry", rivertype.JobStateCompleted)
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts=%d; want failure then successful retry", got)
	}
	var storedErrors, storedAttempts int
	if err := pool.QueryRow(ctx, `SELECT cardinality(errors),attempt FROM v3_platform.river_job WHERE args->>'action'='retry'`).Scan(&storedErrors, &storedAttempts); err != nil {
		t.Fatal(err)
	}
	if storedErrors != 1 || storedAttempts != 2 {
		t.Fatalf("persisted retry: errors=%d attempts=%d", storedErrors, storedAttempts)
	}
	t.Log("two River workers: 40 concurrent submissions -> 1 persisted job, 1 execution; failed job retried successfully")
}

func waitJobState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, action string, want rivertype.JobState) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var state string
		err := pool.QueryRow(ctx, `SELECT state::text FROM v3_platform.river_job WHERE args->>'action'=$1 ORDER BY id DESC LIMIT 1`, action).Scan(&state)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
		if state == string(want) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("action %s state=%s; want %s: %v", action, state, want, ctx.Err())
		case <-ticker.C:
		}
	}
}
