package commerce

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func loadWalletConversionSources(ctx context.Context, q rowQuerierCommerce, f walletConversionFacts) ([]WalletConversionSource, error) {
	var sources []WalletConversionSource
	err := q.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object(
 'order_id',o.id,'state',o.state,'purchase_type',o.purchase_type,'credits',o.credits,
 'revenue_credits',o.recognized_revenue_credits,
 'previously_paid',COALESCE((SELECT sum(original_amount) FROM v3_billing.funding_lots l
 WHERE l.source='subscription_conversion' AND l.reference_type<>'peer_transfer'
 AND l.metadata->>'original_order_id'=o.id::text AND NOT l.non_transferable),0),
 'pending_refund',EXISTS(SELECT 1 FROM v3_commerce.user_refunds r WHERE r.order_id=o.id AND r.status='processing')) ORDER BY o.id),'[]')
 FROM v3_commerce.orders o WHERE o.user_id=$1 AND o.kind='subscription'
 AND (o.id=$2 OR o.target_subscription_id=$3 OR EXISTS(SELECT 1 FROM v3_commerce.subscription_fuel_fulfillments ff WHERE ff.order_id=o.id AND ff.subscription_id=$3 AND NOT ff.revoked))`, f.User, f.Order, f.ID).Scan(&sources)
	return sources, err
}

// Forecast only grants the existing reset algorithm would issue before expiry,
// assuming full consumption of each granted bucket. Past missed cycles do not
// manufacture grants; due resets must run first so the evidence is unambiguous.
func walletFutureCredits(f walletConversionFacts, now time.Time) (credits.Micro, error) {
	if f.NextReset == nil {
		return 0, nil
	}
	if !f.NextReset.After(now) {
		return 0, ErrStateConflict
	}
	used, err := f.Used.Add(f.Spent)
	if err != nil || used < 0 || f.Balance < 0 {
		return 0, ErrInvalid
	}
	used, err = used.Add(f.Balance)
	if err != nil {
		return 0, err
	}
	var future credits.Micro
	for due, n := f.NextReset, 0; due != nil && due.Before(f.ExpiresAt); n++ {
		if n >= 530000 { // One year at the minimum supported 60-second cycle.
			return 0, ErrInvalid
		}
		if f.LegacyPeriodic {
			used = max(used-f.Renewable, 0)
		}
		grant := max(f.Total-used, 0)
		if f.Total == 0 {
			grant = f.Period
		}
		if f.Period > 0 {
			grant = min(grant, f.Period)
		}
		future, err = future.Add(grant)
		if err != nil {
			return 0, err
		}
		used, err = used.Add(grant)
		if err != nil {
			return 0, err
		}
		next := nextReset(due.In(now.Location()), f.ResetPeriod, f.ResetSeconds, f.ExpiresAt)
		if next != nil && !next.After(*due) {
			return 0, ErrInvalid
		}
		due = next
	}
	return future, nil
}

const walletReviewColumns = `id,subscription_id,fact_hash,revision,segments,note,enabled,reviewed,reviewer_id,reviewed_at`

func scanWalletReview(row scanner) (WalletConversionReview, error) {
	var review WalletConversionReview
	err := row.Scan(&review.ID, &review.SubscriptionID, &review.FactHash, &review.Revision, &review.Segments, &review.Note, &review.Enabled, &review.Reviewed, &review.ReviewerID, &review.ReviewedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return review, err
}

func (s *Service) WalletConversionReviewEvidence(ctx context.Context, id int64) (WalletConversionReviewEvidence, error) {
	var out WalletConversionReviewEvidence
	var user int64
	if id <= 0 {
		return out, ErrInvalid
	}
	if err := s.pool.QueryRow(ctx, `SELECT user_id FROM v3_commerce.subscriptions WHERE id=$1`, id).Scan(&user); err != nil {
		return out, err
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		f, err := loadWalletConversionFacts(ctx, tx, user, id, true)
		if err != nil {
			return err
		}
		if f.Policy != PolicyLegacy || f.State != "active" || f.ConvertedAt != nil || !f.ExpiresAt.After(s.cfg.Now()) {
			return ErrStateConflict
		}
		out = WalletConversionReviewEvidence{SubscriptionID: id, UserID: user, CurrentCredits: f.Balance, ExpiresAt: f.ExpiresAt, ResetUsed: f.ResetUsed, Sources: f.Sources}
		out.FactHash, err = digestValue(f)
		if err != nil {
			return err
		}
		out.FutureCredits, err = walletFutureCredits(f, s.cfg.Now())
		if err != nil {
			return err
		}
		r, err := scanWalletReview(tx.QueryRow(ctx, `SELECT `+walletReviewColumns+` FROM v3_commerce.subscription_wallet_reviews WHERE subscription_id=$1`, id))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err == nil {
			out.Review = &r
		}
		return err
	})
	return out, err
}
