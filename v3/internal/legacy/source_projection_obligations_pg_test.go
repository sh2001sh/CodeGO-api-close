//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedUnknownObligation(t *testing.T, source *pgxpool.Pool) {
	t.Helper()
	seedProjectionDrain(t, source)
	_, err := source.Exec(context.Background(), `DELETE FROM migration_source.logs WHERE request_id='rejected-request';
	 UPDATE migration_source.tokens SET unlimited_quota=false;
	 ALTER TABLE billing.balance_snapshots ADD COLUMN granted_total bigint DEFAULT 0;
	 ALTER TABLE billing.balance_snapshots ADD COLUMN consumed_total bigint DEFAULT 0;
	 ALTER TABLE billing.balance_snapshots ADD COLUMN refunded_total bigint DEFAULT 0;
	 INSERT INTO billing.accounts VALUES('token-11','token',11,'token','quota');
	 INSERT INTO billing.balance_snapshots VALUES('token-11',100,0,105,5,0);
	 INSERT INTO billing.ledger_entries(entry_id,account_id,reference_type,reference_id,entry_type,direction,amount,balance_after,idempotency_key,metadata)
	 VALUES('token-bootstrap','token-11','token','11','grant_credit','credit',105,105,'mirror-bootstrap:token:11','{}');
	 INSERT INTO billing.ledger_entries(entry_id,account_id,reference_type,reference_id,entry_type,direction,amount,balance_after,idempotency_key,metadata)
	 VALUES('unlinked-token-adjustment','token-11','token','11','settle_debit','debit',5,100,'token-adjust:old-operation','{}');`)
	if err != nil {
		t.Fatal(err)
	}
}

func unknownObligationQuery(t *testing.T, tx pgx.Tx, suffix string) string {
	t.Helper()
	ctx := context.Background()
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	condition, err := auditDrainSQL(ctx, tx, sources, "audit")
	if err != nil {
		t.Fatal(err)
	}
	query := `SELECT ` + condition.predicate + ` FROM gateway.request_audits audit ` + suffix
	if condition.ctes != "" {
		query = `WITH ` + condition.ctes + ` ` + query
	}
	return query
}

func TestProjectionUnknownNoObligationPreservesExistingKeyBudget(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedUnknownObligation(t, source)
	ctx := context.Background()
	m := NewImporter(readonlySource(t, source), target, crypto)
	for range 2 {
		r, err := m.Import(ctx, true)
		if err != nil || !r.Applied {
			t.Fatalf("import=%+v err=%v", r, err)
		}
		if r, err = m.Check(ctx); err != nil || len(r.Issues) != 0 {
			t.Fatalf("check=%+v err=%v", r, err)
		}
	}
	var status string
	var counted, billable bool
	var amount, budget, entries int64
	err := target.QueryRow(ctx, `SELECT status,counted_in_success_rate,billable,amount,
	 (SELECT balance FROM v3_billing.accounts WHERE owner_type='api_key' AND owner_id=11 AND kind='key_budget'),
	 (SELECT count(*) FROM v3_billing.ledger_entries) FROM v3_audit.request_audits WHERE request_id='rejected-request'`).Scan(&status, &counted, &billable, &amount, &budget, &entries)
	if err != nil || status != "historical_unknown" || counted || billable || amount != 0 || budget != 200 || entries != 2 {
		t.Fatalf("unknown/budget modified status=%s counted=%t billable=%t amount=%d budget=%d entries=%d err=%v", status, counted, billable, amount, budget, entries, err)
	}
	if _, err = source.Exec(ctx, `UPDATE billing.balance_snapshots SET available_balance=99 WHERE account_id='token-11'`); err != nil {
		t.Fatal(err)
	}
	if r, err := m.Import(ctx, true); err == nil || r.Applied {
		t.Fatal("immutable token ledger/snapshot inconsistency admitted")
	}
}

