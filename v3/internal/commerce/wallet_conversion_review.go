package commerce

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func (s *Service) SaveWalletConversionReview(ctx context.Context, actor int64, in WalletConversionReview) (WalletConversionReview, error) {
	raw, err := hex.DecodeString(in.FactHash)
	if err != nil || len(raw) != 32 || actor <= 0 || in.SubscriptionID <= 0 || len(in.Segments) == 0 || len(in.Segments) > 100 || strings.TrimSpace(in.Note) == "" || len(in.Note) > 2000 || (in.Enabled && !in.Reviewed) {
		return in, ErrInvalid
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockSubscriptionUserTx(ctx, tx, in.SubscriptionID); err != nil {
			return err
		}
		if in.ID > 0 && !in.Enabled {
			tag, err := tx.Exec(ctx, `UPDATE v3_commerce.subscription_wallet_reviews SET enabled=false,reviewed=$4,note=$5,reviewer_id=$6,reviewed_at=$7,revision=revision+1 WHERE id=$1 AND subscription_id=$2 AND revision=$3`, in.ID, in.SubscriptionID, in.Revision, in.Reviewed, in.Note, actor, s.cfg.Now())
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return ErrStateConflict
			}
			in, err = scanWalletReview(tx.QueryRow(ctx, `SELECT `+walletReviewColumns+` FROM v3_commerce.subscription_wallet_reviews WHERE id=$1`, in.ID))
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_wallet_review_versions(review_id,revision,review_snapshot,fact_snapshot)
 SELECT $1,$2,$3,fact_snapshot FROM v3_commerce.subscription_wallet_review_versions WHERE review_id=$1 ORDER BY revision DESC LIMIT 1`, in.ID, in.Revision, in)
			return err
		}
		var user int64
		if err := tx.QueryRow(ctx, `SELECT user_id FROM v3_commerce.subscriptions WHERE id=$1`, in.SubscriptionID).Scan(&user); err != nil {
			return err
		}
		f, err := loadWalletConversionFacts(ctx, tx, user, in.SubscriptionID, true)
		if err != nil {
			return err
		}
		if f.Policy != PolicyLegacy || f.State != "active" || f.ConvertedAt != nil || !f.ExpiresAt.After(s.cfg.Now()) || f.StartsAt.After(s.cfg.Now()) {
			return ErrStateConflict
		}
		if err = s.checkPackagePending(ctx, tx, f.ID); err != nil {
			return err
		}
		hash, err := digestValue(f)
		if err != nil {
			return err
		}
		if hash != in.FactHash {
			return ErrStateConflict
		}
		future, err := walletFutureCredits(f, s.cfg.Now())
		if err != nil {
			return err
		}
		if err = valueWalletConversionSegments(in.Segments, f, future); err != nil {
			return err
		}
		in.ReviewerID, in.ReviewedAt = actor, s.cfg.Now()
		if in.ID == 0 {
			err = tx.QueryRow(ctx, `INSERT INTO v3_commerce.subscription_wallet_reviews(subscription_id,fact_hash,segments,note,enabled,reviewed,reviewer_id,reviewed_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id,revision`, in.SubscriptionID, in.FactHash, in.Segments, in.Note, in.Enabled, in.Reviewed, actor, in.ReviewedAt).Scan(&in.ID, &in.Revision)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_wallet_review_versions(review_id,revision,review_snapshot,fact_snapshot) VALUES($1,$2,$3,$4)`, in.ID, in.Revision, in, f)
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE v3_commerce.subscription_wallet_reviews SET fact_hash=$3,segments=$4,note=$5,enabled=$6,reviewed=$7,reviewer_id=$8,reviewed_at=$9,revision=revision+1
 WHERE id=$1 AND subscription_id=$2 AND revision=$10`, in.ID, in.SubscriptionID, in.FactHash, in.Segments, in.Note, in.Enabled, in.Reviewed, actor, in.ReviewedAt, in.Revision)
		if err == nil && tag.RowsAffected() != 1 {
			return ErrStateConflict
		}
		in.Revision++
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_wallet_review_versions(review_id,revision,review_snapshot,fact_snapshot) VALUES($1,$2,$3,$4)`, in.ID, in.Revision, in, f)
		return err
	})
	return in, err
}

