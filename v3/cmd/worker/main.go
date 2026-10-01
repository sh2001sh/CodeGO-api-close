// Command worker runs v3's background jobs:
//
//   - publishes the catalog snapshot at start and on every catalog change
//   - relays the cache-invalidation outbox to Redis pub/sub
//   - releases billing reservations whose gateway died before finalizing
//   - posts billing events into the PostgreSQL ledger and trims the stream
//   - runs durable River maintenance for subscriptions, marketplace assets,
//     billing holds and monthly usage partitions
//   - refreshes encrypted OAuth credentials and relays business balance changes
//   - reconciles Redis balances against the ledger every 5 minutes and serves
//     billing health on V3_WORKER_METRICS_ADDR (default 127.0.0.1:9102)
//
// Several workers may run at once: the outbox uses SKIP LOCKED, publishes are
// serialized by an advisory lock, and River jobs use unique insertion and
// per-action advisory locks. All business mutations remain idempotent.
// Configuration comes from the environment, see cmd/internal/boot.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/credentials"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(ctx, log); err != nil {
		fmt.Fprintln(os.Stderr, "worker:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := loadWorkerConfig()
	if err != nil {
		return err
	}
	deps, err := boot.Open(ctx, 16)
	if err != nil {
		return err
	}
	defer deps.Close()
	group, ctx := errgroup.WithContext(ctx)
	if err := ledger.EnsureUsagePartitions(ctx, deps.PG.Pool, time.Now().UTC()); err != nil {
		return err
	}
	s, err := buildServices(ctx, deps, log)
	if err != nil {
		return err
	}
	defer s.close()

	actions, periods := maintenance(deps, s, cfg)
	jobs, err := newJobs(deps.PG.Pool, actions, periods, log)
	if err != nil {
		return err
	}
	credentialPool, closeTransports, err := newCredentialPool(deps, log)
	if err != nil {
		return err
	}
	defer closeTransports()
	if err = jobs.Start(ctx); err != nil {
		return fmt.Errorf("river start: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := jobs.StopAndCancel(stopCtx); err != nil {
			log.Error("River shutdown failed", "err", err)
		}
	}()
	startLoops(ctx, group, deps, s, credentialPool, cfg, log)

	log.Info("worker running", "metrics", cfg.metricsAddr)
	err = group.Wait()
	// errgroup cancels its context on any failure; the caller's cancellation
	// is distinguished by the error returned from the running service.
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func newCredentialPool(deps *boot.Deps, log *slog.Logger) (*credentials.Pool, func(), error) {
	transports := credentials.NewTransportPool(credentials.TransportConfig{})
	refreshers, err := credentials.DefaultRefreshers(transports, credentials.OAuthConfig{
		ClientID: os.Getenv("V3_GEMINI_CLIENT_ID"), ClientSecret: os.Getenv("V3_GEMINI_CLIENT_SECRET"),
	})
	if err != nil {
		transports.CloseIdle()
		return nil, nil, err
	}
	pool, err := credentials.New(credentials.NewPGStore(deps.PG.Pool, deps.Crypto), refreshers, credentials.Config{}, log)
	if err != nil {
		transports.CloseIdle()
		return nil, nil, err
	}
	return pool, transports.CloseIdle, nil
}

// startLoops runs every long-lived loop under group; the first failure
// cancels the rest.
func startLoops(ctx context.Context, group *errgroup.Group, deps *boot.Deps, s *services,
	credentialPool *credentials.Pool, cfg workerConfig, log *slog.Logger) {
	loops := []struct {
		name string
		run  func(context.Context) error
	}{
		{"catalog relay", s.relay.Run},
		{"worker catalog subscription", s.store.Run},
		{"ledger consumer", s.posting.Run},
		{"balance relay", ledger.NewBalanceRelay(deps.PG.Pool, deps.Redis, log).Run},
		{"credential refresh", credentialPool.Run},
		{"billing WAL replay", s.sweeper.Run},
		{"response background jobs", s.native.live.Run},
		{"account protection cache refresh", func(ctx context.Context) error { return runSecurityRefresh(ctx, s.guard, log) }},
		{"user refund recovery", s.refunds.Run},
		{"metrics server", func(ctx context.Context) error { return s.obs.serve(ctx, cfg.metricsAddr) }},
	}
	for _, l := range loops {
		group.Go(func() error { return runService(ctx, l.name, l.run) })
	}
}
