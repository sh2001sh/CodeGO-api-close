package marketplace

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type BatchStatistics struct {
	DrawCount                  int64         `json:"draw_count"`
	BaseCredits                credits.Micro `json:"base_credits_micro"`
	RewardCredits              credits.Micro `json:"reward_credits_micro"`
	SubscriptionAwardedCount   int64         `json:"subscription_awarded_count"`
	SubscriptionActivatedCount int64         `json:"subscription_activated_count"`
	APIUsed                    credits.Micro `json:"api_used_micro"`
	Reserved                   credits.Micro `json:"reserved_micro"`
	Spent                      credits.Micro `json:"spent_micro"`
	Remaining                  credits.Micro `json:"remaining_micro"`
}

func (s *Service) BatchStats(ctx context.Context, id int64) (BatchStatistics, error) {
	var out BatchStatistics
	if id <= 0 {
		return out, ErrInvalidInput
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		b, err := scanBatch(tx.QueryRow(ctx, `SELECT `+batchColumns+` FROM v3_marketplace.blind_box_batches WHERE id=$1`, id))
		if err != nil {
			return err
		}
		if b.State != "draft" {
			out.Reserved, out.Spent, out.Remaining = b.RequiredBudget, b.SpentBudget, b.RemainingBudget
		}
		out.DrawCount = b.TotalCount - b.RemainingCount
		out.BaseCredits = b.BaseCredits * credits.Micro(out.DrawCount)
		if err = tx.QueryRow(ctx, `SELECT (coalesce(sum((r.reward->>'amount_micro')::numeric) FILTER(WHERE r.reward->>'kind'='credits'),0)+coalesce(sum(r.guarantee_credits_micro::numeric),0))::bigint,count(*) FILTER(WHERE r.reward->>'kind'='subscription'),count(*) FILTER(WHERE r.reward->>'kind'='subscription' AND p.status='used') FROM v3_marketplace.blind_box_open_records r LEFT JOIN v3_marketplace.blind_box_props p ON p.open_record_id=r.id WHERE r.batch_id=$1`, id).Scan(&out.RewardCredits, &out.SubscriptionAwardedCount, &out.SubscriptionActivatedCount); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `WITH wallet AS (
 SELECT coalesce(sum(fa.amount::numeric*LEAST(GREATEST(-coalesce((SELECT sum(e.amount::numeric) FROM v3_billing.ledger_entries e WHERE e.account_id=fa.account_id AND e.request_id=fa.request_id AND e.kind IN('usage','refund')),0),0),tot.amount)/nullif(tot.amount,0)),0) used
 FROM v3_billing.funding_allocations fa JOIN v3_billing.funding_lots l ON l.lot_id=fa.lot_id
 CROSS JOIN LATERAL (SELECT sum(a.amount::numeric) amount FROM v3_billing.funding_allocations a WHERE a.account_id=fa.account_id AND a.request_id=fa.request_id) tot
 WHERE l.source IN('blind_box_batch_base','blind_box_batch_reward') AND l.metadata->>'batch_id'=$1::bigint::text
 ), subscriptions AS (
 SELECT GREATEST(-coalesce(sum(e.amount::numeric),0),0) used
 FROM v3_commerce.subscriptions s
 JOIN v3_marketplace.blind_box_props p ON s.reward_operation='blind-box:prop:'||p.id
 JOIN v3_marketplace.blind_box_open_records r ON r.id=p.open_record_id
 JOIN v3_billing.ledger_entries e ON e.account_id=s.account_id AND e.kind IN('usage','refund') WHERE r.batch_id=$1
 ) SELECT floor(wallet.used+subscriptions.used)::bigint FROM wallet,subscriptions`, id).Scan(&out.APIUsed)
	})
	return out, err
}
