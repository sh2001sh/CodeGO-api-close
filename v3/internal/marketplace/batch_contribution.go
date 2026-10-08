package marketplace

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Qualifying facts require a real, unrefunded paid order. Unknown costs,
// transferred topups and mixed/reward funding never qualify. We reserve the
// maximum referral promise rather than counting only awards paid so far.
// Procurement and seller payouts are both deducted conservatively; configured
// ancillary costs cover payment and other attributable expenses.
const batchContributionSQL = `WITH candidates AS (
 SELECT f.*, CASE WHEN f.policy_version='wallet' THEN a.owner_id ELSE s.user_id END user_id,
 CASE WHEN f.policy_version='standard_v2' THEN f.order_id ELSE (
  SELECT o.id FROM v3_billing.funding_allocations fa
  JOIN v3_billing.funding_lots l ON l.lot_id=fa.lot_id
  JOIN v3_commerce.orders o ON l.reference_id='order:paid:'||o.trade_no
  WHERE fa.request_id=f.request_id AND fa.account_id=f.account_id AND fa.source='topup' AND l.source='topup'
  AND NOT (l.metadata ? 'transfer_operation_id') AND o.kind='topup' AND o.user_id=a.owner_id
  GROUP BY o.id HAVING sum(fa.amount::numeric)=f.amount
 ) END paid_order
 FROM v3_billing.funding_source_usage f JOIN v3_billing.accounts a ON a.id=f.account_id
 LEFT JOIN v3_commerce.subscriptions s ON s.id=f.subscription_id
 WHERE f.amount>0 AND f.policy_version IN('wallet','standard_v2')
 AND f.procurement_cost_amount IS NOT NULL AND f.revenue_multiplier_ppm IS NOT NULL
 AND f.settled_at >= $1 AND f.settled_at <= $2
 AND NOT EXISTS(SELECT 1 FROM v3_marketplace.blind_box_contribution_claims c WHERE c.request_id=f.request_id AND c.account_id=f.account_id)
), paid AS (
 SELECT c.*,o.credits order_credits,o.id purchase_order_id,
 floor(c.amount::numeric*o.recognized_revenue_credits/nullif(o.credits,0)) revenue,
 COALESCE((SELECT max(q.max_reward_credits) FROM v3_commerce.referral_consumption_qualifications q WHERE q.order_id=o.id AND q.state<>'released'),0) referral_reserve
 FROM candidates c JOIN v3_commerce.orders o ON o.id=c.paid_order
 WHERE o.user_id=c.user_id AND o.state='paid' AND o.amount_minor>0 AND o.credits>0
 AND o.recognized_revenue_credits>0
 AND EXISTS(SELECT 1 FROM v3_commerce.payment_events e WHERE e.trade_no=o.trade_no AND e.provider=o.provider)
 AND (c.policy_version<>'standard_v2' OR c.revenue_multiplier_ppm>0)
 AND NOT EXISTS(SELECT 1 FROM v3_commerce.provider_refund_progress r WHERE r.order_id=o.id AND r.amount_minor>0)
 AND NOT EXISTS(SELECT 1 FROM v3_commerce.user_refunds r WHERE r.order_id=o.id AND r.status IN('processing','success'))
 AND NOT EXISTS(SELECT 1 FROM v3_billing.ledger_entries e WHERE e.account_id=c.account_id AND e.request_id=c.request_id AND e.kind='refund')
 AND EXISTS(SELECT 1 FROM v3_billing.ledger_entries e WHERE e.account_id=c.account_id AND e.request_id=c.request_id AND e.kind='usage' AND e.amount=-c.amount)
), contributions AS (
 SELECT p.request_id,p.account_id,p.user_id,p.purchase_order_id order_id,p.settled_at,
 GREATEST(revenue-procurement_cost_amount
 -COALESCE((SELECT ceil((m.net_micro+m.fee_micro)::numeric*p.amount/nullif(m.consumer_micro,0)) FROM v3_channelmarket.settlements m WHERE m.request_id=p.request_id),0)
 -ceil(revenue*$3::numeric/1000000)
 -ceil(referral_reserve::numeric*p.amount/nullif(order_credits,0)),0)::bigint contribution
 FROM paid p
) SELECT request_id,account_id,user_id,order_id,contribution FROM contributions
WHERE contribution>0 AND ($4::bigint=0 OR user_id=$4) ORDER BY settled_at,account_id,request_id LIMIT $5`

type contributionFact struct {
	request                      string
	account, user, order, amount int64
}

