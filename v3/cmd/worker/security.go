package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/security"
)

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
