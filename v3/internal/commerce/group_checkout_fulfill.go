package commerce

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// ValidateGroupFulfillmentTx holds the room through the subsequent grant and
// enrollment. A stale explicit room records the paid receipt for reconciliation
// without granting a package or pretending the member was admitted.
func (s *Service) ValidateGroupFulfillmentTx(ctx context.Context, tx pgx.Tx, o Order) (bool, error) {
	if !isGroupPurchase(o.PurchaseType) {
		return false, nil
	}
	if s.cfg.GroupCheckouts == nil || o.PlanID == nil {
		return false, ErrProviderUnavailable
	}
	var user int64
	if err := tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, o.UserID).Scan(&user); err != nil {
		return false, err
	}
	// Every auto room chooser for one plan must serialize, including the empty
	// pool case where there is no row to lock yet.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "commerce:group-plan:"+strconv.FormatInt(*o.PlanID, 10)); err != nil {
		return false, err
	}
	var requested int64
	var state string
	err := tx.QueryRow(ctx, `SELECT COALESCE(requested_group_id,0),state FROM v3_commerce.group_checkouts
	 WHERE order_id=$1 AND user_id=$2 FOR UPDATE`, o.ID, o.UserID).Scan(&requested, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, s.recordPackagePaymentReview(ctx, tx, o, "group checkout intent is missing")
	}
	if err != nil {
		return false, err
	}
	if state == "review" {
		return true, nil
	}
	id, err := s.selectCheckoutGroupTx(ctx, tx, o, requested)
	if errors.Is(err, ErrStateConflict) || errors.Is(err, ErrNotFound) {
		if _, saveErr := tx.Exec(ctx, `UPDATE v3_commerce.group_checkouts SET state='review',review_reason='group is full, expired, mismatched or already joined' WHERE order_id=$1`, o.ID); saveErr != nil {
			return false, saveErr
		}
		return true, s.recordPackagePaymentReview(ctx, tx, o, "group is full, expired, mismatched or already joined")
	}
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_commerce.group_checkouts SET selected_group_id=NULLIF($2,0) WHERE order_id=$1`, o.ID, id)
	return false, err
}

// ApplyGroupCheckoutTx must follow successful package delivery in the same
// transaction. A lifecycle review skips group admission as well.
func (s *Service) ApplyGroupCheckoutTx(ctx context.Context, tx pgx.Tx, o Order) error {
	if !isGroupPurchase(o.PurchaseType) {
		return nil
	}
	var review bool
	if err := tx.QueryRow(ctx, `SELECT fulfillment_state='requires_review' FROM v3_commerce.orders WHERE id=$1`, o.ID).Scan(&review); err != nil {
		return err
	}
	if review {
		_, err := tx.Exec(ctx, `UPDATE v3_commerce.group_checkouts SET state='review',review_reason='package delivery requires review' WHERE order_id=$1`, o.ID)
		return err
	}
	var groupID int64
	var state string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(selected_group_id,0),state FROM v3_commerce.group_checkouts WHERE order_id=$1 FOR UPDATE`, o.ID).Scan(&groupID, &state); err != nil {
		return err
	}
	if state == "applied" {
		return nil
	}
	if state != "pending" || s.cfg.GroupCheckouts == nil {
		return ErrStateConflict
	}
	if groupID == 0 {
		g, err := s.cfg.GroupCheckouts.CreateGroupTx(ctx, tx, o.UserID, o.ID)
		if err != nil {
			return err
		}
		groupID = g.ID
	} else if _, err := s.cfg.GroupCheckouts.JoinGroupTx(ctx, tx, o.UserID, groupID, o.ID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE v3_commerce.group_checkouts SET selected_group_id=$2,state='applied',applied_at=$3 WHERE order_id=$1`, o.ID, groupID, s.cfg.Now())
	return err
}
