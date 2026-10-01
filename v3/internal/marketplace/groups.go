package marketplace

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type Group struct {
	ID            int64         `json:"id"`
	InitiatorID   int64         `json:"initiator_id"`
	PlanID        int64         `json:"plan_id"`
	Status        string        `json:"status"`
	TargetCount   int           `json:"target_count"`
	CurrentCount  int           `json:"current_count"`
	BonusMicro    credits.Micro `json:"bonus_micro"`
	BonusAt2Micro credits.Micro `json:"bonus_at_2_micro"`
	BonusAt3Micro credits.Micro `json:"bonus_at_3_micro"`
	BonusAt5Micro credits.Micro `json:"bonus_at_5_micro"`
	ExpiresAt     time.Time     `json:"expires_at"`
	SettledAt     *time.Time    `json:"settled_at,omitempty"`
}

const groupColumns = `id,initiator_id,plan_id,status,target_count,current_count,bonus_micro,bonus_at_2_micro,bonus_at_3_micro,bonus_at_5_micro,expires_at,settled_at`

func scanGroup(row pgx.Row, g *Group) error {
	err := row.Scan(&g.ID, &g.InitiatorID, &g.PlanID, &g.Status, &g.TargetCount, &g.CurrentCount, &g.BonusMicro, &g.BonusAt2Micro, &g.BonusAt3Micro, &g.BonusAt5Micro, &g.ExpiresAt, &g.SettledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// CreateGroup is called by commerce after a paid group-enabled purchase. The
// original order can participate once; retrying returns its original group.
func (s *Service) CreateGroup(ctx context.Context, userID, orderID int64) (Group, error) {
	var g Group
	if userID <= 0 || orderID <= 0 {
		return g, ErrInvalidInput
	}
	if s.purchases == nil {
		return g, ErrUnavailable
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		g, err = s.CreateGroupTx(ctx, tx, userID, orderID)
		return err
	})
	return g, stateError(err)
}

// CreateGroupTx joins the paid order's transaction so a failed group admission
// rolls back its subscription grant and payment state together.
func (s *Service) CreateGroupTx(ctx context.Context, tx pgx.Tx, userID, orderID int64) (Group, error) {
	var g Group
	if tx == nil || userID <= 0 || orderID <= 0 {
		return g, ErrInvalidInput
	}
	if s.purchases == nil {
		return g, ErrUnavailable
	}
	err := func() error {
		if err := lockUser(ctx, tx, userID); err != nil {
			return err
		}
		var existingID, owner int64
		err := tx.QueryRow(ctx, `SELECT group_buy_id,user_id FROM v3_marketplace.group_buy_members WHERE order_id=$1`, orderID).Scan(&existingID, &owner)
		if err == nil {
			if owner != userID {
				return ErrConflict
			}
			return scanGroup(tx.QueryRow(ctx, `SELECT `+groupColumns+` FROM v3_marketplace.group_buys WHERE id=$1`, existingID), &g)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		p, err := s.purchases.GroupPurchaseTx(ctx, tx, userID, orderID)
		if err != nil {
			return err
		}
		if !p.Enabled || p.PlanID <= 0 || p.OrderID != orderID || p.TargetCount < 2 || p.TargetCount > 1000 || p.BonusMicro < 0 || p.Lifetime <= 0 {
			return ErrInvalidInput
		}
		if p.BonusAt2Micro < 0 || p.BonusAt3Micro < 0 || p.BonusAt5Micro < 0 {
			return ErrInvalidInput
		}
		if p.BonusAccountID <= 0 || p.SubscriptionID <= 0 {
			return ErrInvalidInput
		}
		err = scanGroup(tx.QueryRow(ctx, `INSERT INTO v3_marketplace.group_buys(initiator_id,plan_id,target_count,bonus_micro,bonus_at_2_micro,bonus_at_3_micro,bonus_at_5_micro,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+groupColumns, userID, p.PlanID, p.TargetCount, p.BonusMicro, p.BonusAt2Micro, p.BonusAt3Micro, p.BonusAt5Micro, s.cfg.Now().Add(p.Lifetime)), &g)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO v3_marketplace.group_buy_members(group_buy_id,user_id,account_id,order_id,subscription_id) VALUES($1,$2,$3,$4,$5)`, g.ID, userID, p.BonusAccountID, orderID, p.SubscriptionID)
		return err
	}()
	return g, stateError(err)
}

func (s *Service) JoinGroup(ctx context.Context, userID, groupID, orderID int64) (Group, error) {
	var g Group
	if userID <= 0 || groupID <= 0 || orderID <= 0 {
		return g, ErrInvalidInput
	}
	if s.purchases == nil {
		return g, ErrUnavailable
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		g, err = s.JoinGroupTx(ctx, tx, userID, groupID, orderID)
		return err
	})
	return g, stateError(err)
}

// JoinGroupTx requires the caller to roll back on an admission error, including
// a paid order whose room filled or expired before its payment arrived.
func (s *Service) JoinGroupTx(ctx context.Context, tx pgx.Tx, userID, groupID, orderID int64) (Group, error) {
	var g Group
	if tx == nil || userID <= 0 || groupID <= 0 || orderID <= 0 {
		return g, ErrInvalidInput
	}
	if s.purchases == nil {
		return g, ErrUnavailable
	}
	err := func() error {
		if err := lockUser(ctx, tx, userID); err != nil {
			return err
		}
		if err := scanGroup(tx.QueryRow(ctx, `SELECT `+groupColumns+` FROM v3_marketplace.group_buys WHERE id=$1 FOR UPDATE`, groupID), &g); err != nil {
			return err
		}
		var existingOrder int64
		err := tx.QueryRow(ctx, `SELECT order_id FROM v3_marketplace.group_buy_members WHERE group_buy_id=$1 AND user_id=$2`, groupID, userID).Scan(&existingOrder)
		if err == nil {
			if existingOrder != orderID {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if g.Status != "pending" || !s.cfg.Now().Before(g.ExpiresAt) || g.CurrentCount >= g.TargetCount {
			return ErrConflict
		}
		p, err := s.purchases.GroupPurchaseTx(ctx, tx, userID, orderID)
		if err != nil {
			return err
		}
		if !p.Enabled || p.PlanID != g.PlanID || p.OrderID != orderID {
			return ErrConflict
		}
		if p.BonusAccountID <= 0 || p.SubscriptionID <= 0 {
			return ErrInvalidInput
		}
		_, err = tx.Exec(ctx, `INSERT INTO v3_marketplace.group_buy_members(group_buy_id,user_id,account_id,order_id,subscription_id) VALUES($1,$2,$3,$4,$5)`, groupID, userID, p.BonusAccountID, orderID, p.SubscriptionID)
		if err != nil {
			return err
		}
		if err := scanGroup(tx.QueryRow(ctx, `UPDATE v3_marketplace.group_buys SET current_count=current_count+1,updated_at=$2 WHERE id=$1 AND status='pending' AND current_count<target_count RETURNING `+groupColumns, groupID, s.cfg.Now()), &g); err != nil {
			return err
		}
		if g.CurrentCount == g.TargetCount {
			return s.settleGroupTx(ctx, tx, &g)
		}
		return nil
	}()
	return g, stateError(err)
}

func (s *Service) GetGroup(ctx context.Context, id int64) (Group, error) {
	var g Group
	err := scanGroup(s.pool.QueryRow(ctx, `SELECT `+groupColumns+` FROM v3_marketplace.group_buys WHERE id=$1`, id), &g)
	return g, err
}

func (s *Service) ListGroups(ctx context.Context, userID, before int64, limit int) ([]Group, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	if before <= 0 {
		before = 9223372036854775807
	}
	rows, err := s.pool.Query(ctx, `SELECT `+groupColumns+` FROM v3_marketplace.group_buys g WHERE g.id<$1 AND ($2::bigint=0 OR EXISTS(SELECT 1 FROM v3_marketplace.group_buy_members m WHERE m.group_buy_id=g.id AND m.user_id=$2)) ORDER BY g.id DESC LIMIT $3`, before, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]Group, 0)
	for rows.Next() {
		var g Group
		if err := scanGroup(rows, &g); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}
