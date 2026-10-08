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
	return applySubscriptionGroupTx(ctx, tx, subscriptionID, strings.TrimSpace(group), &planID, s.cfg.Now())
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
	return applySubscriptionGroupTx(ctx, tx, subscriptionID, strings.TrimSpace(group), nil, now)
}

func applySubscriptionGroupTx(ctx context.Context, tx pgx.Tx, id int64, target string, planID *int64, now time.Time) error {
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
		if strings.TrimSpace(previous) == "" {
			previous = current
		}
	}
	if priorTarget != "" && target == "" {
		// Replacing a group-bearing plan with a plain plan ends this group
		// benefit immediately, while its replacement credit package stays live.
		if err := RestoreSubscriptionGroupTx(ctx, tx, id, now); err != nil {
			return err
		}
	} else if priorTarget != "" && priorTarget != target && previous != "" {
		// A package upgrade replaces its saved group. Unlink the old group
		// before overwriting it so other packages cannot later restore it.
		if err := unlinkSubscriptionGroupTx(ctx, tx, user, id, priorTarget, previous); err != nil {
			return err
		}
	}
	if target != "" {
		if current != target {
			if _, err := tx.Exec(ctx, `UPDATE v3_identity.users SET group_name=$2 WHERE id=$1`, user, target); err != nil {
				return err
			}
		}
	}
	_, err := tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET upgrade_group=$2,prev_user_group=$3 WHERE id=$1`, id, target, previous)
	return err
}

// RestoreSubscriptionGroupTx removes this entitlement from the saved group
// chain and selects a remaining entitlement, or the original base group.
// An administrator's explicit group change still wins.
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
	if target == "" {
		return nil
	}
	if previous == "" {
		previous = target
	}
	var err error
	previous, err = subscriptionBaseGroupTx(ctx, tx, user, subscriptionID, previous)
	if err != nil {
		return err
	}
	// If A granted vip and B saved vip as its previous group, ending A must
	// replace B's previous group with A's baseline before B ends. Older same-
	// group grants saved an empty previous group; repair those in the same
	// transaction. Include expired rows awaiting the worker so batch ordering
	// cannot leave the last row restoring an already-expired entitlement.
	if err := unlinkSubscriptionGroupTx(ctx, tx, user, subscriptionID, target, previous); err != nil {
		return err
	}
	if current != target {
		return nil
	}
	var remaining string
	err = tx.QueryRow(ctx, `SELECT upgrade_group FROM v3_commerce.subscriptions
	 WHERE user_id=$1 AND id<>$2 AND (state='active' AND starts_at<=$3 AND expires_at>$3 OR benefits_until>$3)
	 AND deleted_at IS NULL AND btrim(upgrade_group)<>'' ORDER BY starts_at DESC,id DESC LIMIT 1`, user, subscriptionID, now).Scan(&remaining)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		previous = strings.TrimSpace(remaining)
	}
	if previous == current {
		return nil
	}
	_, err = tx.Exec(ctx, `UPDATE v3_identity.users SET group_name=$2 WHERE id=$1`, user, previous)
	return err
}

func unlinkSubscriptionGroupTx(ctx context.Context, tx pgx.Tx, user, id int64, target, previous string) error {
	_, err := tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET prev_user_group=$4
	 WHERE user_id=$1 AND id<>$2 AND deleted_at IS NULL AND (state='active' OR benefits_until IS NOT NULL)
	 AND (btrim(prev_user_group)=$3 OR (btrim(prev_user_group)='' AND btrim(upgrade_group)=$3))`, user, id, target, previous)
	return err
}

// Existing overlapping snapshots may still reference a predecessor that ended
// before this fix. Follow only earlier overlapping grants, never unrelated old
// history. The (starts_at,id) tuple strictly decreases so a corrupt group cycle
// cannot make this traversal loop.
func subscriptionBaseGroupTx(ctx context.Context, tx pgx.Tx, user, id int64, previous string) (string, error) {
	var start time.Time
	if err := tx.QueryRow(ctx, `SELECT starts_at FROM v3_commerce.subscriptions WHERE id=$1`, id).Scan(&start); err != nil {
		return "", err
	}
	for {
		var priorID int64
		var priorStart time.Time
		var baseline string
		err := tx.QueryRow(ctx, `SELECT id,starts_at,prev_user_group FROM v3_commerce.subscriptions
		 WHERE user_id=$1 AND (starts_at,id)<($3::timestamptz,$4::bigint) AND btrim(upgrade_group)=$2
		 AND btrim(prev_user_group)<>'' AND btrim(prev_user_group)<>btrim(upgrade_group)
		 AND CASE WHEN benefits_until IS NOT NULL THEN benefits_until ELSE LEAST(expires_at,COALESCE(ended_at,expires_at)) END >=$3
		 ORDER BY starts_at DESC,id DESC LIMIT 1`, user, previous, start, id).Scan(&priorID, &priorStart, &baseline)
		if errors.Is(err, pgx.ErrNoRows) {
			return previous, nil
		}
		if err != nil {
			return "", err
		}
		id, start, previous = priorID, priorStart, strings.TrimSpace(baseline)
	}
}
