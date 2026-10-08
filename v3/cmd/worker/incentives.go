package main

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/incentives"
)

func (s *services) buildIncentives(deps *boot.Deps) {
	s.rewards = incentives.New(deps.PG.Pool, s.poster, incentives.Config{
		ResetSubscriptionTx: s.commerce.ResetRewardSubscriptionTx,
	})
	s.commerce.SetBeforeOrderHook(func(ctx context.Context, tx pgx.Tx, order commerce.Order) error {
		return s.rewards.ReserveReferralTx(ctx, tx, order.ID)
	})
	s.commerce.SetOrderReleasedHook(func(ctx context.Context, tx pgx.Tx, order commerce.Order) error {
		return s.rewards.ReleaseReferralTx(ctx, tx, order.ID)
	})
	s.commerce.SetPaidPurchaseHook(func(ctx context.Context, tx pgx.Tx, order commerce.Order) error {
		planID, source := int64(0), "topup_order"
		if order.PlanID != nil {
			planID, source = *order.PlanID, "subscription_order"
		}
		return s.rewards.PurchaseTx(ctx, tx, incentives.Purchase{
			UserID: order.UserID, OrderID: order.ID, PlanID: planID,
			AmountMinor: order.AmountMinor, SourceType: source, SourceID: order.TradeNo,
		})
	})
}
