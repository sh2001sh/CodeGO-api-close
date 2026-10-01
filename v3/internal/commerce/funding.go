package commerce

import (
	"context"

	"github.com/sh2001sh/new-api/v3/internal/billing"
)

// ActiveFundingSources is loaded off the gateway request path into the immutable
// billing account profile. It also excludes future starts and canceled buckets.
func (s *Service) ActiveFundingSources(ctx context.Context) ([]billing.FundingSource, error) {
	rows, err := s.pool.Query(ctx, `SELECT user_id,account_id,LEAST(expires_at,COALESCE(next_reset_at,expires_at)) FROM v3_commerce.subscriptions
	    WHERE state='active' AND account_id IS NOT NULL AND starts_at<=$1 AND expires_at>$1
	    AND NOT EXISTS(SELECT 1 FROM v3_commerce.package_checkouts pc WHERE pc.target_subscription_id=v3_commerce.subscriptions.id AND pc.state IN ('preparing','checkout')) AND NOT EXISTS(SELECT 1 FROM v3_commerce.subscription_operations op WHERE op.subscription_id=v3_commerce.subscriptions.id AND op.kind IN ('conversion','invalidate','delete') AND op.state='pending')
	    ORDER BY user_id,expires_at,account_id`, s.cfg.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]billing.FundingSource, 0)
	for rows.Next() {
		var source billing.FundingSource
		if err = rows.Scan(&source.UserID, &source.AccountID, &source.ExpiresAt); err != nil {
			return nil, err
		}
		result = append(result, source)
	}
	return result, rows.Err()
}
