//go:build pgintegration

package legacy

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedEditedKeyBudget(t *testing.T, source *pgxpool.Pool, cap int64) {
	t.Helper()
	seedUnknownObligation(t, source)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `UPDATE migration_source.tokens SET remain_quota=$1`, cap); err != nil {
		t.Fatal(err)
	}
	_, err := source.Exec(ctx, `
	 CREATE TABLE billing.outbox_events(aggregate_id text,payload jsonb,idempotency_key text,status text);
	 CREATE TABLE gateway.responses_background_jobs(id text,user_id bigint,token_id bigint,status text);
	 CREATE TABLE migration_source.tasks(id bigint,status text,private_data text);
	 CREATE SCHEMA workflow;
	 CREATE TABLE workflow.task_workflows(workflow_id text,request_id text,status text);
	 CREATE TABLE workflow.task_terminal_results(workflow_id text,settlement_status text);`)
	if err != nil {
		t.Fatal(err)
	}
}

func keyBudgetDrainReader(t *testing.T, source *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	reader := readonlySource(t, source)
	if _, err := source.Exec(context.Background(), "GRANT EXECUTE ON FUNCTION pg_control_system() TO "+pgx.Identifier{reader.Config().ConnConfig.User}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	return reader
}

func TestEditedKeyBudgetCapsPreserveBothNativeCapAndHistoricalMirror(t *testing.T) {
	for _, cap := range []int64{0, 50, 150} {
		t.Run(fmt.Sprint(cap), func(t *testing.T) {
			source, target, crypto := importTestDB(t)
			seedEditedKeyBudget(t, source, cap)
			ctx := context.Background()
			reader := keyBudgetDrainReader(t, source)
			drain, err := DrainSource(ctx, reader, SourceDrainOptions{})
			if err != nil || !drain.Completed || !drain.Source.ReadOnly || len(drain.Report.Issues) != 0 || drain.Report.Counts["key_budget_caps_differ_from_historical_mirror"] != 1 || drain.Report.Counts["key_budget_mirrors_checked"] != 1 {
				t.Fatalf("legitimate edited cap rejected: %+v %v", drain, err)
			}
			if len(drain.Checks) != 11 {
				t.Fatalf("canonical check coverage changed: %+v", drain.Checks)
			}
			for name, check := range drain.Checks {
				if !check.Present || !check.Completed {
					t.Fatalf("canonical check %s is incomplete: %+v", name, check)
				}
			}
			if _, err = reader.Exec(ctx, drain.ProjectionAssertionSQL); err != nil {
				t.Fatalf("generated post assertion rejects the same valid edited-cap contract: %v", err)
			}
			m := NewImporter(reader, target, crypto)
			for range 2 {
				r, err := m.Import(ctx, true)
				if err != nil || !r.Applied || len(r.Issues) != 0 {
					t.Fatalf("import/reapply=%+v err=%v", r, err)
				}
				if r, err = m.Check(ctx); err != nil || len(r.Issues) != 0 {
					t.Fatalf("check=%+v err=%v", r, err)
				}
			}
			var actual, mirror, current, historicalAfter, historicalDebit, nativeEntries int64
			if err = target.QueryRow(ctx, `SELECT balance,(SELECT count(*) FROM v3_billing.ledger_entries)
			 FROM v3_billing.accounts WHERE owner_type='api_key' AND owner_id=11 AND kind='key_budget'`).Scan(&actual, &nativeEntries); err != nil || actual != cap*microPerV2Unit {
				t.Fatalf("old mirror overwrote edited native cap=%d want=%d err=%v", actual, cap*microPerV2Unit, err)
			}
			if err = target.QueryRow(ctx, `SELECT balance_after,amount FROM v3_billing.historical_entries WHERE entry_id='unlinked-token-adjustment'`).Scan(&historicalAfter, &historicalDebit); err != nil || historicalAfter != 200 || historicalDebit != -10 {
				t.Fatalf("immutable old debit or old mirror changed: after=%d debit=%d err=%v", historicalAfter, historicalDebit, err)
			}
			if err = source.QueryRow(ctx, `SELECT t.remain_quota,s.available_balance FROM migration_source.tokens t JOIN billing.accounts a ON a.owner_type='token' AND a.owner_id=t.id JOIN billing.balance_snapshots s ON s.account_id=a.account_id`).Scan(&current, &mirror); err != nil || current != cap || mirror != 100 {
				t.Fatalf("migration modified source cap/mirror: cap=%d mirror=%d err=%v", current, mirror, err)
			}
			var status string
			var amount int64
			if err = target.QueryRow(ctx, `SELECT status,amount FROM v3_audit.request_audits WHERE request_id='rejected-request'`).Scan(&status, &amount); err != nil || status != "historical_unknown" || amount != 0 {
				t.Fatalf("unknown HTTP was fabricated or rebilled: %s %d %v", status, amount, err)
			}
		})
	}
}

func TestEditedKeyBudgetMirrorRejectsBrokenMoneyIdentityAndPendingWork(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedEditedKeyBudget(t, source, 50)
	ctx := context.Background()
	reader := keyBudgetDrainReader(t, source)
	valid, err := DrainSource(ctx, reader, SourceDrainOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, boundary := range []struct{ mutate, restore string }{
		{`UPDATE billing.balance_snapshots SET available_balance=99 WHERE account_id='token-11'`, `UPDATE billing.balance_snapshots SET available_balance=100 WHERE account_id='token-11'`},
		{`UPDATE billing.balance_snapshots SET granted_total=104 WHERE account_id='token-11'`, `UPDATE billing.balance_snapshots SET granted_total=105 WHERE account_id='token-11'`},
		{`UPDATE billing.balance_snapshots SET consumed_total=4 WHERE account_id='token-11'`, `UPDATE billing.balance_snapshots SET consumed_total=5 WHERE account_id='token-11'`},
		{`UPDATE billing.balance_snapshots SET refunded_total=1 WHERE account_id='token-11'`, `UPDATE billing.balance_snapshots SET refunded_total=0 WHERE account_id='token-11'`},
		{`UPDATE billing.balance_snapshots SET granted_total=NULL WHERE account_id='token-11'`, `UPDATE billing.balance_snapshots SET granted_total=105 WHERE account_id='token-11'`},
		{`UPDATE billing.ledger_entries SET amount=6 WHERE entry_id='unlinked-token-adjustment'`, `UPDATE billing.ledger_entries SET amount=5 WHERE entry_id='unlinked-token-adjustment'`},
		{`UPDATE billing.ledger_entries SET amount=-1 WHERE entry_id='unlinked-token-adjustment'`, `UPDATE billing.ledger_entries SET amount=5 WHERE entry_id='unlinked-token-adjustment'`},
		{`UPDATE billing.ledger_entries SET direction='credit' WHERE entry_id='unlinked-token-adjustment'`, `UPDATE billing.ledger_entries SET direction='debit' WHERE entry_id='unlinked-token-adjustment'`},
		{`UPDATE billing.ledger_entries SET reference_id='12' WHERE entry_id='unlinked-token-adjustment'`, `UPDATE billing.ledger_entries SET reference_id='11' WHERE entry_id='unlinked-token-adjustment'`},
		{`UPDATE billing.ledger_entries SET entry_type='unknown' WHERE entry_id='unlinked-token-adjustment'`, `UPDATE billing.ledger_entries SET entry_type='settle_debit' WHERE entry_id='unlinked-token-adjustment'`},
		{`UPDATE billing.accounts SET owner_type='user' WHERE account_id='token-11'`, `UPDATE billing.accounts SET owner_type='token' WHERE account_id='token-11'`},
		{`UPDATE billing.accounts SET owner_id=12 WHERE account_id='token-11'`, `UPDATE billing.accounts SET owner_id=11 WHERE account_id='token-11'`},
		{`UPDATE billing.accounts SET quota_unit='usd' WHERE account_id='token-11'`, `UPDATE billing.accounts SET quota_unit='quota' WHERE account_id='token-11'`},
		{`UPDATE migration_source.tokens SET user_id=8`, `UPDATE migration_source.tokens SET user_id=7`},
		{`UPDATE migration_source.tokens SET key=''`, `UPDATE migration_source.tokens SET key='kept-existing-key'`},
		{`UPDATE migration_source.tokens SET remain_quota=-1`, `UPDATE migration_source.tokens SET remain_quota=50`},
		{fmt.Sprintf(`UPDATE migration_source.tokens SET remain_quota=%d`, math.MaxInt64), `UPDATE migration_source.tokens SET remain_quota=50`},
		{`UPDATE billing.balance_snapshots SET reserved_balance=1 WHERE account_id='token-11'`, `UPDATE billing.balance_snapshots SET reserved_balance=0 WHERE account_id='token-11'`},
		{`INSERT INTO billing.reservations VALUES('token-open','unknown-key-request','token-11',1,'open')`, `DELETE FROM billing.reservations WHERE reservation_id='token-open'`},
		{`ALTER TABLE billing.balance_snapshots DROP COLUMN granted_total`, `ALTER TABLE billing.balance_snapshots ADD COLUMN granted_total bigint DEFAULT 0; UPDATE billing.balance_snapshots SET granted_total=105 WHERE account_id='token-11'`},
	} {
		t.Run(boundary.mutate, func(t *testing.T) {
			if _, err := source.Exec(ctx, boundary.mutate); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := source.Exec(ctx, boundary.restore); err != nil {
					t.Fatal(err)
				}
			})
			drain, err := DrainSource(ctx, reader, SourceDrainOptions{})
			if !errors.Is(err, ErrSourceDrainBlocked) || !drain.Completed || len(drain.Report.Issues) == 0 {
				t.Fatalf("bad money/owner/future obligation admitted: %+v %v", drain, err)
			}
			m := NewImporter(reader, target, crypto)
			if r, err := m.Import(ctx, true); err == nil || r.Applied {
				t.Fatalf("invalid source wrote target: %+v %v", r, err)
			}
			if !strings.HasPrefix(boundary.mutate, "ALTER ") {
				// A previously generated assertion must see the damaged live fact,
				// not accept its earlier read-only proof as a reusable waiver.
				if _, err := source.Exec(ctx, valid.ProjectionAssertionSQL); err == nil || !strings.Contains(err.Error(), "canonical_post_drain_failed") {
					t.Fatalf("same-transaction post assertion accepted damaged mirror: %v", err)
				}
			}
		})
	}
	var users int64
	if err := target.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users`).Scan(&users); err != nil || users != 0 {
		t.Fatalf("blocked source changed native identities: %d %v", users, err)
	}
}

func TestEditedKeyBudgetUnknown22CapsAreNotReplayedOrCompensated(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedEditedKeyBudget(t, source, 150)
	ctx := context.Background()
	_, err := source.Exec(ctx, `
	 INSERT INTO migration_source.tokens SELECT g,7,'kept-token-'||g,1,CASE WHEN g%2=0 THEN 150+g ELSE g END,false,'127.0.0.1' FROM generate_series(12,32) g;
	 INSERT INTO billing.accounts SELECT 'token-'||g,'token',g,'token','quota' FROM generate_series(12,32) g;
	 INSERT INTO billing.balance_snapshots SELECT 'token-'||g,100,0,105,5,0 FROM generate_series(12,32) g;
	 INSERT INTO billing.ledger_entries(entry_id,account_id,reference_type,reference_id,entry_type,direction,amount,balance_after,idempotency_key,metadata)
	 SELECT 'bootstrap-'||g,'token-'||g,'token',g::text,'grant_credit','credit',105,105,'mirror-bootstrap:token:'||g,'{}'::jsonb FROM generate_series(12,32) g;
	 INSERT INTO billing.ledger_entries(entry_id,account_id,reference_type,reference_id,entry_type,direction,amount,balance_after,idempotency_key,metadata)
	 SELECT 'adjustment-'||g,'token-'||g,'token',g::text,'settle_debit','debit',5,100,'token-adjust:old-'||g,'{}'::jsonb FROM generate_series(12,32) g;
	 INSERT INTO gateway.request_audits SELECT 'unknown-key-'||g,trace_id,user_id,g,model_name,group_name,protocol,request_type,status,counted_in_success_rate,billable,quota,prompt_tokens,completion_tokens,final_channel_id,attempts_count,retry_count,status_code,error_code,started_at,completed_at,created_at,updated_at FROM gateway.request_audits,generate_series(12,32) g WHERE request_id='rejected-request';`)
	if err != nil {
		t.Fatal(err)
	}
	reader := keyBudgetDrainReader(t, source)
	drain, err := DrainSource(ctx, reader, SourceDrainOptions{})
	if err != nil || !drain.Completed || drain.Report.Counts["key_budget_mirrors_checked"] != 22 || drain.Report.Counts["key_budget_caps_differ_from_historical_mirror"] != 22 {
		t.Fatalf("finite-key unknown set failed canonical drain: %+v %v", drain, err)
	}
	m := NewImporter(reader, target, crypto)
	for range 2 {
		if r, err := m.Import(ctx, true); err != nil || !r.Applied {
			t.Fatalf("unknown-key import/reapply=%+v %v", r, err)
		}
		if r, err := m.Check(ctx); err != nil || len(r.Issues) != 0 {
			t.Fatalf("unknown-key check=%+v %v", r, err)
		}
	}
	var actual, want, unknown, entries, historicalDebits int64
	if err = source.QueryRow(ctx, `SELECT sum(remain_quota)*2 FROM migration_source.tokens`).Scan(&want); err != nil {
		t.Fatal(err)
	}
	if err = target.QueryRow(ctx, `SELECT (SELECT sum(balance) FROM v3_billing.accounts WHERE owner_type='api_key' AND kind='key_budget'),
	 (SELECT count(*) FROM v3_audit.request_audits WHERE (request_id='rejected-request' OR request_id LIKE 'unknown-key-%') AND status='historical_unknown' AND amount=0 AND NOT billable),
	 (SELECT count(*) FROM v3_billing.ledger_entries),
	 (SELECT count(*) FROM v3_billing.historical_entries WHERE reference_type='token' AND direction='debit' AND amount=-10 AND balance_after=200)`).Scan(&actual, &unknown, &entries, &historicalDebits); err != nil || actual != want || unknown != 22 || entries != 23 || historicalDebits != 22 {
		t.Fatalf("unknown token debits were replayed/refunded or evidence lost: balance=%d want=%d unknown=%d entries=%d history=%d err=%v", actual, want, unknown, entries, historicalDebits, err)
	}
}

func TestEditedKeyBudgetMirrorCountsCompletedSettlementBuckets(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedEditedKeyBudget(t, source, 50)
	ctx := context.Background()
	_, err := source.Exec(ctx, `
	 INSERT INTO billing.reservations VALUES('token-settled','token-settlement-request','token-11',30,'settled');
	 INSERT INTO billing.settlements VALUES('token-settlement','token-settled',25,-5,'token-settlement-request','completed');
	 INSERT INTO billing.ledger_entries(entry_id,account_id,reference_type,reference_id,entry_type,direction,amount,balance_after,idempotency_key,metadata)
	 VALUES('token-reserve','token-11','reservation','token-settled','reserve_hold','debit',30,70,'token:reserve:archived','{}'),
	 ('token-settlement-refund','token-11','settlement','token-settlement','settle_credit','credit',5,75,'token:settle:archived','{}');
	 UPDATE billing.balance_snapshots SET available_balance=75,consumed_total=30,refunded_total=5 WHERE account_id='token-11';`)
	if err != nil {
		t.Fatal(err)
	}
	reader := keyBudgetDrainReader(t, source)
	valid, err := DrainSource(ctx, reader, SourceDrainOptions{})
	if err != nil || !valid.Completed || valid.Report.Counts["key_budget_caps_differ_from_historical_mirror"] != 1 {
		t.Fatalf("completed settlement did not reconcile its consumption/refund buckets: %+v %v", valid, err)
	}
	if _, err = reader.Exec(ctx, valid.ProjectionAssertionSQL); err != nil {
		t.Fatal(err)
	}
	m := NewImporter(reader, target, crypto)
	if r, err := m.Import(ctx, true); err != nil || !r.Applied {
		t.Fatalf("completed settlement import=%+v %v", r, err)
	}
	if r, err := m.Check(ctx); err != nil || len(r.Issues) != 0 {
		t.Fatalf("completed settlement check=%+v %v", r, err)
	}
	for _, boundary := range []struct{ mutate, restore string }{
		{`UPDATE billing.balance_snapshots SET consumed_total=25 WHERE account_id='token-11'`, `UPDATE billing.balance_snapshots SET consumed_total=30 WHERE account_id='token-11'`},
		{`UPDATE billing.balance_snapshots SET refunded_total=10 WHERE account_id='token-11'`, `UPDATE billing.balance_snapshots SET refunded_total=5 WHERE account_id='token-11'`},
		{`UPDATE billing.settlements SET actual_amount=24 WHERE settlement_id='token-settlement'`, `UPDATE billing.settlements SET actual_amount=25 WHERE settlement_id='token-settlement'`},
		{`UPDATE billing.settlements SET delta_amount=-6 WHERE settlement_id='token-settlement'`, `UPDATE billing.settlements SET delta_amount=-5 WHERE settlement_id='token-settlement'`},
		{`UPDATE billing.settlements SET status='pending' WHERE settlement_id='token-settlement'`, `UPDATE billing.settlements SET status='completed' WHERE settlement_id='token-settlement'`},
		{`UPDATE billing.reservations SET status='released' WHERE reservation_id='token-settled'`, `UPDATE billing.reservations SET status='settled' WHERE reservation_id='token-settled'`},
		{`UPDATE billing.reservations SET reserved_amount=NULL WHERE reservation_id='token-settled'`, `UPDATE billing.reservations SET reserved_amount=30 WHERE reservation_id='token-settled'`},
	} {
		t.Run(boundary.mutate, func(t *testing.T) {
			if _, err := source.Exec(ctx, boundary.mutate); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := source.Exec(ctx, boundary.restore); err != nil {
					t.Fatal(err)
				}
			})
			drain, err := DrainSource(ctx, reader, SourceDrainOptions{})
			if !errors.Is(err, ErrSourceDrainBlocked) || !drain.Completed {
				t.Fatalf("broken settlement admitted: %+v %v", drain, err)
			}
			if r, err := m.Import(ctx, true); err == nil || r.Applied {
				t.Fatalf("broken settlement reapplied=%+v %v", r, err)
			}
			if _, err := source.Exec(ctx, valid.ProjectionAssertionSQL); err == nil || !strings.Contains(err.Error(), "canonical_post_drain_failed") {
				t.Fatalf("post assertion admitted broken settlement: %v", err)
			}
		})
	}
}
