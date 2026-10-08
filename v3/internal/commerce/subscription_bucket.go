package commerce

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// Caller holds the subscription and drained old account. Never reopen a retired
// Redis bucket: in-memory gateway profiles can still reference it briefly.
func (s *Service) rotateSubscriptionBucket(ctx context.Context, tx pgx.Tx, id, user, account int64, grant credits.Micro, operation string, last time.Time) (int64, error) {
	var balance credits.Micro
	if err := tx.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE id=$1 FOR UPDATE`, account).Scan(&balance); err != nil {
		return 0, err
	}
	if balance > 0 {
		if _, err := s.poster.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: -balance, Kind: "subscription_expire",
			OperationID: fmt.Sprintf("subscription:cycle:close:%d:%s", id, operation), Reason: "allowance cycle ended"}); err != nil {
			return 0, err
		}
	}
	var bucket, newAccount int64
	if err := tx.QueryRow(ctx, `SELECT nextval('v3_commerce.subscription_bucket_ids')`).Scan(&bucket); err != nil {
		return 0, err
	}
	// Positive owner ids identify initial subscription accounts. Negative ids
	// identify their later buckets, allocated uniquely from the bucket sequence.
	if err := tx.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind)
	 VALUES('subscription',$1,'subscription') RETURNING id`, -bucket).Scan(&newAccount); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE v3_commerce.subscription_buckets SET ended_at=$2 WHERE account_id=$1`, account, s.cfg.Now()); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_buckets(account_id,subscription_id,starts_at,policy_version) SELECT $1,id,$3,policy_version FROM v3_commerce.subscriptions WHERE id=$2`, newAccount, id, last); err != nil {
		return 0, err
	}
	if grant > 0 {
		if _, err := s.poster.PostTx(ctx, tx, billing.Entry{AccountID: newAccount, Amount: grant, Kind: "subscription_grant",
			OperationID: fmt.Sprintf("subscription:cycle:grant:%d:%s", id, operation), Metadata: map[string]any{"user_id": user, "subscription_id": id}}); err != nil {
			return 0, err
		}
	}
	return newAccount, nil
}
