package marketplace

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// groupMember is a group-buy participant's bonus-settlement state.
type groupMember struct {
	userID    int64
	accountID *int64
	subID     *int64
	granted   bool
}

func (s *Service) settleGroupTx(ctx context.Context, tx pgx.Tx, g *Group) error {
	members, err := loadGroupMembersTx(ctx, tx, g.ID)
	if err != nil {
		return err
	}
	if len(members) != g.CurrentCount {
		return ErrConflict
	}
	bonus := g.bonus()
	if err := s.prepareGroupBonusAccountsTx(ctx, tx, g.ID, bonus, members); err != nil {
		return err
	}
	if err := s.postGroupBonusesTx(ctx, tx, g.ID, bonus, members); err != nil {
		return err
	}
	return s.finalizeGroupSettlementTx(ctx, tx, g)
}

// loadGroupMembersTx loads every member of the group buy, ordered so that
// subscription-less members (if any) sort last and ties break by user id.
func loadGroupMembersTx(ctx context.Context, tx pgx.Tx, groupID int64) ([]groupMember, error) {
	rows, err := tx.Query(ctx, `SELECT user_id,account_id,subscription_id,bonus_granted FROM v3_marketplace.group_buy_members WHERE group_buy_id=$1 ORDER BY subscription_id NULLS LAST,user_id`, groupID)
	if err != nil {
		return nil, err
	}
	var members []groupMember
	for rows.Next() {
		var m groupMember
		if err := rows.Scan(&m.userID, &m.accountID, &m.subID, &m.granted); err != nil {
			rows.Close()
			return nil, err
		}
		members = append(members, m)
	}
	err = rows.Err()
	rows.Close()
	return members, err
}

// prepareGroupBonusAccountsTx re-resolves each ungranted member's current
// bonus account from their subscription (accounts can rotate between join
// and settlement) and persists the refreshed account id onto the member row.
func (s *Service) prepareGroupBonusAccountsTx(ctx context.Context, tx pgx.Tx, groupID int64, bonus credits.Micro, members []groupMember) error {
	if bonus == 0 {
		return nil
	}
	budgets, hasBudgets := s.purchases.(GroupBonusBudgets)
	for i := range members {
		m := &members[i]
		if bonus == 0 || m.accountID == nil || m.granted {
			continue
		}
		if !hasBudgets {
			return ErrUnavailable
		}
		if m.subID == nil || *m.subID <= 0 {
			return ErrInvalidInput
		}
		account, err := budgets.PrepareGroupBonusTx(ctx, tx, m.userID, *m.subID, bonus)
		if err != nil {
			return err
		}
		if account <= 0 {
			return ErrInvalidInput
		}
		m.accountID = &account
		if _, err := tx.Exec(ctx, `UPDATE v3_marketplace.group_buy_members SET account_id=$3 WHERE group_buy_id=$1 AND user_id=$2`, groupID, m.userID, account); err != nil {
			return err
		}
	}
	return nil
}

// postGroupBonusesTx posts the bonus ledger entry for every member who has an
// account and hasn't already been granted one, then marks them granted.
// Members are posted in ascending account-id order to keep ledger locks ordered.
func (s *Service) postGroupBonusesTx(ctx context.Context, tx pgx.Tx, groupID int64, bonus credits.Micro, members []groupMember) error {
	// Rotation can change account ordering; ledger locks follow the new IDs.
	sort.Slice(members, func(i, j int) bool {
		if members[i].accountID == nil {
			return false
		}
		return members[j].accountID == nil || *members[i].accountID < *members[j].accountID
	})
	for _, m := range members {
		if bonus == 0 || m.accountID == nil || m.granted {
			continue
		}
		if _, err := s.money.PostTx(ctx, tx, billing.Entry{AccountID: *m.accountID, Amount: bonus, Kind: "reward", OperationID: fmt.Sprintf("group-buy:%d:%d", groupID, m.userID), Reason: "group_buy_bonus"}); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `UPDATE v3_marketplace.group_buy_members SET bonus_granted=true,bonus_amount_micro=$2 WHERE group_buy_id=$1 AND account_id IS NOT NULL AND NOT bonus_granted`, groupID, bonus)
	return err
}

// finalizeGroupSettlementTx marks the group buy completed or expired and
// updates g in place to reflect the new status.
func (s *Service) finalizeGroupSettlementTx(ctx context.Context, tx pgx.Tx, g *Group) error {
	now := s.cfg.Now()
	status := "completed"
	if g.CurrentCount < 2 {
		status = "expired"
	}
	tag, err := tx.Exec(ctx, `UPDATE v3_marketplace.group_buys SET status=$3,settled_at=$2,updated_at=$2 WHERE id=$1 AND status='pending' AND (current_count=target_count OR expires_at<=$2)`, g.ID, now, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	g.Status = status
	g.SettledAt = &now
	return nil
}

func (g Group) bonus() credits.Micro {
	if g.BonusAt2Micro == 0 && g.BonusAt3Micro == 0 && g.BonusAt5Micro == 0 {
		if g.CurrentCount >= g.TargetCount {
			return g.BonusMicro
		}
		return 0
	}
	switch {
	case g.CurrentCount >= 5:
		return g.BonusAt5Micro
	case g.CurrentCount >= 3:
		return g.BonusAt3Micro
	case g.CurrentCount >= 2:
		return g.BonusAt2Micro
	default:
		return 0
	}
}

// ExpireGroups settles the earned 2/3/5-member tier when a room closes.
func (s *Service) ExpireGroups(ctx context.Context) (int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM v3_marketplace.group_buys WHERE status='pending' AND (expires_at<=$1 OR current_count=target_count) ORDER BY id LIMIT 100`, s.cfg.Now())
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	var count int64
	// One group per transaction keeps every account lock in ascending order
	// even when multiple workers select overlapping memberships.
	for _, id := range ids {
		settled := false
		err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			var g Group
			err := scanGroup(tx.QueryRow(ctx, `SELECT `+groupColumns+` FROM v3_marketplace.group_buys WHERE id=$1 AND status='pending' AND (expires_at<=$2 OR current_count=target_count) FOR UPDATE SKIP LOCKED`, id, s.cfg.Now()), &g)
			if errors.Is(err, ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			if err := s.settleGroupTx(ctx, tx, &g); err != nil {
				return err
			}
			settled = true
			return nil
		})
		if err != nil {
			return count, err
		}
		if settled {
			count++
		}
	}
	return count, nil
}
