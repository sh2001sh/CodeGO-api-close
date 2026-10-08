package marketplace

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// SimulateBatch reads an official snapshot and performs no inventory, money,
// request, limit or progress writes. A new call starts from official progress.
func (s *Service) SimulateBatch(ctx context.Context, user, batch int64, count int) (BatchDrawResult, error) {
	var out BatchDrawResult
	if user <= 0 || batch <= 0 || count < 1 || count > 100 {
		return out, ErrInvalidInput
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL)`, user).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		b, err := scanBatch(tx.QueryRow(ctx, `SELECT `+batchColumns+` FROM v3_marketplace.blind_box_batches WHERE id=$1`, batch))
		if err != nil {
			return err
		}
		if b.State != "published" && b.State != "paused" {
			return ErrConflict
		}
		if b.RemainingCount < int64(count) {
			return ErrInventory
		}
		out.Pity, err = readBatchPityTx(ctx, tx, user)
		if err != nil {
			return err
		}
		out.BatchID, out.Records = batch, make([]OpenRecord, 0, count)
		for range count {
			record, err := drawBatchRecord(&b, &out.Pity, s.cfg.Draw)
			if err != nil {
				return err
			}
			out.Records = append(out.Records, record)
		}
		return nil
	})
	if err != nil {
		return BatchDrawResult{}, err
	}
	return out, nil
}
