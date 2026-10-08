package incentives

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// The scheduled repair also releases a reservation after a crash between an
// order cancellation and its business hook. Paid orders retain their promise.
func (s *Service) releaseInactiveReferralReservations(ctx context.Context, limit int) error {
	rows, err := s.pool.Query(ctx, `SELECT q.order_id FROM v3_commerce.referral_consumption_qualifications q JOIN v3_commerce.orders o ON o.id=q.order_id WHERE q.state='reserved' AND o.state IN('canceled','expired','failed','refunded') ORDER BY q.id LIMIT $1`, limit)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return s.ReleaseReferralTx(ctx, tx, id) }); err != nil {
			return err
		}
	}
	return nil
}
