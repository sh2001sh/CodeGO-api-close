//go:build pgintegration

package migrations

import (
	"context"
	"testing"
)

func TestAccountProfileInvalidationIgnoresLedgerChurn(t *testing.T) {
	conn := connect(t)
	ctx := context.Background()
	steps := []struct {
		name, sql string
		want      int
	}{
		{"platform creation", `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('platform',1,'platform_revenue')`, 0},
		{"held income creation", `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',1,'marketplace_pending')`, 0},
		{"wallet creation", `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',1,'wallet')`, 1},
		{"unchanged wallet identity", `UPDATE v3_billing.accounts SET owner_id=owner_id,owner_type=owner_type,kind=kind WHERE kind='wallet'`, 1},
		{"wallet balance posting", `UPDATE v3_billing.accounts SET balance=100,version=version+1 WHERE kind='wallet'`, 1},
		{"wallet owner transfer", `UPDATE v3_billing.accounts SET owner_id=2 WHERE kind='wallet'`, 3},
		{"wallet kind removal", `UPDATE v3_billing.accounts SET kind='marketplace_earned' WHERE owner_id=2`, 4},
		{"budget creation", `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('api_key',3,'key_budget')`, 5},
		{"budget removal", `DELETE FROM v3_billing.accounts WHERE owner_type='api_key'`, 6},
		{"subscription creation", `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('subscription',4,'subscription')`, 7},
		{"subscription removal", `DELETE FROM v3_billing.accounts WHERE owner_type='subscription'`, 8},
		{"income upsert", `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('platform',1,'platform_revenue') ON CONFLICT(owner_type,owner_id,kind) DO UPDATE SET owner_id=EXCLUDED.owner_id`, 8},
		{"unused income removal", `DELETE FROM v3_billing.accounts WHERE kind='marketplace_earned'`, 8},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			mustExec(t, conn, step.sql)
			var got int
			if err := conn.QueryRow(ctx, `SELECT count(*) FROM v3_platform.cache_invalidation_outbox WHERE entity='account_profile'`).Scan(&got); err != nil || got != step.want {
				t.Fatalf("profile invalidations=%d want=%d err=%v", got, step.want, err)
			}
		})
	}
	var oldWallet, newWallet int
	if err := conn.QueryRow(ctx, `SELECT count(*) FILTER(WHERE entity_id='1'),count(*) FILTER(WHERE entity_id='2') FROM v3_platform.cache_invalidation_outbox WHERE entity='account_profile'`).Scan(&oldWallet, &newWallet); err != nil || oldWallet != 2 || newWallet != 2 {
		t.Fatalf("both old and new wallet profiles must invalidate: old=%d new=%d err=%v", oldWallet, newWallet, err)
	}
}
