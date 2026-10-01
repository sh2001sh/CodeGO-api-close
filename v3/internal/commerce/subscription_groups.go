package commerce

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ApplySubscriptionUpgradeGroupTx snapshots the destination plan's group in
// the same transaction as the grant or package replacement. Callers must lock
// the user before locking an existing subscription to keep lifecycle ordering.
func (s *Service) ApplySubscriptionUpgradeGroupTx(ctx context.Context, tx pgx.Tx, subscriptionID, planID int64) error {
	if tx == nil || subscriptionID <= 0 || planID <= 0 {
		return ErrInvalid
	}
	var group string
	if err := tx.QueryRow(ctx, `SELECT upgrade_group FROM v3_commerce.plans WHERE id=$1 FOR SHARE`, planID).Scan(&group); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return applySubscriptionGroupTx(ctx, tx, subscriptionID, strings.TrimSpace(group), &planID)
}

// ReapplySubscriptionGroupTx restores an active entitlement using its saved
// policy, so a later plan edit cannot alter a rejected refund's restoration.
func ReapplySubscriptionGroupTx(ctx context.Context, tx pgx.Tx, subscriptionID int64, now time.Time) error {
	if tx == nil || subscriptionID <= 0 {
		return ErrInvalid
	}
	var group string
	err := tx.QueryRow(ctx, `SELECT upgrade_group FROM v3_commerce.subscriptions
	 WHERE id=$1 AND state='active' AND deleted_at IS NULL AND expires_at>$2`, subscriptionID, now).Scan(&group)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return applySubscriptionGroupTx(ctx, tx, subscriptionID, strings.TrimSpace(group), nil)
}

func applySubscriptionGroupTx(ctx context.Context, tx pgx.Tx, id int64, target string, planID *int64) error {
	var user int64
	if err := tx.QueryRow(ctx, `SELECT user_id FROM v3_commerce.subscriptions WHERE id=$1`, id).Scan(&user); err != nil {
		return err
	}
	var current string
	if err := tx.QueryRow(ctx, `SELECT group_name FROM v3_identity.users WHERE id=$1 FOR UPDATE`, user).Scan(&current); err != nil {
		return err
	}
	var previous, priorTarget string
	var actualPlan int64
	if err := tx.QueryRow(ctx, `SELECT plan_id,upgrade_group,prev_user_group FROM v3_commerce.subscriptions WHERE id=$1 FOR UPDATE`, id).
		Scan(&actualPlan, &priorTarget, &previous); err != nil {
		return err
	}
	if planID != nil && actualPlan != *planID {
		return ErrStateConflict
	}
	if target != "" {
		var available bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_catalog.groups WHERE name=$1)`, target).Scan(&available); err != nil {
			return err
		}
		if !available {
			return ErrInvalid
		}
		if strings.TrimSpace(previous) == "" && (current != target || priorTarget != "") {
			previous = current
		}
		if current != target {
			if _, err := tx.Exec(ctx, `UPDATE v3_identity.users SET group_name=$2 WHERE id=$1`, user, target); err != nil {
				return err
			}
		}
	}
	_, err := tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET upgrade_group=$2,prev_user_group=$3 WHERE id=$1`, id, target, previous)
	return err
}

// RestoreSubscriptionGroupTx follows the source guard: another live group
// entitlement suppresses downgrade, and an administrator's group change wins.
// Call only after the subscription has ceased providing the entitlement.
func RestoreSubscriptionGroupTx(ctx context.Context, tx pgx.Tx, subscriptionID int64, now time.Time) error {
	if tx == nil || subscriptionID <= 0 {
		return ErrInvalid
	}
	var user int64
	if err := tx.QueryRow(ctx, `SELECT user_id FROM v3_commerce.subscriptions WHERE id=$1`, subscriptionID).Scan(&user); err != nil {
		return err
	}
	var current string
	if err := tx.QueryRow(ctx, `SELECT group_name FROM v3_identity.users WHERE id=$1 FOR UPDATE`, user).Scan(&current); err != nil {
		return err
	}
	var target, previous string
	if err := tx.QueryRow(ctx, `SELECT upgrade_group,prev_user_group FROM v3_commerce.subscriptions WHERE id=$1 FOR UPDATE`, subscriptionID).Scan(&target, &previous); err != nil {
		return err
	}
	target, previous = strings.TrimSpace(target), strings.TrimSpace(previous)
	if target == "" || previous == "" || current != target || current == previous {
		return nil
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.subscriptions
	 WHERE user_id=$1 AND id<>$2 AND state='active' AND expires_at>$3 AND deleted_at IS NULL AND btrim(upgrade_group)<>'')`, user, subscriptionID, now).Scan(&active); err != nil {
		return err
	}
	if active {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE v3_identity.users SET group_name=$2 WHERE id=$1`, user, previous)
	return err
}
