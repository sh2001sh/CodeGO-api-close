package commerce

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// InitializeImportedSubscriptions fills runtime metadata for subscriptions
// imported after schema migrations. It never changes an opening ledger balance;
// the importer must already cap that opening to both lifetime and period funds.
// Run before enabling gateways, then replay safely on interrupted imports.
func (s *Service) InitializeImportedSubscriptions(ctx context.Context) (int, error) {
	count := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		count, err = s.InitializeImportedSubscriptionsTx(ctx, tx)
		return err
	})
	return count, err
}

// InitializeImportedSubscriptionsTx shares the importer's atomic target transaction.
func (s *Service) InitializeImportedSubscriptionsTx(ctx context.Context, tx pgx.Tx) (int, error) {
	if err := backfillPlanBonusMicroColumns(ctx, tx); err != nil {
		return 0, err
	}
	pending, err := loadUnbucketedImportedSubscriptions(ctx, tx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, row := range pending {
		if row.next == nil && row.rule != "never" {
			row.next = nextReset(row.start.In(s.cfg.Now().Location()), row.rule, row.seconds, row.end)
		}
		if _, err = tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET reset_period=$2,reset_custom_seconds=$3,
		 renewable_credits=$4,next_reset_at=$5,last_reset_at=COALESCE(last_reset_at,starts_at) WHERE id=$1`, row.id, row.rule, row.seconds, int64(row.renewable), row.next); err != nil {
			return count, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_buckets(account_id,subscription_id,starts_at) VALUES($1,$2,$3)`, row.account, row.id, row.start); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// backfillPlanBonusMicroColumns copies legacy decimal bonus/fuel-price plan
// columns (surfaced only via to_jsonb, since the typed columns were added
// later) into their micro-integer replacements where those are still unset.
func backfillPlanBonusMicroColumns(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `UPDATE v3_commerce.plans p SET
	 group_buy_bonus2_micro=CASE WHEN group_buy_bonus2_micro=0 THEN COALESCE((NULLIF(to_jsonb(p)->>'group_buy_bonus2','')::numeric*1000000)::bigint,0) ELSE group_buy_bonus2_micro END,
	 group_buy_bonus3_micro=CASE WHEN group_buy_bonus3_micro=0 THEN COALESCE((NULLIF(to_jsonb(p)->>'group_buy_bonus3','')::numeric*1000000)::bigint,0) ELSE group_buy_bonus3_micro END,
	 group_buy_bonus5_micro=CASE WHEN group_buy_bonus5_micro=0 THEN COALESCE((NULLIF(to_jsonb(p)->>'group_buy_bonus5','')::numeric*1000000)::bigint,0) ELSE group_buy_bonus5_micro END,
	 fuel_unit_price_micro=CASE WHEN fuel_unit_price_micro=0 THEN COALESCE((NULLIF(to_jsonb(p)->>'fuel_unit_price','')::numeric*1000000)::bigint,0) ELSE fuel_unit_price_micro END
	 WHERE (group_buy_bonus2_micro=0 AND COALESCE(NULLIF(to_jsonb(p)->>'group_buy_bonus2','')::numeric,0)<>0)
	 OR (group_buy_bonus3_micro=0 AND COALESCE(NULLIF(to_jsonb(p)->>'group_buy_bonus3','')::numeric,0)<>0)
	 OR (group_buy_bonus5_micro=0 AND COALESCE(NULLIF(to_jsonb(p)->>'group_buy_bonus5','')::numeric,0)<>0)
	 OR (fuel_unit_price_micro=0 AND COALESCE(NULLIF(to_jsonb(p)->>'fuel_unit_price','')::numeric,0)<>0)`)
	return err
}

type importedSubscriptionRow struct {
	id, account int64
	start, end  time.Time
	next        *time.Time
	rule        string
	seconds     int64
	renewable   credits.Micro
}

// loadUnbucketedImportedSubscriptions locks and returns subscriptions that
// have an account but no bucket row yet, meaning they were imported directly
// rather than created through grantSubscription.
func loadUnbucketedImportedSubscriptions(ctx context.Context, tx pgx.Tx) ([]importedSubscriptionRow, error) {
	rows, err := tx.Query(ctx, `SELECT s.id,s.account_id,s.starts_at,s.expires_at,s.next_reset_at,
	 p.reset_period,p.reset_custom_seconds,LEAST(s.total_credits,p.credits)
	 FROM v3_commerce.subscriptions s JOIN v3_commerce.plans p ON p.id=s.plan_id
	 WHERE s.account_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM v3_commerce.subscription_buckets b WHERE b.account_id=s.account_id)
	 ORDER BY s.id FOR UPDATE OF s`)
	if err != nil {
		return nil, err
	}
	var pending []importedSubscriptionRow
	for rows.Next() {
		var row importedSubscriptionRow
		if err = rows.Scan(&row.id, &row.account, &row.start, &row.end, &row.next, &row.rule, &row.seconds, &row.renewable); err != nil {
			rows.Close()
			return nil, err
		}
		pending = append(pending, row)
	}
	rows.Close()
	return pending, rows.Err()
}
