//go:build pgintegration

package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

func TestSlowMaintenanceCollapsesTicksAndAllowsNextPass(t *testing.T) {
	pool := jobsTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	started, finish := make(chan struct{}, 1), make(chan struct{})
	client, err := newJobs(pool, map[string]func(context.Context) error{
		"slow": func(ctx context.Context) error {
			started <- struct{}{}
			select {
			case <-finish:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if err := client.StopAndCancel(stop); err != nil {
			t.Error(err)
		}
	})
	insert := func() int64 {
		t.Helper()
		result, err := client.Insert(ctx, maintenanceArgs{Action: "slow"}, &river.InsertOpts{UniqueOpts: maintenanceUniqueOpts()})
		if err != nil {
			t.Fatal(err)
		}
		return result.Job.ID
	}
	first := insert()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for range 40 {
		if id := insert(); id != first {
			t.Fatalf("slow pass queued extra job %d while %d was running", id, first)
		}
	}
	close(finish)
	waitJobState(t, ctx, pool, "slow", rivertype.JobStateCompleted)
	if next := insert(); next == first {
		t.Fatal("completed job suppressed the next maintenance pass")
	}
	waitJobState(t, ctx, pool, "slow", rivertype.JobStateCompleted)
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_platform.river_job WHERE args->>'action'='slow'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("expected two actual passes, count=%d err=%v", count, err)
	}
}