func contributionFacts(ctx context.Context, tx pgx.Tx, b Batch, cutoff time.Time, user int64, limit int) ([]contributionFact, error) {
	rows, err := tx.Query(ctx, batchContributionSQL, b.PublishedAt, cutoff, b.AncillaryCostPPM, user, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var facts []contributionFact
	for rows.Next() {
		var f contributionFact
		if err = rows.Scan(&f.request, &f.account, &f.user, &f.order, &f.amount); err != nil {
			return nil, err
		}
		facts = append(facts, f)
	}
	return facts, rows.Err()
}

// AccrueBatchEntitlements runs in low-frequency worker maintenance, not on the
// gateway's high-frequency settlement path. A seven-day maturation window and
// the pre-funded reserve absorb later reversals without revoking issued boxes.
func (s *Service) AccrueBatchEntitlements(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	batches, err := s.ListBatches(ctx, true)
	if err != nil {
		return 0, err
	}
	awarded := 0
	for _, batch := range batches {
		if batch.State != "published" || batch.Purpose != "consumption" || batch.EntitledCount >= batch.RemainingCount {
			continue
		}
		var facts []contributionFact
		err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			var e error
			facts, e = contributionFacts(ctx, tx, batch, s.cfg.Now().Add(-7*24*time.Hour), 0, limit)
			return e
		})
		if err != nil {
			return awarded, err
		}
		seen := map[int64]bool{}
		for _, f := range facts {
			if seen[f.user] {
				continue
			}
			seen[f.user] = true
			n, e := s.accrueUserBatch(ctx, batch.ID, f.user, limit)
			if e != nil {
				return awarded, fmt.Errorf("batch %d contribution: %w", batch.ID, e)
			}
			awarded += n
		}
	}
	return awarded, nil
}

func (s *Service) accrueUserBatch(ctx context.Context, batch, user int64, limit int) (int, error) {
	awarded := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		b, err := scanBatch(tx.QueryRow(ctx, `SELECT `+batchColumns+` FROM v3_marketplace.blind_box_batches WHERE id=$1`, batch))
		if err != nil {
			return err
		}
		if b.State != "published" || b.Purpose != "consumption" || b.RemainingCount == b.EntitledCount {
			return nil
		}
		facts, err := contributionFacts(ctx, tx, b, s.cfg.Now().Add(-7*24*time.Hour), user, limit)
		if err != nil || len(facts) == 0 {
			return err
		}
		// Commerce callbacks lock orders before users. Follow that order rather
		// than holding a user/batch lock while waiting for a concurrent refund.
		orders := make([]int64, 0, len(facts))
		lockedOrders := make(map[int64]bool, len(facts))
		for _, f := range facts {
			orders = append(orders, f.order)
			lockedOrders[f.order] = true
		}
		if _, err = tx.Exec(ctx, `SELECT id FROM v3_commerce.orders WHERE id=ANY($1) ORDER BY id FOR SHARE`, orders); err != nil {
			return err
		}
		if err = lockUser(ctx, tx, user); err != nil {
			return err
		}
		b, err = scanBatch(tx.QueryRow(ctx, `SELECT `+batchColumns+` FROM v3_marketplace.blind_box_batches WHERE id=$1 FOR UPDATE`, batch))
		if err != nil {
			return err
		}
		if b.State != "published" || b.Purpose != "consumption" || b.RemainingCount == b.EntitledCount {
			return nil
		}
		facts, err = contributionFacts(ctx, tx, b, s.cfg.Now().Add(-7*24*time.Hour), user, limit)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_batch_entitlements(batch_id,user_id) VALUES($1,$2) ON CONFLICT(batch_id,user_id) DO NOTHING`, batch, user); err != nil {
			return err
		}
		for _, f := range facts {
			if !lockedOrders[f.order] {
				continue
			}
			tag, err := tx.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_contribution_claims(request_id,account_id,batch_id,user_id,contribution_micro) VALUES($1,$2,$3,$4,$5) ON CONFLICT(request_id,account_id) DO NOTHING`, f.request, f.account, batch, user, f.amount)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 1 {
				if _, err = tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_batch_entitlements SET contribution_micro=contribution_micro+$3 WHERE batch_id=$1 AND user_id=$2`, batch, user, f.amount); err != nil {
					return err
				}
			}
		}
		var maxReward int64
		for _, r := range b.Rewards {
			maxReward = max(maxReward, int64(r.Amount))
		}
		var delta int64
		// Numeric arithmetic prevents intermediate overflow at bigint limits.
		if err = tx.QueryRow(ctx, `SELECT LEAST(GREATEST(floor(contribution_micro::numeric*$3/1000000/$4)-awarded_count,0),$5)::bigint FROM v3_marketplace.blind_box_batch_entitlements WHERE batch_id=$1 AND user_id=$2 FOR UPDATE`, batch, user, b.ContributionSharePPM, maxReward, b.RemainingCount-b.EntitledCount).Scan(&delta); err != nil {
			return err
		}
		if delta > 0 {
			if _, err = tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_batch_entitlements SET available_count=available_count+$3,awarded_count=awarded_count+$3,committed_budget_micro=committed_budget_micro+$4 WHERE batch_id=$1 AND user_id=$2`, batch, user, delta, delta*maxReward); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_batches SET entitled_count=entitled_count+$2,updated_at=$3 WHERE id=$1`, batch, delta, s.cfg.Now()); err != nil {
				return err
			}
			awarded = int(delta)
		}
		return nil
	})
	return awarded, err
}
