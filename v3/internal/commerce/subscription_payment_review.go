package commerce

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type PackagePaymentReview struct {
	OrderID     int64     `json:"order_id"`
	UserID      int64     `json:"user_id"`
	TradeNo     string    `json:"trade_no"`
	Provider    string    `json:"provider"`
	AmountMinor int64     `json:"amount_minor"`
	Currency    string    `json:"currency"`
	Reason      string    `json:"reason"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Service) recordPackagePaymentReview(ctx context.Context, tx pgx.Tx, o Order, reason string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO v3_commerce.package_payment_reviews(order_id,user_id,reason)
	 VALUES($1,$2,$3) ON CONFLICT(order_id) DO NOTHING`, o.ID, o.UserID, reason); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE v3_commerce.orders SET fulfillment_state='requires_review' WHERE id=$1`, o.ID)
	return err
}

func (s *Service) ListPackagePaymentReviews(ctx context.Context) ([]PackagePaymentReview, error) {
	rows, err := s.pool.Query(ctx, `SELECT r.order_id,r.user_id,o.trade_no,o.provider,o.amount_minor,o.currency,r.reason,r.created_at
	 FROM v3_commerce.package_payment_reviews r JOIN v3_commerce.orders o ON o.id=r.order_id WHERE r.resolved_at IS NULL ORDER BY r.created_at,o.id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []PackagePaymentReview{}
	for rows.Next() {
		var row PackagePaymentReview
		if err = rows.Scan(&row.OrderID, &row.UserID, &row.TradeNo, &row.Provider, &row.AmountMinor, &row.Currency, &row.Reason, &row.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// A review is resolved only from an already verified full-refund receipt. An
// admin acknowledgement cannot invent a provider refund or a quota delivery.
func (s *Service) ResolvePackagePaymentReview(ctx context.Context, orderID int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE v3_commerce.package_payment_reviews r SET resolved_at=$2
	 FROM v3_commerce.orders o WHERE r.order_id=$1 AND o.id=r.order_id AND o.state='refunded'`, orderID, s.cfg.Now())
	if err == nil && tag.RowsAffected() == 0 {
		return ErrStateConflict
	}
	return err
}

func (h *handler) packagePaymentReviews(w http.ResponseWriter, r *http.Request, _ Actor) {
	result, err := h.s.ListPackagePaymentReviews(r.Context())
	respond(w, result, err)
}
func (h *handler) resolvePackagePaymentReview(w http.ResponseWriter, r *http.Request, _ Actor) {
	id, err := pathID(r)
	if err == nil {
		err = h.s.ResolvePackagePaymentReview(r.Context(), id)
	}
	respond(w, nil, err)
}
