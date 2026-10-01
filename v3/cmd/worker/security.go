package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/security"
)

func securityUsageHook(guard *security.Guard, income ledger.UsageHook) ledger.UsageHook {
	return func(ctx context.Context, tx pgx.Tx, fields map[string]string) error {
		if err := income(ctx, tx, fields); err != nil {
			return err
		}
		return guard.RecordUsageTx(ctx, tx, fields)
	}
}

func runSecurityRefresh(ctx context.Context, guard *security.Guard, log *slog.Logger) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			refresh, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, err := guard.FlushStateCache(refresh, 1000)
			cancel()
			if err != nil && ctx.Err() == nil {
				log.Error("account protection cache refresh failed; retrying committed outbox", "err", err)
			}
		}
	}
}
