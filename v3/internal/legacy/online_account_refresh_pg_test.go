//go:build pgintegration

package legacy

import (
	"context"
	"testing"
)

func TestOnlineSyncUserEventsDoNotRewriteHistoricalAccounts(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	stage := onlineStage("v3_billing.historical_accounts")
	var before, after string
	if err := target.QueryRow(ctx, "SELECT jsonb_agg(jsonb_build_object('row',to_jsonb(t),'xmin',xmin::text) ORDER BY source_account_id)::text FROM "+stage+" t").Scan(&before); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryExec(t, source, `UPDATE migration_source.users SET setting='{"user_updated":true}' WHERE id=7`)
	r, err := m.SyncOnline(ctx, opts)
	if err != nil || r.Acknowledged != 1 || r.Pending != 0 {
		t.Fatalf("user event report=%+v err=%v", r, err)
	}
	if err := target.QueryRow(ctx, "SELECT jsonb_agg(jsonb_build_object('row',to_jsonb(t),'xmin',xmin::text) ORDER BY source_account_id)::text FROM "+stage+" t").Scan(&after); err != nil || after != before {
		t.Fatalf("user-only sync rewrote account rows: %v", err)
	}
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	t.Log("user-only event acknowledged with historical account values and xmin unchanged; full verify passed, no native money")
}

func TestOnlineSyncNewUserAndAccountInSeparateBatches(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	onlineRecoveryExec(t, source, `INSERT INTO migration_source.users(id,username,role,status,"group",quota,claude_quota,setting) VALUES(8,'newuser',1,1,'default',0,0,'{}');
 INSERT INTO billing.accounts(account_id,owner_type,owner_id,account_type,quota_unit) VALUES('wallet-8','user',8,'claude_wallet','quota')`)
	opts.SyncEventLimit = 1
	first, err := m.SyncOnline(ctx, opts)
	if err != nil || first.Acknowledged != 1 || first.Pending != 1 {
		t.Fatalf("first user event report=%+v err=%v", first, err)
	}
	var mappings, accounts int
	if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_migration_online.account_ids WHERE owner_type='user' AND owner_id=8 AND kind='wallet'").Scan(&mappings); err != nil || mappings != 1 {
		t.Fatalf("new wallet identity mappings=%d err=%v", mappings, err)
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM "+onlineStage("v3_billing.historical_accounts")+" WHERE source_account_id='wallet-8'").Scan(&accounts); err != nil || accounts != 0 {
		t.Fatalf("later account event was applied early: rows=%d err=%v", accounts, err)
	}
	if _, err := m.VerifyOnline(ctx, opts); err == nil {
		t.Fatal("verification accepted an unprocessed account event")
	}
	second, err := m.SyncOnline(ctx, opts)
	if err != nil || second.Acknowledged != 1 || second.Pending != 0 {
		t.Fatalf("second account event report=%+v err=%v", second, err)
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM "+onlineStage("v3_billing.historical_accounts")+" h JOIN v3_migration_online.account_ids a ON a.id=h.account_id WHERE h.source_account_id='wallet-8' AND a.owner_type='user' AND a.owner_id=8 AND a.kind='wallet'").Scan(&accounts); err != nil || accounts != 1 {
		t.Fatalf("later account event lost reserved wallet mapping: rows=%d err=%v", accounts, err)
	}
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	t.Log("new user reserves wallet once; next bounded batch refreshes account; pending account prevents early verification")
}

func TestOnlineSyncAccountMetadataStillRefreshes(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineRecoveryExec(t, source, `ALTER TABLE billing.accounts ADD COLUMN status text DEFAULT 'active', ADD COLUMN version bigint DEFAULT 0, ADD COLUMN metadata jsonb DEFAULT '{}'`)
	onlineMigrationReady(t, m, opts)
	onlineRecoveryExec(t, source, `UPDATE billing.accounts SET version=version+1,metadata='{"precise":9007199254740993}' WHERE account_id='wallet-7'`)
	r, err := m.SyncOnline(ctx, opts)
	if err != nil || r.Acknowledged != 1 || r.Pending != 0 {
		t.Fatalf("account metadata report=%+v err=%v", r, err)
	}
	var correct bool
	if err := target.QueryRow(ctx, "SELECT version=1 AND metadata='{"+`"precise":9007199254740993`+"}'::jsonb FROM "+onlineStage("v3_billing.historical_accounts")+" WHERE source_account_id='wallet-7'").Scan(&correct); err != nil || !correct {
		t.Fatalf("account metadata refresh lost version or numeric precision: %v", err)
	}
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
}
