package commerce

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func (s *Service) checkPackagePending(ctx context.Context, tx pgx.Tx, id int64) error {
	var pending bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.package_checkouts WHERE target_subscription_id=$1 AND state IN ('preparing','checkout'))
	 OR EXISTS(SELECT 1 FROM v3_commerce.subscription_operations WHERE subscription_id=$1 AND kind IN ('conversion','invalidate','delete') AND state='pending')`, id).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return ErrFundingPending
	}
	return nil
}