func TestProjectionUnknownRejectsAnyTraceableObligationOrUnknownShape(t *testing.T) {
	source, _, _ := importTestDB(t)
	seedUnknownObligation(t, source)
	mutations := []string{
		`UPDATE gateway.request_audits SET billable=true WHERE request_id='rejected-request'`,
		`UPDATE gateway.request_audits SET quota=1 WHERE request_id='rejected-request'`,
		`UPDATE gateway.request_audits SET user_id=8 WHERE request_id='rejected-request'`,
		`UPDATE gateway.request_audits SET token_id=12 WHERE request_id='rejected-request'`,
		`UPDATE migration_source.tokens SET user_id=8`,
		`INSERT INTO billing.reservations VALUES('pending','rejected-request','wallet-7',1,'open')`,
		`INSERT INTO billing.settlements VALUES('orphan','absent',0,0,'rejected-request','completed')`,
		`UPDATE billing.ledger_entries SET reference_type=NULL,reference_id='rejected-request' WHERE entry_id='unlinked-token-adjustment'`,
		`UPDATE billing.ledger_entries SET reference_type='unknown-kind',reference_id='rejected-request' WHERE entry_id='unlinked-token-adjustment'`,
		`INSERT INTO gateway.request_executions(execution_id,request_id) VALUES('pending','rejected-request')`,
		`INSERT INTO gateway.usage_evidence(request_id) VALUES('rejected-request')`,
		`CREATE TABLE billing.request_economics(request_id text); INSERT INTO billing.request_economics VALUES('rejected-request')`,
		`CREATE TABLE billing.funding_allocations(request_id text); INSERT INTO billing.funding_allocations VALUES('rejected-request')`,
		`CREATE TABLE migration_source.subscription_pre_consume_records(request_id text); INSERT INTO migration_source.subscription_pre_consume_records VALUES('rejected-request')`,
		`CREATE SCHEMA workflow; CREATE TABLE workflow.task_workflows(request_id text); INSERT INTO workflow.task_workflows VALUES('rejected-request')`,
		`CREATE TABLE migration_source.tasks(private_data text); INSERT INTO migration_source.tasks VALUES('{"request_id":"rejected-request"}')`,
		`CREATE TABLE gateway.responses_background_jobs(id text,user_id bigint,token_id bigint,status text); INSERT INTO gateway.responses_background_jobs VALUES('rejected-request',7,11,'in_progress')`,
		`CREATE TABLE gateway.responses_background_jobs(id text,user_id bigint,token_id bigint,status text); INSERT INTO gateway.responses_background_jobs VALUES('rejected-request',8,11,'failed')`,
		`INSERT INTO migration_source.logs(id,request_id,type,quota) VALUES(99,'rejected-request',2,0)`,
		`INSERT INTO migration_source.logs(id,request_id,type,user_id,token_id,other) VALUES(99,'rejected-request',5,7,11,'{}')`,
		`CREATE TABLE billing.request_economics(unknown_column text)`,
		`CREATE TABLE migration_source.tasks(unknown_column text)`,
		`CREATE SCHEMA workflow; CREATE TABLE workflow.task_workflows(unknown_column text)`,
		`CREATE TABLE billing.outbox_events(unknown_column text)`,
		`ALTER TABLE billing.ledger_entries DROP COLUMN idempotency_key`,
		`UPDATE gateway.request_audits SET request_id='unsupported:encoding' WHERE request_id='rejected-request'`,
		`UPDATE billing.ledger_entries SET idempotency_key='subscription:rejected-request:未識別操作' WHERE entry_id='unlinked-token-adjustment'`,
		`CREATE TABLE billing.outbox_events(aggregate_id text,payload jsonb,idempotency_key text); INSERT INTO billing.outbox_events VALUES('other','{}','outbox:subscription:rejected-request:未識別操作')`,
	}
	for _, prefix := range []string{"entry:subscription:", "subscription:", "entry:relay:wallet:", "relay:wallet:", "entry:relay:claude_wallet:", "relay:claude_wallet:", "entry:relay:subscription:", "relay:subscription:"} {
		mutations = append(mutations, `UPDATE billing.ledger_entries SET idempotency_key='`+prefix+`rejected-request:reserve-extra:42' WHERE entry_id='unlinked-token-adjustment'`)
		mutations = append(mutations, `CREATE TABLE billing.outbox_events(aggregate_id text,payload jsonb,idempotency_key text); INSERT INTO billing.outbox_events VALUES('other','{}','outbox:`+prefix+`rejected-request:settle:reservation')`)
	}
	for _, prefix := range []string{"entry:relay:claude_wallet:", "relay:claude_wallet:"} {
		mutations = append(mutations, `UPDATE billing.ledger_entries SET idempotency_key='`+prefix+`rejected-request:未識別操作' WHERE entry_id='unlinked-token-adjustment'`)
		mutations = append(mutations, `CREATE TABLE billing.outbox_events(aggregate_id text,payload jsonb,idempotency_key text); INSERT INTO billing.outbox_events VALUES('other','{}','outbox:`+prefix+`rejected-request:未識別操作')`)
	}
	for _, field := range []string{"request_id", "reference_id", "usage_evidence_id"} {
		mutations = append(mutations, `CREATE TABLE billing.outbox_events(aggregate_id text,payload jsonb,idempotency_key text); INSERT INTO billing.outbox_events VALUES('other','{"`+field+`":"rejected-request"}','other')`)
	}
	mutations = append(mutations, `CREATE TABLE billing.outbox_events(aggregate_id text,payload jsonb,idempotency_key text); INSERT INTO billing.outbox_events VALUES('rejected-request','{}','other')`)
	for i, mutation := range mutations {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			ctx := context.Background()
			tx, err := source.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if _, err = tx.Exec(ctx, mutation); err != nil {
				t.Fatal(err)
			}
			var safe bool
			err = tx.QueryRow(ctx, unknownObligationQuery(t, tx, `WHERE request_id IN ('rejected-request','unsupported:encoding')`)).Scan(&safe)
			if err != nil || safe {
				t.Fatalf("traceable/unknown admitted mutation=%s safe=%t err=%v", mutation, safe, err)
			}
		})
	}
	t.Run("malformed_task_json", func(t *testing.T) {
		ctx := context.Background()
		tx, err := source.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err = tx.Exec(ctx, `CREATE TABLE migration_source.tasks(private_data text); INSERT INTO migration_source.tasks VALUES('invalid-json')`); err != nil {
			t.Fatal(err)
		}
		var safe bool
		if err = tx.QueryRow(ctx, unknownObligationQuery(t, tx, `WHERE request_id='rejected-request'`)).Scan(&safe); err == nil {
			t.Fatal("malformed provider evidence silently admitted")
		}
	})
}

