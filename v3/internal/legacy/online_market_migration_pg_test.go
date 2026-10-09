//go:build pgintegration

package legacy

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestOnlineMarketMigrationCatchUpFinalizationAndIndependentCheck(t *testing.T) {
	source, target, crypto := importTestDB(t)
	historyFixture(t, source)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `INSERT INTO migration_source.users(id,username,role,status,"group",quota,claude_quota,setting) VALUES(8,'consumer',1,1,'default',0,0,'{}')`); err != nil {
		t.Fatal(err)
	}
	seedChannelMarketFixture(t, source)
	// The shared typed fixture is deliberately unconstrained. Online keyset
	// copying requires genuine unique primary keys on every large source table.
	for _, table := range channelMarketSourceTables {
		if !cmStreamedTable(table) {
			continue
		}
		keys := "id"
		if table == "pelican_artifacts" {
			keys = "group_id,model"
		}
		if _, err := source.Exec(ctx, "ALTER TABLE "+pgx.Identifier{"marketplace", table}.Sanitize()+" ADD PRIMARY KEY("+keys+")"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := source.Exec(ctx, `INSERT INTO marketplace.settlements
	 SELECT (jsonb_populate_record(NULL::marketplace.settlements,to_jsonb(s)||jsonb_build_object('id','settlement-deleted','request_id','request-deleted'))).*
	 FROM marketplace.settlements s WHERE s.id='settlement-201';
	 UPDATE billing.balance_snapshots SET available_balance=190 WHERE account_id='fixture-market-pending-7';
	 ANALYZE`); err != nil {
		t.Fatal(err)
	}
	m := NewImporter(source, target, crypto)
	opts := OnlineOptions{RunID: "online-market-pipeline-20261009", SourceAdmin: source}
	var initialFK int64
	if err := target.QueryRow(ctx, "SELECT count(*) FROM pg_constraint WHERE contype='f' AND connamespace='v3_channelmarket'::regnamespace").Scan(&initialFK); err != nil || initialFK == 0 {
		t.Fatalf("fixture lacks marketplace foreign keys count=%d err=%v", initialFK, err)
	}
	onlineMigrationReady(t, m, opts)
	var baselinePending, initialMoney int64
	if err := target.QueryRow(ctx, "SELECT sum(net_micro) FROM "+onlineStage("v3_channelmarket.settlements")+" WHERE status='pending'").Scan(&baselinePending); err != nil || baselinePending != 380 {
		t.Fatalf("baseline pending=%d err=%v", baselinePending, err)
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_billing.ledger_entries").Scan(&initialMoney); err != nil || initialMoney != 0 {
		t.Fatalf("online baseline posted live money count=%d err=%v", initialMoney, err)
	}
	// A real transaction updates, inserts and deletes settlement identities, as
	// well as the canonical pending projection, after the initial verification.
	// Nothing is added to the schema after capture was installed.
	if _, err := source.Exec(ctx, `UPDATE marketplace.settlements SET status='released' WHERE id='settlement-201';
	 INSERT INTO marketplace.settlements
	 SELECT (jsonb_populate_record(NULL::marketplace.settlements,to_jsonb(s)||jsonb_build_object(
	  'id','settlement-new','request_id','request-new','status','pending','settlement_gross_amount',75,'owner_net_amount',70))).*
	 FROM marketplace.settlements s WHERE s.id='settlement-201';
	 DELETE FROM marketplace.settlements WHERE id='settlement-deleted';
	 UPDATE billing.balance_snapshots SET available_balance=70 WHERE account_id='fixture-market-pending-7';
	 UPDATE billing.balance_snapshots SET available_balance=10 WHERE account_id='fixture-market-platform-1';
	 UPDATE marketplace.ranking_snapshots SET request_count=101 WHERE id='ranking-201'`); err != nil {
		t.Fatal(err)
	}
	onlineMigrationSync(t, m, opts)
	if r, err := m.VerifyOnline(ctx, opts); err != nil || r.Phase != "verified" {
		t.Fatalf("changed marketplace verification=%+v err=%v", r, err)
	}
	var pending, settlements int64
	var oldReleased, deletedAbsent bool
	if err := target.QueryRow(ctx, `SELECT count(*),sum(net_micro) FILTER(WHERE status='pending'),
	 bool_or(id='settlement-201' AND status='released'),NOT bool_or(id='settlement-deleted')
	 FROM `+onlineStage("v3_channelmarket.settlements")).Scan(&settlements, &pending, &oldReleased, &deletedAbsent); err != nil || settlements != 2 || pending != 140 || !oldReleased || !deletedAbsent {
		t.Fatalf("delta settlement count=%d pending=%d released=%t deleted=%t err=%v", settlements, pending, oldReleased, deletedAbsent, err)
	}
	if _, err := SealOnlineCapture(ctx, source, opts.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec(ctx, "UPDATE marketplace.settlements SET owner_net_amount=1 WHERE id='settlement-new'"); err == nil {
		t.Fatal("sealed marketplace accepted a source write")
	}
	if r, err := m.FinalizeOnline(ctx, opts); err != nil || !r.Applied || len(r.Issues) != 0 {
		t.Fatalf("market finalization=%+v err=%v", r, err)
	}
	// This check has no private online view: it reads and projects every source
	// row again and compares all native fields independently of the receipt.
	if r, err := m.Check(ctx); err != nil || len(r.Issues) != 0 || r.Counts["check:v3_channelmarket.settlements"] != 2 || r.Counts["check:v3_channelmarket.ranking_snapshots"] != 1 {
		t.Fatalf("independent full marketplace check=%+v err=%v", r, err)
	}
	var wallet, ownerPending, platform, openings, uniqueOpenings, invalidFK, finalFK int64
	if err := target.QueryRow(ctx, `SELECT
	 (SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet'),
	 (SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='marketplace_pending'),
	 (SELECT balance FROM v3_billing.accounts WHERE owner_type='platform' AND owner_id=1 AND kind='platform_revenue'),
	 (SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='opening'),
	 (SELECT count(DISTINCT operation_id) FROM v3_billing.ledger_entries WHERE kind='opening'),
	 (SELECT count(*) FROM pg_constraint WHERE contype='f' AND NOT convalidated AND connamespace='v3_channelmarket'::regnamespace),
	 (SELECT count(*) FROM pg_constraint WHERE contype='f' AND connamespace='v3_channelmarket'::regnamespace)`).Scan(&wallet, &ownerPending, &platform, &openings, &uniqueOpenings, &invalidFK, &finalFK); err != nil || wallet != 1000 || ownerPending != 140 || platform != 20 || openings != 3 || uniqueOpenings != 3 || invalidFK != 0 || finalFK != initialFK {
		t.Fatalf("wallet=%d pending=%d platform=%d openings=%d/%d invalidFK=%d foreignKeys=%d/%d err=%v", wallet, ownerPending, platform, openings, uniqueOpenings, invalidFK, finalFK, initialFK, err)
	}
	if _, err := m.FinalizeOnline(ctx, opts); err == nil {
		t.Fatal("marketplace could be finalized twice")
	}
	var afterOpenings int64
	if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='opening'").Scan(&afterOpenings); err != nil || afterOpenings != openings {
		t.Fatalf("rejected replay changed live money: %d err=%v", afterOpenings, err)
	}
	// A native amount mutation must make the independent full check fail even
	// though the online receipt and settlement count are unchanged.
	if _, err := target.Exec(ctx, "UPDATE v3_channelmarket.settlements SET consumer_micro=consumer_micro+2 WHERE id='settlement-new'"); err != nil {
		t.Fatal(err)
	}
	if r, err := m.Check(ctx); err == nil || len(r.Issues) == 0 {
		t.Fatalf("independent check accepted changed native money: %+v err=%v", r, err)
	}
	t.Log("online marketplace baseline, settlement update/insert/delete, canonical pending catch-up, sealed finalization, full independent check, single openings and validated foreign keys passed")
}
