package legacy

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
)

// V2 edits the public key cap independently of its ledger mirror. A later
// nonzero token adjustment reconciles the mirror to that cap, not vice versa.
// Preserve both facts; only a mirror proven against immutable money may differ.
func keyBudgetMirrorSQL(ctx context.Context, tx pgx.Tx, sources map[string]string) (string, error) {
	accounts := sources["accounts"]
	if accounts == "" {
		return "", nil
	}
	scope := ` WHERE a.owner_type='token' OR a.account_type='token'`
	ledger := projectionSource(sources, "billing_", "ledger_entries")
	reservations := projectionSource(sources, "billing_", "reservations")
	settlements := projectionSource(sources, "billing_", "settlements")
	for _, required := range []struct {
		table, columns string
	}{
		{accounts, "account_id owner_type owner_id account_type quota_unit"},
		{sources["balance_snapshots"], "account_id available_balance reserved_balance granted_total consumed_total refunded_total"},
		{sources["tokens"], "id user_id key remain_quota unlimited_quota"},
		{sources["users"], "id"},
		{ledger, "entry_id account_id entry_type direction amount reference_type reference_id idempotency_key"},
		{reservations, "reservation_id account_id reserved_amount status"},
		{settlements, "reservation_id actual_amount delta_amount status"},
	} {
		complete, err := projectionColumns(ctx, tx, required.table, strings.Fields(required.columns)...)
		if err != nil {
			return "", err
		}
		if !complete {
			// Partial old schemas cannot prove the extra edited-cap contract.
			// With no token accounts this is empty, preserving old installations.
			return `SELECT a.owner_id,false AS proven,false AS edited FROM ` + accounts + ` a` + scope, nil
		}
	}
	maximum := fmt.Sprint(math.MaxInt64 / microPerV2Unit)
	return `WITH key_budget_accounts AS MATERIALIZED (
	 SELECT a.account_id,a.owner_type,a.owner_id,a.account_type,a.quota_unit,t.id AS token_id,u.id AS user_id,
	 t.key,t.remain_quota,t.unlimited_quota,bs.account_id AS snapshot_id,bs.available_balance,bs.reserved_balance,
	 bs.granted_total,bs.consumed_total,bs.refunded_total FROM ` + accounts + ` a
	 LEFT JOIN ` + sources["tokens"] + ` t ON t.id=a.owner_id
	 LEFT JOIN ` + sources["users"] + ` u ON u.id=t.user_id
	 LEFT JOIN ` + sources["balance_snapshots"] + ` bs ON bs.account_id=a.account_id` + scope + `),
	 key_budget_entry_totals AS MATERIALIZED (
	 SELECT l.account_id,COALESCE(sum(CASE WHEN l.entry_type IN ('grant_credit','reserve_release','settle_credit') THEN l.amount::numeric
	 WHEN l.entry_type IN ('reserve_hold','settle_debit') THEN -l.amount::numeric ELSE 0 END),0) AS available,
	 COALESCE(sum(CASE WHEN l.entry_type='grant_credit' THEN l.amount::numeric ELSE 0 END),0) AS granted,
	 COALESCE(sum(CASE WHEN l.entry_type='settle_debit' AND COALESCE(l.reference_type,'')<>'settlement' THEN l.amount::numeric ELSE 0 END),0) AS consumed,
	 COALESCE(sum(CASE WHEN l.entry_type='settle_credit' AND COALESCE(l.reference_type,'')<>'settlement' THEN l.amount::numeric ELSE 0 END),0) AS refunded,
	 COALESCE(bool_or(COALESCE(l.entry_id,'')='' OR COALESCE(l.idempotency_key,'')='' OR l.amount IS NULL OR l.amount<0 OR l.amount>` + maximum + `
	 OR (CASE WHEN l.entry_type IN ('grant_credit','reserve_release','settle_credit') THEN l.direction='credit'
	 WHEN l.entry_type IN ('reserve_hold','settle_debit') THEN l.direction='debit'
	 WHEN l.entry_type='adjustment' THEN l.direction='credit' AND l.amount=0 ELSE false END) IS NOT TRUE
	 OR (l.reference_type='token' AND l.reference_id IS DISTINCT FROM a.owner_id::text)),false) AS invalid
	 FROM ` + ledger + ` l JOIN key_budget_accounts a ON a.account_id=l.account_id GROUP BY l.account_id),
	 key_budget_settlement_totals AS MATERIALIZED (
	 SELECT r.account_id,COALESCE(sum(CASE WHEN s.status='completed' THEN s.actual_amount::numeric ELSE 0 END),0) AS consumed,
	 COALESCE(sum(CASE WHEN s.status='completed' AND s.delta_amount<0 THEN -s.delta_amount::numeric ELSE 0 END),0) AS refunded,
	 COALESCE(bool_or(s.status IS NULL OR s.status NOT IN ('completed','rejected')
	 OR (s.status='completed' AND (r.status IS DISTINCT FROM 'settled' OR r.reserved_amount IS NULL OR r.reserved_amount<0
	 OR s.actual_amount IS NULL OR s.actual_amount<0 OR s.delta_amount IS NULL
	 OR s.actual_amount::numeric IS DISTINCT FROM r.reserved_amount::numeric+s.delta_amount::numeric))),false) AS invalid
	 FROM ` + reservations + ` r JOIN ` + settlements + ` s ON s.reservation_id=r.reservation_id
	 JOIN key_budget_accounts a ON a.account_id=r.account_id GROUP BY r.account_id),
	 key_budget_reservation_totals AS MATERIALIZED (
	 SELECT r.account_id,COALESCE(sum(CASE WHEN r.status='open' THEN r.reserved_amount::numeric ELSE 0 END),0) AS reserved,
	 COALESCE(bool_or(r.status IS NULL OR r.status NOT IN ('settled','released','expired') OR r.reserved_amount IS NULL OR r.reserved_amount<0),false) AS invalid
	 FROM ` + reservations + ` r JOIN key_budget_accounts a ON a.account_id=r.account_id GROUP BY r.account_id)
	 SELECT a.owner_id,
	 COALESCE(a.owner_type='token' AND a.account_type='token' AND a.quota_unit='quota'
	 AND a.owner_id>0 AND a.token_id IS NOT NULL AND a.user_id>0 AND COALESCE(btrim(a.key),'')<>''
	 AND (a.unlimited_quota IS TRUE OR (a.remain_quota>=0 AND a.remain_quota<=` + maximum + `))
	 AND a.snapshot_id IS NOT NULL AND a.available_balance>=0 AND a.reserved_balance=0
	 AND a.granted_total>=0 AND a.consumed_total>=0 AND a.refunded_total>=0
	 AND a.available_balance::numeric=COALESCE(e.available,0) AND a.granted_total::numeric=COALESCE(e.granted,0)
	 AND a.consumed_total::numeric=COALESCE(e.consumed,0)+COALESCE(s.consumed,0)
	 AND a.refunded_total::numeric=COALESCE(e.refunded,0)+COALESCE(s.refunded,0)
	 AND COALESCE(r.reserved,0)=0 AND NOT COALESCE(e.invalid,false) AND NOT COALESCE(s.invalid,false)
	 AND NOT COALESCE(r.invalid,false),false) AS proven,
	 COALESCE(a.unlimited_quota IS NOT TRUE AND a.remain_quota IS DISTINCT FROM a.available_balance,false) AS edited
	 FROM key_budget_accounts a LEFT JOIN key_budget_entry_totals e ON e.account_id=a.account_id
	 LEFT JOIN key_budget_settlement_totals s ON s.account_id=a.account_id
	 LEFT JOIN key_budget_reservation_totals r ON r.account_id=a.account_id`, nil
}

func validateKeyBudgetMirrors(ctx context.Context, tx pgx.Tx, sources map[string]string, report *Report) error {
	query, err := keyBudgetMirrorSQL(ctx, tx, sources)
	if err != nil || query == "" {
		return err
	}
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var proven, edited bool
		if err = rows.Scan(&id, &proven, &edited); err != nil {
			return err
		}
		report.Counts["key_budget_mirrors_checked"]++
		if !proven {
			report.Issues = append(report.Issues, Issue{"account", id, "key_budget_mirror_unproven", "key mirror must retain valid ownership and exactly match its immutable ledger, settlement totals and zero reservations"})
		} else if edited {
			report.Counts["key_budget_caps_differ_from_historical_mirror"]++
		}
	}
	return rows.Err()
}
