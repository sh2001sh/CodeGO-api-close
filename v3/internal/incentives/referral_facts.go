package incentives

import (
	"context"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// Real usage only: refunds reduce revenue. Procurement expense remains until
// accounting supplies a genuine recovered-cost fact; customer refunds are not
// evidence that the upstream refunded its cost.
const referralFactQuery = `
WITH raw_subscription AS (
 SELECT u.request_id,u.account_id,u.amount,u.revenue_multiplier_ppm,u.procurement_cost_amount,u.settled_at,
 GREATEST(-COALESCE((SELECT sum(e.amount::numeric) FROM v3_billing.ledger_entries e WHERE e.request_id=u.request_id AND e.account_id=u.account_id AND e.kind IN('usage','refund')),0),0) net
 FROM v3_billing.funding_source_usage u WHERE u.order_id=$1 AND u.policy_version='standard_v2'
), subscription_facts AS (
 SELECT floor(LEAST(net,amount)*COALESCE(revenue_multiplier_ppm,0)/1000000) revenue,
 COALESCE(procurement_cost_amount,0)::numeric cost, revenue_multiplier_ppm IS NOT NULL AND procurement_cost_amount IS NOT NULL known,settled_at
 FROM raw_subscription
), wallet_facts AS (
 SELECT floor(a.amount::numeric*o.recognized_revenue_credits/nullif(o.credits,0)*
 LEAST(GREATEST(-COALESCE((SELECT sum(e.amount::numeric) FROM v3_billing.ledger_entries e WHERE e.request_id=a.request_id AND e.account_id=a.account_id AND e.kind IN('usage','refund')),0),0),COALESCE(u.amount,a.amount))/nullif(COALESCE(u.amount,(SELECT sum(fa.amount::numeric) FROM v3_billing.funding_allocations fa WHERE fa.request_id=a.request_id AND fa.account_id=a.account_id)),0)) revenue,
 ceil(COALESCE(u.procurement_cost_amount,0)::numeric*a.amount/nullif(u.amount,0)) cost,
 o.recognized_revenue_credits IS NOT NULL AND u.procurement_cost_amount IS NOT NULL AND u.amount>0 known,
 COALESCE(u.settled_at,(SELECT min(e.created_at) FROM v3_billing.ledger_entries e WHERE e.request_id=a.request_id AND e.account_id=a.account_id AND e.kind='usage')) settled_at
 FROM v3_billing.funding_allocations a JOIN v3_billing.funding_lots l ON l.lot_id=a.lot_id
 JOIN v3_commerce.orders o ON o.id=$1 AND o.kind='topup' AND o.user_id=(SELECT owner_id FROM v3_billing.accounts WHERE id=a.account_id)
 LEFT JOIN v3_billing.funding_source_usage u ON u.request_id=a.request_id AND u.account_id=a.account_id AND u.policy_version='wallet'
 WHERE a.source='topup' AND l.source='topup' AND l.reference_id='order:paid:'||o.trade_no
 AND NOT (l.metadata ? 'transfer_operation_id')
 AND EXISTS(SELECT 1 FROM v3_billing.ledger_entries e WHERE e.request_id=a.request_id AND e.account_id=a.account_id AND e.kind='usage')
), facts AS (SELECT * FROM subscription_facts UNION ALL SELECT * FROM wallet_facts)
SELECT COALESCE(sum(revenue),0)::bigint,COALESCE(sum(cost),0)::bigint,COALESCE(bool_and(known),true)
FROM facts WHERE settled_at>=$2 AND settled_at<$3 AND settled_at<=$4`

func referralAncillaryCost(revenue, ppm int64) (int64, error) {
	if revenue < 0 || ppm < 0 || ppm > 1000000 {
		return 0, ErrInvalid
	}
	n := new(big.Int).Mul(big.NewInt(revenue), big.NewInt(ppm))
	n.Add(n, big.NewInt(999999))
	n.Quo(n, big.NewInt(1000000))
	if !n.IsInt64() {
		return 0, credits.ErrOverflow
	}
	return n.Int64(), nil
}
func referralFacts(ctx context.Context, tx pgx.Tx, q referralQualification, cutoff time.Time) (int64, int64, bool, error) {
	var revenue, cost int64
	var known bool
	err := tx.QueryRow(ctx, referralFactQuery, q.order, q.paidAt, q.until, cutoff).Scan(&revenue, &cost, &known)
	if err != nil {
		return 0, 0, false, err
	}
	var recognized *int64
	var paid, refunded int64
	err = tx.QueryRow(ctx, `SELECT recognized_revenue_credits,amount_minor,COALESCE((SELECT amount_minor FROM v3_commerce.provider_refund_progress WHERE order_id=o.id),0) FROM v3_commerce.orders o WHERE id=$1`, q.order).Scan(&recognized, &paid, &refunded)
	if err != nil {
		return 0, 0, false, err
	}
	if refunded > 0 {
		if recognized == nil || paid <= 0 {
			known = false
		} else {
			n := new(big.Int).Mul(big.NewInt(*recognized), big.NewInt(max(paid-refunded, 0)))
			n.Quo(n, big.NewInt(paid))
			if !n.IsInt64() {
				return 0, 0, false, credits.ErrOverflow
			}
			revenue = min(revenue, n.Int64())
		}
	}
	ancillary, err := referralAncillaryCost(revenue, q.ancillary)
	if err != nil {
		return 0, 0, false, err
	}
	total, err := credits.Micro(cost).Add(credits.Micro(ancillary))
	return revenue, int64(total), known, err
}
