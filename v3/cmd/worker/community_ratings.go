package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/community"
)

func runCommunityRatingSync(ctx context.Context, deps *boot.Deps, log *slog.Logger) error {
	relay, err := community.NewRatingRelay(deps.PG.Pool, os.Getenv("CODEGO_COMMUNITY_RATING_EVENTS_URL"), os.Getenv("CODEGO_COMMUNITY_API_SECRET"), log)
	if err != nil {
		return err
	}
	return relay.Run(ctx)
}
