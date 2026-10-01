package marketplace

import (
	"context"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// Gifts serialize both owners in ascending user ID order. Replays are scoped
// to the sender and fingerprint the recipient and selected inventory.
func (s *Service) GiftBoxes(ctx context.Context, sender, recipient int64, requestID string, count int) ([]int64, error) {
	var ids []int64
	if err := validRequest(sender, requestID, count); err != nil {
		return nil, err
	}
	if recipient <= 0 || recipient == sender {
		return nil, ErrInvalidInput
	}
	input := struct {
		Recipient int64
		Count     int
	}{recipient, count}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockUsersAscending(ctx, tx, sender, recipient); err != nil {
			return err
		}
		found, err := replay(ctx, tx, sender, "gift", requestID, input, &ids)
		if err != nil || found {
			return err
		}
		if err := s.EnsureExternalInventoryTx(ctx, tx, sender); err != nil {
			return err
		}
		ids, err = lockGiftableItemsTx(ctx, tx, sender, count, s.cfg.Now())
		if err != nil {
			return err
		}
		if err := transferGiftedItemsTx(ctx, tx, sender, recipient, requestID, ids, s.cfg.Now()); err != nil {
			return err
		}
		return remember(ctx, tx, sender, "gift", requestID, input, ids)
	})
	return ids, err
}

// lockUsersAscending locks both account rows in ascending user-id order to
// keep lock acquisition order consistent across concurrent gift transfers.
func lockUsersAscending(ctx context.Context, tx pgx.Tx, a, b int64) error {
	users := []int64{a, b}
	slices.Sort(users)
	for _, id := range users {
		if err := lockUser(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}

// lockGiftableItemsTx locks up to count available inventory items owned by
// sender, returning ErrInventory if fewer are found.
func lockGiftableItemsTx(ctx context.Context, tx pgx.Tx, sender int64, count int, now time.Time) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT i.id FROM v3_marketplace.blind_box_items i JOIN v3_marketplace.blind_box_purchases p ON p.id=i.purchase_id LEFT JOIN v3_marketplace.blind_box_orders o ON o.id=p.external_order_id WHERE i.owner_user_id=$1 AND i.status='available' AND p.status='completed' AND (i.expires_at IS NULL OR i.expires_at>$3) AND (p.external_order_id IS NULL OR o.status IN('success','completed') AND (o.expires_at IS NULL OR o.expires_at>$3)) ORDER BY i.id LIMIT $2 FOR UPDATE OF i`, sender, count, now)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) != count {
		return nil, ErrInventory
	}
	return ids, nil
}

// transferGiftedItemsTx reassigns the locked items to recipient and records
// the gift and its item manifest.
func transferGiftedItemsTx(ctx context.Context, tx pgx.Tx, sender, recipient int64, requestID string, ids []int64, now time.Time) error {
	tag, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_items SET owner_user_id=$2,updated_at=$4 WHERE id=ANY($1::bigint[]) AND owner_user_id=$3 AND status='available'`, ids, recipient, sender, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != int64(len(ids)) {
		return ErrConflict
	}
	var gift int64
	if err := tx.QueryRow(ctx, `INSERT INTO v3_marketplace.blind_box_gifts(sender_user_id,recipient_user_id,quantity,request_id,created_at) VALUES($1,$2,$3,$4,$5) RETURNING id`, sender, recipient, len(ids), operation("gift", sender, requestID, 0), now).Scan(&gift); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_gift_items(gift_id,item_id,from_user_id,to_user_id,created_at) SELECT $1,unnest($2::bigint[]),$3,$4,$5`, gift, ids, sender, recipient, now)
	return err
}

func (s *Service) GiftProp(ctx context.Context, sender, recipient, propID int64, requestID string) error {
	if err := validRequest(sender, requestID, 1); err != nil {
		return err
	}
	if recipient <= 0 || recipient == sender || propID <= 0 {
		return ErrInvalidInput
	}
	input := struct{ Recipient, PropID int64 }{recipient, propID}
	var replayed bool
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockUsersAscending(ctx, tx, sender, recipient); err != nil {
			return err
		}
		found, err := replay(ctx, tx, sender, "gift_prop", requestID, input, &replayed)
		if err != nil || found {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_props SET user_id=$3,updated_at=$4 WHERE id=$1 AND user_id=$2 AND status='available'`, propID, sender, recipient, s.cfg.Now())
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
		if _, err := tx.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_prop_gifts(prop_id,sender_user_id,recipient_user_id,request_id,prop_type,prop_title,created_at) SELECT id,$2,$3,$4,prop_type,title,$5 FROM v3_marketplace.blind_box_props WHERE id=$1`, propID, sender, recipient, operation("gift_prop", sender, requestID, 0), s.cfg.Now()); err != nil {
			return err
		}
		return remember(ctx, tx, sender, "gift_prop", requestID, input, true)
	})
}
