package main

import (
	"context"
	"time"

	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/audit"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
)

// maintenance returns the River maintenance actions and how often each runs.
// sample_cleanup is scheduled only when an audit retention is configured.
func maintenance(deps *boot.Deps, s *services, cfg workerConfig) (map[string]func(context.Context) error, map[string]time.Duration) {
	periods := map[string]time.Duration{
		"reservation_sweep":       30 * time.Second,
		"reconcile":               cfg.reconcileEvery,
		"usage_partitions":        24 * time.Hour,
		"commerce_expire":         time.Minute,
		"marketplace_expire":      time.Minute,
		"channel_market":          time.Minute,
		"workflow":                10 * time.Second,
		"lucky_reward_recovery":   time.Minute,
		"referral_rewards":        time.Minute,
		"blind_box_contributions": time.Minute,
	}
	if cfg.retentionDays > 0 {
		periods["sample_cleanup"] = 24 * time.Hour
	}
	actions := map[string]func(context.Context) error{
		"reservation_sweep": func(ctx context.Context) error { _, err := s.sweeper.SweepExpired(ctx, 1000); return err },
		"reconcile":         s.obs.reconcile,
		"workflow": func(ctx context.Context) error {
			_, err := s.native.workflow.Reconcile(ctx, 8)
			return err
		},
		"usage_partitions": func(ctx context.Context) error {
			return ledger.EnsureUsagePartitions(ctx, deps.PG.Pool, time.Now().UTC())
		},
		"commerce_expire":       s.commerceMaintenance,
		"lucky_reward_recovery": s.rewards.RecoverLuckyRewards,
		"referral_rewards": func(ctx context.Context) error {
			_, err := s.rewards.SettleReferrals(ctx, 100)
			return err
		},
		"marketplace_expire": s.marketplaceMaintenance,
		"blind_box_contributions": func(ctx context.Context) error {
			_, err := s.market.AccrueBatchEntitlements(ctx, 100)
			return err
		},
		"channel_market": func(ctx context.Context) error {
			return channelMarketMaintenance(ctx, s.channelMarket)
		},
		"sample_cleanup": func(ctx context.Context) error {
			cutoff := time.Now().UTC().AddDate(0, 0, -cfg.retentionDays)
			_, err := audit.New(deps.PG.Pool, audit.Config{}).DeleteSamplesBefore(ctx, cutoff)
			return err
		},
	}
	return actions, periods
}

// commerceMaintenance runs every order and subscription recovery pass. Each
// step is idempotent, so a failure part way is resumed by the next run.
func (s *services) commerceMaintenance(ctx context.Context) error {
	c := s.commerce
	if _, err := c.ExpireOrders(ctx); err != nil {
		return err
	}
	if _, err := c.RecoverCashBoxOrders(ctx, 1000); err != nil {
		return err
	}
	for _, step := range []func(context.Context, int) (int, error){
		c.RecoverCheckoutDiscounts, c.RecoverPackageCheckouts, c.RecoverSubscriptionChanges,
		c.ResetDueSubscriptions, c.ExpireSubscriptions,
	} {
		if _, err := step(ctx, 1000); err != nil {
			return err
		}
	}
	return nil
}

func (s *services) marketplaceMaintenance(ctx context.Context) error {
	if _, err := s.market.ExpireGroups(ctx); err != nil {
		return err
	}
	_, err := s.market.ExpireProps(ctx, 1000)
	return err
}
