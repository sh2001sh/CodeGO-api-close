package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
)

type maintenanceArgs struct {
	Action string `json:"action" river:"unique"`
}

func (maintenanceArgs) Kind() string { return "v3_maintenance" }

// The persisted queue retries a failed maintenance pass after a worker restart.
// Business effects retain their transaction/idempotency guards; a job retry
// cannot promise exactly-once execution across a process crash.
type maintenanceWorker struct {
	river.WorkerDefaults[maintenanceArgs]
	pool    *pgxpool.Pool
	actions map[string]func(context.Context) error
}

func (w *maintenanceWorker) Timeout(job *river.Job[maintenanceArgs]) time.Duration {
	if job.Args.Action == "workflow" || job.Args.Action == "channel_market" {
		// Provider polling and charged batch tests have bounded network deadlines.
		return 15 * time.Minute
	}
	return 0
}

func (w *maintenanceWorker) Work(ctx context.Context, job *river.Job[maintenanceArgs]) error {
	run, ok := w.actions[job.Args.Action]
	if !ok {
		return river.JobCancel(errors.New("worker: unsupported maintenance action"))
	}
	conn, err := w.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	lock := "v3:maintenance:" + job.Args.Action
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, lock).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return river.JobSnooze(time.Second)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, unlockErr := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, lock); unlockErr != nil {
			// A session holding a lock must never return to the pool.
			_ = conn.Hijack().Close(unlockCtx)
		}
	}()
	return run(ctx)
}

func newJobs(pool *pgxpool.Pool, actions map[string]func(context.Context) error, periods map[string]time.Duration, log *slog.Logger) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &maintenanceWorker{pool: pool, actions: actions})
	periodic := make([]*river.PeriodicJob, 0, len(periods))
	for action, period := range periods {
		if period < time.Second || actions[action] == nil {
			return nil, fmt.Errorf("worker: invalid schedule for %s", action)
		}
		periodic = append(periodic, river.NewPeriodicJob(river.PeriodicInterval(period), func() (river.JobArgs, *river.InsertOpts) {
			return maintenanceArgs{Action: action}, &river.InsertOpts{
				MaxAttempts: 25, UniqueOpts: maintenanceUniqueOpts(),
			}
		}, &river.PeriodicJobOpts{ID: action, RunOnStart: true}))
	}
	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Schema: "v3_platform", Logger: log, Workers: workers, PeriodicJobs: periodic,
		Queues: map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 4}},
	})
}

// A slow pass must not build an unbounded backlog of identical periodic jobs.
// Completed jobs leave the set so the next scheduled maintenance pass can run.
func maintenanceUniqueOpts() river.UniqueOpts {
	return river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
		rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning,
		rivertype.JobStateRetryable, rivertype.JobStateScheduled,
	}}
}
