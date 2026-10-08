//go:build pgintegration

package legacy

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedProjectionDrain(t *testing.T, source *pgxpool.Pool) {
	t.Helper()
	historyFixture(t, source)
	_, err := source.Exec(context.Background(), `
	 CREATE TABLE billing.reservations(reservation_id text PRIMARY KEY,request_id text,account_id text,reserved_amount bigint,status text);
	 CREATE TABLE billing.settlements(settlement_id text PRIMARY KEY,reservation_id text UNIQUE,actual_amount bigint,delta_amount bigint,usage_evidence_id text,status text);
	 CREATE TABLE gateway.request_executions(execution_id text PRIMARY KEY,request_id text UNIQUE,route_plan_id text,status text,user_id bigint,token_id bigint,account_id text,reservation_id text,settlement_id text,actual_amount bigint,usage_evidence_id text);
	 CREATE TABLE gateway.execution_attempts(attempt_id text,execution_id text,status text);
	 CREATE TABLE gateway.route_plans(route_plan_id text,request_id text,status text);
	 CREATE TABLE gateway.usage_evidence(usage_evidence_id text,execution_id text,request_id text,actual_amount bigint);
	 INSERT INTO billing.reservations VALUES('stale-reservation','stale-request','wallet-7',30,'settled'),('released-reservation','released-request','wallet-7',5,'released');
	 INSERT INTO billing.settlements VALUES('stale-settlement','stale-reservation',25,-5,'stale-request','completed');
	 INSERT INTO gateway.request_executions VALUES('stale-execution','stale-request','stale-plan','provider_completed',7,11,'wallet-7','stale-reservation','stale-settlement',100,'stale-request');
	 INSERT INTO gateway.execution_attempts VALUES('stale-attempt','stale-execution','provider_completed');
	 INSERT INTO gateway.route_plans VALUES('stale-plan','stale-request','recorded');
	 INSERT INTO gateway.usage_evidence VALUES('stale-request','stale-execution','stale-request',100);
	 INSERT INTO gateway.request_audits SELECT 'stale-request','',7,11,'chat-model','default','chat','text','in_flight',true,false,0,0,0,13,0,0,0,'',
	 '2023-11-14T22:15:00Z'::timestamptz,'0001-01-01T00:00:00Z'::timestamptz,'2023-11-14T22:15:00Z'::timestamptz,'2023-11-14T22:15:00Z'::timestamptz;
	 INSERT INTO gateway.request_audits SELECT 'released-request','',7,11,'chat-model','default','chat','text','in_flight',true,false,0,0,0,13,0,0,0,'',
	 '2023-11-14T22:15:00Z'::timestamptz,'0001-01-01T00:00:00Z'::timestamptz,'2023-11-14T22:15:00Z'::timestamptz,'2023-11-14T22:15:00Z'::timestamptz;
	 INSERT INTO gateway.request_audits SELECT 'rejected-request','',7,11,'chat-model','default','chat','text','in_flight',true,false,0,0,0,0,0,0,0,'',
	 '2023-11-14T22:15:00Z'::timestamptz,'0001-01-01T00:00:00Z'::timestamptz,'2023-11-14T22:15:00Z'::timestamptz,'2023-11-14T22:15:00Z'::timestamptz;
	 INSERT INTO migration_source.logs VALUES(55,7,1700000100,2,'funding debit','alice','key','chat-model',25,2,1,1,true,13,11,'default','','stale-request','','{}'),
	 (56,7,1700000101,5,'final refusal','alice','key','chat-model',0,0,0,0,false,0,11,'default','','rejected-request','',
	 '{"status":"failed","status_code":429,"error_type":"service_busy","error_code":"rejected","retry_count":0,"counted_in_success_rate":false,"request_path":"/v1/chat/completions"}');`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestProjectionDrainCanonicalAmountsAndUnknownHTTPRemainIndependent(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedProjectionDrain(t, source)
	ctx := context.Background()
	m := NewImporter(readonlySource(t, source), target, crypto)
	r, err := m.Import(ctx, false)
	if err != nil || len(r.Issues) != 0 || r.Counts["archived_projection_diagnostics.gateway_request_executions"] != 1 || r.Counts["archived_projection_diagnostics.gateway_request_audits"] != 3 {
		t.Fatalf("preview=%+v err=%v", r, err)
	}
	for range 2 {
		if r, err = m.Import(ctx, true); err != nil || !r.Applied {
			t.Fatalf("apply=%+v err=%v", r, err)
		}
		if r, err = m.Check(ctx); err != nil || len(r.Issues) != 0 {
			t.Fatalf("check=%+v err=%v", r, err)
		}
	}
	var status string
	var counted, billable bool
	var quota, balance, entries int64
	var completed time.Time
	if err = target.QueryRow(ctx, `SELECT status,counted_in_success_rate,billable,amount,completed_at FROM v3_audit.request_audits WHERE request_id='stale-request'`).Scan(&status, &counted, &billable, &quota, &completed); err != nil || status != "historical_unknown" || counted || billable || quota != 0 || completed.Year() != 1 {
		t.Fatalf("HTTP invented: %s counted=%t billable=%t quota=%d time=%v err=%v", status, counted, billable, quota, completed, err)
	}
	if err = target.QueryRow(ctx, `SELECT balance,(SELECT count(*) FROM v3_billing.ledger_entries) FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet'`).Scan(&balance, &entries); err != nil || balance != 1000 || entries != 1 {
		t.Fatalf("double billing balance=%d entries=%d err=%v", balance, entries, err)
	}
	if err = source.QueryRow(ctx, `SELECT status,quota FROM gateway.request_audits WHERE request_id='stale-request'`).Scan(&status, &quota); err != nil || status != "in_flight" || quota != 0 {
		t.Fatal("source audit mutated")
	}
	if _, err = target.Exec(ctx, `UPDATE v3_audit.request_audits SET counted_in_success_rate=true WHERE request_id='stale-request'`); err != nil {
		t.Fatal(err)
	}
	if r, err = m.Check(ctx); err == nil || len(r.Issues) == 0 {
		t.Fatal("unknown outcome mutation not detected")
	}
}

func TestProjectionDrainRejectsConflictsOrUnprovenOldRecords(t *testing.T) {
	for _, mutation := range []string{
		`UPDATE billing.reservations SET status='open' WHERE reservation_id='stale-reservation'`,
		`UPDATE billing.reservations SET request_id='wrong-request' WHERE reservation_id='stale-reservation'`,
		`UPDATE billing.settlements SET reservation_id='released-reservation'`,
		`UPDATE billing.settlements SET delta_amount=-6`,
		`UPDATE billing.settlements SET usage_evidence_id='wrong-request'`,
		`UPDATE gateway.request_executions SET user_id=8`,
		`UPDATE gateway.request_executions SET token_id=12`,
		`UPDATE gateway.request_executions SET actual_amount=11`,
		`UPDATE gateway.usage_evidence SET actual_amount=11`,
		`UPDATE gateway.usage_evidence SET request_id='orphan-request'`,
		`UPDATE gateway.request_audits SET status='succeeded',billable=true,quota=24 WHERE request_id='stale-request'`,
		`UPDATE billing.reservations SET status='expired' WHERE request_id='released-request'`,
		`UPDATE migration_source.logs SET other='{"status":"failed","is_channel_test":true}' WHERE request_id='rejected-request'`,
		`UPDATE migration_source.logs SET other='invalid-json' WHERE request_id='rejected-request'`,
		`DELETE FROM migration_source.logs WHERE request_id='rejected-request'; UPDATE gateway.request_audits SET billable=true WHERE request_id='rejected-request'`,
		`UPDATE gateway.request_audits SET token_id=12 WHERE request_id='rejected-request'`,
		`INSERT INTO billing.reservations VALUES('other-open','stale-request','wallet-7',1,'open')`,
	} {
		t.Run(mutation, func(t *testing.T) {
			source, target, crypto := importTestDB(t)
			seedProjectionDrain(t, source)
			ctx := context.Background()
			if _, err := source.Exec(ctx, mutation); err != nil {
				t.Fatal(err)
			}
			m := NewImporter(readonlySource(t, source), target, crypto)
			r, err := m.Import(ctx, false)
			if mutation == `UPDATE migration_source.logs SET other='invalid-json' WHERE request_id='rejected-request'` {
				if err == nil {
					t.Fatal("malformed final evidence did not block")
				}
			} else if err != nil || len(r.Issues) == 0 {
				t.Fatalf("unsafe preview=%+v err=%v", r, err)
			}
			if r, err = m.Import(ctx, true); err == nil || r.Applied {
				t.Fatal("conflict accepted")
			}
			var users int
			if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users`).Scan(&users); err != nil || users != 0 {
				t.Fatal("blocked import wrote target")
			}
		})
	}
}

func TestProjectionDrainTerminalBackgroundDoesNotInventHTTP(t *testing.T) {
	source, _, _ := importTestDB(t)
	seedProjectionDrain(t, source)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `CREATE TABLE gateway.responses_background_jobs(id text,user_id bigint,token_id bigint,status text);
	 INSERT INTO gateway.responses_background_jobs VALUES('rejected-request',7,11,'in_progress');
	 DELETE FROM migration_source.logs WHERE request_id='rejected-request'`); err != nil {
		t.Fatal(err)
	}
	tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	condition, err := auditDrainSQL(ctx, tx, sources, "audit")
	if err != nil {
		t.Fatal(err)
	}
	var safe bool
	query := `SELECT ` + condition.predicate + ` FROM gateway.request_audits audit WHERE request_id='rejected-request'`
	if condition.ctes != "" {
		query = `WITH ` + condition.ctes + ` ` + query
	}
	if err = tx.QueryRow(ctx, query).Scan(&safe); err != nil || safe {
		t.Fatalf("active background safe=%t err=%v", safe, err)
	}
	_ = tx.Rollback(ctx)
	if _, err = source.Exec(ctx, `UPDATE gateway.responses_background_jobs SET status='failed'`); err != nil {
		t.Fatal(err)
	}
	tx, err = source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = tx.QueryRow(ctx, query).Scan(&safe); err != nil || !safe {
		t.Fatalf("terminal background safe=%t err=%v", safe, err)
	}
}

func TestProjectionDrainSubscriptionMatchesAllFundingSlices(t *testing.T) {
	source, _, _ := importTestDB(t)
	seedProjectionDrain(t, source)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `CREATE TABLE migration_source.user_subscriptions(id bigint,user_id bigint);
	 INSERT INTO migration_source.user_subscriptions VALUES(17,7);
	 UPDATE billing.accounts SET owner_type='user_subscription',owner_id=17,account_type='subscription';
	 UPDATE billing.settlements SET actual_amount=10,delta_amount=-20;
	 INSERT INTO billing.reservations VALUES('extra-reservation','stale-request','wallet-7',20,'settled');
	 INSERT INTO billing.settlements VALUES('extra-settlement','extra-reservation',15,-5,'stale-request','completed');
	 UPDATE gateway.request_audits SET status='succeeded',billable=true,quota=25 WHERE request_id='stale-request'`); err != nil {
		t.Fatal(err)
	}
	check := func(want bool) {
		t.Helper()
		tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		sources, err := discoverSources(ctx, tx)
		if err != nil {
			t.Fatal(err)
		}
		predicate, err := executionDrainSQL(ctx, tx, sources, "execution")
		if err != nil {
			t.Fatal(err)
		}
		var safe bool
		if err = tx.QueryRow(ctx, `SELECT `+predicate+` FROM gateway.request_executions execution`).Scan(&safe); err != nil || safe != want {
			t.Fatalf("split settlement safe=%t want=%t err=%v", safe, want, err)
		}
	}
	check(true)
	if _, err := source.Exec(ctx, `UPDATE billing.settlements SET actual_amount=14,delta_amount=-6 WHERE settlement_id='extra-settlement'`); err != nil {
		t.Fatal(err)
	}
	check(false)
}
