package commerce

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func lockSubscriptionUserTx(ctx context.Context, tx pgx.Tx, id int64) error {
	var user int64
	err := tx.QueryRow(ctx, `SELECT u.id FROM v3_identity.users u JOIN v3_commerce.subscriptions s ON s.user_id=u.id WHERE s.id=$1 FOR UPDATE OF u`, id).Scan(&user)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// Expiry claims users before subscriptions, matching purchase and group
// restoration. A concurrent worker skips a user whose entitlements are busy.
func lockExpiredSubscriptionUsersTx(ctx context.Context, tx pgx.Tx, now time.Time, limit int) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT u.id FROM v3_identity.users u WHERE EXISTS(
	 SELECT 1 FROM v3_commerce.subscriptions s WHERE s.user_id=u.id AND s.state='active' AND s.expires_at<=$1
	 AND NOT EXISTS(SELECT 1 FROM v3_commerce.package_checkouts pc WHERE pc.target_subscription_id=s.id AND pc.state IN ('preparing','checkout'))
	 AND NOT EXISTS(SELECT 1 FROM v3_commerce.subscription_operations op WHERE op.subscription_id=s.id AND op.kind IN ('conversion','invalidate','delete') AND op.state='pending'))
	 ORDER BY u.id LIMIT $2 FOR UPDATE SKIP LOCKED`, now, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}
