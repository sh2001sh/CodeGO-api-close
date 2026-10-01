//go:build pgintegration

package legacy

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Reusable in the full independent-source/target fixture. Extend the older
// commerce fixture's abbreviated funding_lots table to the real v2 schema.
func seedFundingFixture(t *testing.T, source *pgxpool.Pool) {
	t.Helper()
	_, err := source.Exec(context.Background(), `
		ALTER TABLE migration_source.users ADD COLUMN IF NOT EXISTS created_at bigint NOT NULL DEFAULT 0;
		UPDATE migration_source.users SET created_at=1790672400 WHERE id=7;
		CREATE TABLE IF NOT EXISTS billing.funding_lots(lot_id text PRIMARY KEY,account_id text,source text,idempotency_key text,original_amount bigint,remaining_amount bigint);
		ALTER TABLE billing.funding_lots ADD COLUMN IF NOT EXISTS reference_type text NOT NULL DEFAULT '';
		ALTER TABLE billing.funding_lots ADD COLUMN IF NOT EXISTS reference_id text NOT NULL DEFAULT '';
		ALTER TABLE billing.funding_lots ADD COLUMN IF NOT EXISTS revenue_multiplier numeric NOT NULL DEFAULT 0;
		ALTER TABLE billing.funding_lots ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT '2026-09-29T09:00:00Z';
		INSERT INTO billing.accounts(account_id,owner_type,owner_id,account_type,quota_unit) VALUES('retired-gpt-7','user',7,'gpt_wallet','quota');
		INSERT INTO billing.balance_snapshots(account_id,available_balance,reserved_balance) VALUES('retired-gpt-7',9223372036854775807,9223372036854775807);
		INSERT INTO billing.funding_lots(lot_id,account_id,source,idempotency_key,original_amount,remaining_amount,reference_type,reference_id,revenue_multiplier,created_at) VALUES
		 ('funding-box-lot','wallet-7','blind_box','funding-box-credit',100,60,'blind_box_reward','box-reward',0.65,'2026-09-29T09:00:00Z'),
		 ('funding-retired-lot','retired-gpt-7','other','funding-retired-credit',9223372036854775807,9223372036854775807,'legacy_wallet','old-gpt',0,'2024-01-01T00:00:00Z');
		CREATE TABLE billing.funding_source_policies(source text PRIMARY KEY,revenue_multiplier numeric,updated_at timestamptz);
		INSERT INTO billing.funding_source_policies VALUES('blind_box',0.65,'2026-09-29T09:00:00Z');
		CREATE TABLE billing.funding_allocations(allocation_id text PRIMARY KEY,request_id text,lot_id text,account_id text,source text,amount bigint,revenue_multiplier numeric,created_at timestamptz);
		INSERT INTO billing.funding_allocations VALUES
		 ('funding-box-allocation','funding-request','funding-box-lot','wallet-7','blind_box',40,0.65,'2026-09-29T10:00:00Z'),
		 ('funding-retired-allocation','funding-old-request','funding-retired-lot','retired-gpt-7','other',50,0,'2024-01-02T00:00:00Z');
		CREATE TABLE billing.request_economics(request_id text PRIMARY KEY,channel_id bigint,route_pool_id bigint,actual_amount bigint,billing_source text,subscription_id bigint,procurement_cost_multiplier numeric,revenue_multiplier numeric,settled_at timestamptz,created_at timestamptz);
		INSERT INTO billing.request_economics VALUES('funding-request',13,0,40,'wallet',0,0.4,0,'2026-09-29T10:00:00Z','2026-09-29T10:00:00Z');
		CREATE TABLE billing.wallet_reward_holds(hold_id text PRIMARY KEY,account_id text,user_id bigint,original_amount bigint,consumed_amount bigint,reference_type text,reference_id text,idempotency_key text,created_at timestamptz);
		INSERT INTO billing.wallet_reward_holds VALUES('funding-hold','wallet-7',7,100,40,'blind_box_reward','box-reward','funding-hold-credit','2026-09-29T09:00:00Z');
		CREATE TABLE IF NOT EXISTS billing.reservations(reservation_id text PRIMARY KEY,request_id text,status text);
		INSERT INTO billing.reservations VALUES('funding-reservation','funding-request','settled');
		CREATE TABLE IF NOT EXISTS billing.settlements(settlement_id text PRIMARY KEY,status text);
		INSERT INTO billing.settlements VALUES('funding-settlement','completed');
		CREATE TABLE IF NOT EXISTS billing.outbox_events(event_id text PRIMARY KEY,status text);
		INSERT INTO billing.outbox_events VALUES('funding-outbox','published');`)
	if err != nil {
		t.Fatal(err)
	}
}
