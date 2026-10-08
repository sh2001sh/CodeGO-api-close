package commerce

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

func (s *Service) applyRedemptionTx(ctx context.Context, tx pgx.Tx, user int64, operation string, result *RedemptionResult, snapshot Plan) error {
	switch result.RedeemType {
	case "credits":
		account, err := walletTx(ctx, tx, user)
		if err != nil {
			return err
		}
		_, err = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: result.Credits, Kind: "redeem", OperationID: operation})
		return err
	case "subscription":
		result.Credits = 0
		return s.redeemSubscriptionTx(ctx, tx, user, operation, result, snapshot)
	case "blind_box":
		result.Credits = 0
		return s.redeemBoxesTx(ctx, tx, user, operation, result)
	default:
		return ErrInvalid
	}
}

func (s *Service) redeemSubscriptionTx(ctx context.Context, tx pgx.Tx, user int64, operation string, result *RedemptionResult, snapshot Plan) error {
	var locked int64
	if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, user).Scan(&locked); err != nil {
		return err
	}
	p := snapshot
	var err error
	if p.ID == 0 {
		p, err = scanPlan(tx.QueryRow(ctx, `SELECT `+planColumns+` FROM v3_commerce.plans WHERE id=$1 FOR SHARE`, result.PlanID))
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if p.MaxPurchasePerUser > 0 {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscriptions WHERE user_id=$1 AND plan_id=$2 AND deleted_at IS NULL`, user, p.ID).Scan(&count); err != nil {
			return err
		}
		if count >= p.MaxPurchasePerUser {
			return ErrStateConflict
		}
	}
	o := Order{PolicyVersion: p.PolicyVersion, PlanSnapshot: p, UserID: user, PlanID: &p.ID, Credits: p.Credits, PeriodCredits: p.PeriodCredits, PeriodSeconds: p.PeriodSeconds,
		ResetPeriod: p.ResetPeriod, ResetCustomSeconds: p.ResetCustomSeconds, TradeNo: operation,
		DurationUnit: p.DurationUnit, DurationValue: p.DurationValue, CustomSeconds: p.CustomSeconds,
		LegacyPeriodic: p.PeriodCredits == 0 && p.ResetPeriod != "never"}
	if err = s.grantSubscription(ctx, tx, o); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `UPDATE v3_commerce.subscriptions SET source='redemption' WHERE reward_operation=$1 RETURNING v3_commerce.subscriptions.id`, operation).Scan(&result.UserSubscriptionID); err != nil {
		return err
	}
	result.PlanTitle = p.Name
	return nil
}

func (s *Service) redeemBoxesTx(ctx context.Context, tx pgx.Tx, user int64, operation string, result *RedemptionResult) error {
	if s.cfg.CashBoxes == nil {
		return ErrProviderUnavailable
	}
	var poolID int64
	rows, err := tx.Query(ctx, `SELECT id FROM v3_marketplace.blind_box_pools WHERE scope='standard' AND enabled ORDER BY id LIMIT 2`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	if len(ids) != 1 {
		return ErrProviderUnavailable
	}
	poolID = ids[0]
	o, err := s.cfg.CashBoxes.CreateBoxOrderTx(ctx, tx, marketplace.BoxOrderInput{UserID: user, PoolID: poolID, Quantity: result.BlindBoxQuantity,
		TradeNo: operation, Currency: "cny", PaymentProvider: "redemption", PaymentMethod: "redemption", Source: "redemption"})
	if err != nil {
		return cashBoxError(err)
	}
	if _, err = s.cfg.CashBoxes.CompleteBoxOrderTx(ctx, tx, user, operation, 0, "cny"); err != nil {
		return cashBoxError(err)
	}
	result.BlindBoxOrderID = o.ID
	return nil
}
