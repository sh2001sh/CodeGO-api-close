package main

import (
	"context"
	"fmt"

	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

// channelMarketMaintenance runs bounded, idempotent work. Errors identify the
// failed phase so a missing dependency never masquerades as completed work.
func channelMarketMaintenance(ctx context.Context, service *channelmarket.Service) error {
	if service == nil {
		return channelmarket.ErrUnavailable
	}
	steps := []struct {
		name string
		run  func(context.Context) error
	}{
		{"release income", func(ctx context.Context) error { _, err := service.ReleaseIncome(ctx, 1000); return err }},
		{"queue automatic probes", func(ctx context.Context) error { _, err := service.QueueAutoProbes(ctx, 10); return err }},
		{"verify channels", func(ctx context.Context) error { _, err := service.ProcessVerification(ctx, 10); return err }},
		{"resume income reclaims", func(ctx context.Context) error { _, err := service.ResumeReclaims(ctx, 10); return err }},
		{"refresh rankings", func(ctx context.Context) error { _, err := service.RefreshRankings(ctx); return err }},
		{"rebuild automatic pools", func(ctx context.Context) error { _, err := service.RebuildPools(ctx, 100); return err }},
		{"execute charged batch tests", func(ctx context.Context) error { _, err := service.ProcessBatchTests(ctx, 2); return err }},
	}
	for _, step := range steps {
		if err := step.run(ctx); err != nil {
			return fmt.Errorf("channel market %s: %w", step.name, err)
		}
	}
	return nil
}