func valueWalletConversionSegments(segments []WalletConversionSegment, f walletConversionFacts, expectedFuture credits.Micro) error {
	var current, future, target credits.Micro
	paidByOrder := map[int64]credits.Micro{}
	sourceByOrder := map[int64]credits.Micro{}
	names := map[string]bool{}
	for i := range segments {
		p := &segments[i]
		if strings.TrimSpace(p.Name) == "" || len(p.Name) > 200 || names[p.Name] || p.OriginalOrderID < 0 || p.SourceTotal <= 0 || p.CurrentCredits < 0 || p.FutureCredits < 0 || p.WalletCredits <= 0 || p.PaidWalletCredits < 0 || p.PaidWalletCredits > p.WalletCredits {
			return ErrInvalid
		}
		names[p.Name] = true
		remaining, err := p.CurrentCredits.Add(p.FutureCredits)
		if err != nil || remaining <= 0 || remaining > p.SourceTotal {
			return ErrInvalid
		}
		if current, err = current.Add(p.CurrentCredits); err != nil {
			return err
		}
		if future, err = future.Add(p.FutureCredits); err != nil {
			return err
		}
		p.TargetCredits, err = floorCreditRatio(remaining, p.WalletCredits, p.SourceTotal)
		if err != nil {
			return err
		}
		p.PaidCredits, err = floorCreditRatio(remaining, p.PaidWalletCredits, p.SourceTotal)
		if err != nil {
			return err
		}
		p.RevenueMultiplierPPM = 0
		if target, err = target.Add(p.TargetCredits); err != nil {
			return err
		}
		if p.PaidCredits == 0 {
			continue
		}
		var source *WalletConversionSource
		for j := range f.Sources {
			if f.Sources[j].OrderID == p.OriginalOrderID {
				source = &f.Sources[j]
			}
		}
		if source == nil || source.State != "paid" || source.PendingRefund || source.RevenueCredits == nil || *source.RevenueCredits <= 0 || source.Credits <= 0 || p.SourceTotal != source.Credits {
			return ErrStateConflict
		}
		sourceByOrder[source.OrderID], err = sourceByOrder[source.OrderID].Add(remaining)
		if err != nil || sourceByOrder[source.OrderID] > source.Credits {
			return ErrInvalid
		}
		paidByOrder[source.OrderID], err = paidByOrder[source.OrderID].Add(p.PaidCredits)
		if err != nil {
			return err
		}
		if paidByOrder[source.OrderID] > *source.RevenueCredits-source.PreviouslyPaid {
			return ErrInvalid
		}
		// Attribute the remaining source revenue proportionally; never claim
		// a full order's revenue anew for every supplemental segment.
		revenue, err := floorCreditRatio(*source.RevenueCredits, remaining, p.SourceTotal)
		if err != nil || revenue < p.PaidCredits {
			return ErrInvalid
		}
		ppm, err := floorCreditRatio(revenue, 1000000, p.PaidCredits)
		if err != nil {
			return err
		}
		p.RevenueMultiplierPPM = int64(ppm)
	}
	if current != f.Balance || future != expectedFuture || target <= 0 {
		return ErrInvalid
	}
	return nil
}

func (s *Service) previewReviewedWalletConversionTx(ctx context.Context, tx pgx.Tx, f walletConversionFacts, out *WalletConversionQuote, persist bool) (bool, error) {
	r, err := scanWalletReview(tx.QueryRow(ctx, `SELECT `+walletReviewColumns+` FROM v3_commerce.subscription_wallet_reviews WHERE subscription_id=$1 AND enabled AND reviewed FOR SHARE`, f.ID))
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	hash, err := digestValue(f)
	if err != nil {
		return true, err
	}
	future, err := walletFutureCredits(f, s.cfg.Now())
	if errors.Is(err, ErrStateConflict) || hash != r.FactHash {
		out.ReviewReason = "消费、刷新或来源已变化，需重新核定分段权益"
		return true, nil
	}
	if err != nil {
		return true, err
	}
	if err = valueWalletConversionSegments(r.Segments, f, future); err != nil {
		return true, err
	}
	out.ReviewID, out.RuleRevision, out.FutureCredits = r.ID, r.Revision, future
	out.Segments = r.Segments
	out.SourceTotal = 0
	for _, p := range r.Segments {
		out.SourceTotal, err = out.SourceTotal.Add(p.SourceTotal)
		if err == nil {
			out.TargetCredits, err = out.TargetCredits.Add(p.TargetCredits)
		}
		if err == nil {
			out.PaidCredits, err = out.PaidCredits.Add(p.PaidCredits)
		}
		if err != nil {
			return true, err
		}
	}
	out.RewardCredits = out.TargetCredits - out.PaidCredits
	out.State, out.ExpiresAt = "quoted", minTime(s.cfg.Now().Add(5*time.Minute), f.ExpiresAt)
	if f.NextReset != nil {
		out.ExpiresAt = minTime(out.ExpiresAt, *f.NextReset)
	}
	if !persist {
		return true, nil
	}
	out.QuoteID, err = tradeNumber()
	if err != nil {
		return true, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_wallet_quotes(quote_id,user_id,subscription_id,review_id,rule_revision,source_account_id,original_order_id,original_total,source_credits,target_credits,paid_credits,fact_hash,subscription_expires_at,expires_at,segments,future_credits)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, out.QuoteID, f.User, f.ID, r.ID, r.Revision, f.Account, f.Order, out.SourceTotal, f.Balance, out.TargetCredits, out.PaidCredits, hash, f.ExpiresAt, out.ExpiresAt, out.Segments, future)
	return true, err
}