func TestProjectionUnknownReverseChecksAreSetBasedAndPrefixesLiteral(t *testing.T) {
	source, _, _ := importTestDB(t)
	seedUnknownObligation(t, source)
	ctx := context.Background()
	_, err := source.Exec(ctx, `
	 INSERT INTO gateway.request_audits SELECT 'unknown-'||g,trace_id,user_id,token_id,model_name,group_name,protocol,request_type,status,counted_in_success_rate,billable,quota,prompt_tokens,completion_tokens,final_channel_id,attempts_count,retry_count,status_code,error_code,started_at,completed_at,created_at,updated_at FROM gateway.request_audits,generate_series(1,1000) g WHERE request_id='rejected-request';
	 INSERT INTO billing.settlements SELECT 'unrelated-'||g,'unrelated-r-'||g,0,0,'unrelated-'||g,'completed' FROM generate_series(1,20000) g;
	 INSERT INTO billing.ledger_entries(entry_id,reference_id,idempotency_key) SELECT 'unrelated-'||g,'unrelated-'||g,'other:'||g FROM generate_series(1,20000) g;
	 CREATE INDEX obligation_key_idx ON billing.ledger_entries(idempotency_key);
	 ANALYZE gateway.request_audits; ANALYZE billing.settlements; ANALYZE billing.ledger_entries;
	 UPDATE gateway.request_audits SET request_id='literal%_id' WHERE request_id='rejected-request';
	 UPDATE billing.ledger_entries SET idempotency_key='subscription:literalXXid:reserve' WHERE entry_id='unlinked-token-adjustment'`)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	query := unknownObligationQuery(t, tx, `WHERE status='in_flight' AND request_id<>'stale-request' AND request_id<>'released-request'`)
	var planJSON []byte
	if err = tx.QueryRow(ctx, `EXPLAIN(ANALYZE,FORMAT JSON)`+query).Scan(&planJSON); err != nil {
		t.Fatal(err)
	}
	var plan []map[string]any
	if err = json.Unmarshal(planJSON, &plan); err != nil {
		t.Fatal(err)
	}
	scans := 0
	var inspect func(map[string]any)
	inspect = func(node map[string]any) {
		alias, _ := node["Alias"].(string)
		if strings.HasPrefix(alias, "fact") && (node["Relation Name"] == "settlements" || node["Relation Name"] == "ledger_entries") {
			scans++
			if node["Actual Loops"].(float64) != 1 {
				t.Fatalf("reverse scan repeated per audit: relation=%v loops=%v", node["Relation Name"], node["Actual Loops"])
			}
		}
		if children, ok := node["Plans"].([]any); ok {
			for _, child := range children {
				inspect(child.(map[string]any))
			}
		}
	}
	inspect(plan[0]["Plan"].(map[string]any))
	if scans != 2 {
		t.Fatalf("financial reverse scan count=%d want=2", scans)
	}
	var safe bool
	if err = tx.QueryRow(ctx, unknownObligationQuery(t, tx, `WHERE request_id='literal%_id'`)).Scan(&safe); err != nil || !safe {
		t.Fatalf("literal request ID wildcard mismatch safe=%t err=%v", safe, err)
	}
	_ = tx.Rollback(ctx)
	if _, err = source.Exec(ctx, `UPDATE billing.ledger_entries SET idempotency_key='subscription:literal%_id:reserve' WHERE entry_id='unlinked-token-adjustment'`); err != nil {
		t.Fatal(err)
	}
	tx, err = source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = tx.QueryRow(ctx, unknownObligationQuery(t, tx, `WHERE request_id='literal%_id'`)).Scan(&safe); err != nil || safe {
		t.Fatalf("literal exact prefix ignored safe=%t err=%v", safe, err)
	}
}

