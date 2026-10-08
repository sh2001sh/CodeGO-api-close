package commerce

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Consuming funding must not revoke an already-paid benefit early. The same
// expiry worker restores the user's group only at the original benefit expiry.
func (s *Service) expireConvertedBenefits(ctx context.Context, limit int) (int, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM v3_commerce.subscriptions WHERE converted_at IS NOT NULL AND benefits_until<=$1 ORDER BY benefits_until,id LIMIT $2`, s.cfg.Now(), limit)
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return 0, err
	}
	count := 0
	for _, id := range ids {
		err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if err := lockSubscriptionUserTx(ctx, tx, id); err != nil {
				return err
			}
			tag, err := tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET benefits_until=NULL WHERE id=$1 AND benefits_until<=$2`, id, s.cfg.Now())
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return nil
			}
			if err = RestoreSubscriptionGroupTx(ctx, tx, id, s.cfg.Now()); err != nil {
				return err
			}
			count++
			return nil
		})
		if err != nil {
			return count, err
		}
	}
	return count, nil
}