func TestProjectionTerminalEvidenceCannotOverrideFinancialOrProviderConflicts(t *testing.T) {
	source, _, _ := importTestDB(t)
	seedProjectionDrain(t, source)
	for _, evidence := range []string{"failure_log", "terminal_background"} {
		for i, mutation := range []string{
			`INSERT INTO billing.settlements VALUES('orphan','absent',0,0,'rejected-request','completed')`,
			`UPDATE billing.ledger_entries SET reference_id='rejected-request' WHERE entry_id='debit-original'`,
			`UPDATE billing.ledger_entries SET idempotency_key='subscription:rejected-request:reserve' WHERE entry_id='debit-original'`,
			`CREATE TABLE billing.funding_allocations(request_id text); INSERT INTO billing.funding_allocations VALUES('rejected-request')`,
			`CREATE TABLE migration_source.tasks(private_data text); INSERT INTO migration_source.tasks VALUES('{"request_id":"rejected-request"}')`,
		} {
			t.Run(fmt.Sprintf("%s/%d", evidence, i), func(t *testing.T) {
				ctx := context.Background()
				tx, err := source.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = tx.Rollback(ctx) }()
				if evidence == "terminal_background" {
					if _, err = tx.Exec(ctx, `DELETE FROM migration_source.logs WHERE request_id='rejected-request';
					 CREATE TABLE gateway.responses_background_jobs(id text,user_id bigint,token_id bigint,status text);
					 INSERT INTO gateway.responses_background_jobs VALUES('rejected-request',7,11,'failed')`); err != nil {
						t.Fatal(err)
					}
				}
				var safe bool
				if err = tx.QueryRow(ctx, unknownObligationQuery(t, tx, `WHERE request_id='rejected-request'`)).Scan(&safe); err != nil || !safe {
					t.Fatalf("valid terminal evidence rejected safe=%t err=%v", safe, err)
				}
				if _, err = tx.Exec(ctx, mutation); err != nil {
					t.Fatal(err)
				}
				if err = tx.QueryRow(ctx, unknownObligationQuery(t, tx, `WHERE request_id='rejected-request'`)).Scan(&safe); err != nil || safe {
					t.Fatalf("terminal evidence hid conflicting obligation mutation=%s safe=%t err=%v", mutation, safe, err)
				}
			})
		}
	}
}

func TestProjectionUnknownLegacyClaudePrefixesMatchRequestLiterally(t *testing.T) {
	source, _, _ := importTestDB(t)
	seedUnknownObligation(t, source)
	for _, prefix := range []string{"entry:relay:claude_wallet:", "relay:claude_wallet:"} {
		for _, outbox := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/outbox=%t", prefix, outbox), func(t *testing.T) {
				ctx := context.Background()
				tx, err := source.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = tx.Rollback(ctx) }()
				if _, err = tx.Exec(ctx, `UPDATE gateway.request_audits SET request_id='literal%_id' WHERE request_id='rejected-request'`); err != nil {
					t.Fatal(err)
				}
				update := `UPDATE billing.ledger_entries SET idempotency_key=$1 WHERE entry_id='unlinked-token-adjustment'`
				if outbox {
					if _, err = tx.Exec(ctx, `CREATE TABLE billing.outbox_events(aggregate_id text,payload jsonb,idempotency_key text); INSERT INTO billing.outbox_events VALUES('other','{}','other')`); err != nil {
						t.Fatal(err)
					}
					prefix = "outbox:" + prefix
					update = `UPDATE billing.outbox_events SET idempotency_key=$1`
				}
				for _, boundary := range []struct {
					key  string
					safe bool
				}{
					{prefix + "literalXXid:reserve", true},
					{strings.Replace(prefix, "claude_wallet:", "claude_wallet_other:", 1) + "literal%_id:reserve", true},
					{prefix + "literal%_id:reserve", false},
					{prefix + "literal%_id:未識別操作", false},
				} {
					if _, err = tx.Exec(ctx, update, boundary.key); err != nil {
						t.Fatal(err)
					}
					var safe bool
					if err = tx.QueryRow(ctx, unknownObligationQuery(t, tx, `WHERE request_id='literal%_id'`)).Scan(&safe); err != nil || safe != boundary.safe {
						t.Fatalf("legacy literal boundary key=%s safe=%t want=%t err=%v", boundary.key, safe, boundary.safe, err)
					}
				}
			})
		}
	}
}
