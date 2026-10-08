//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedArchivedSourceContracts(t *testing.T, source *pgxpool.Pool) {
	t.Helper()
	_, err := source.Exec(context.Background(), `CREATE SCHEMA gateway;
	 CREATE TABLE gateway.request_executions(execution_id text,request_id text,route_plan_id text,status text);
	 CREATE TABLE gateway.execution_attempts(attempt_id text,execution_id text,status text);
	 CREATE TABLE gateway.route_plans(route_plan_id text,request_id text,status text);
	 CREATE TABLE gateway.usage_evidence(usage_evidence_id text,execution_id text,request_id text,actual_amount bigint);
	 INSERT INTO gateway.request_executions VALUES('execution-1','request-1','plan-1','settled');
	 INSERT INTO gateway.execution_attempts VALUES('attempt-1','execution-1','provider_completed');
	 INSERT INTO gateway.route_plans VALUES('plan-1','request-1','recorded');
	 INSERT INTO gateway.usage_evidence VALUES('evidence-1','execution-1','request-1',10);
	 CREATE TABLE migration_source.balance_blind_box_simulation_sessions(id bigint,user_id bigint,status text,simulated_cost_usd numeric,simulated_reward_value_usd numeric);
	 CREATE TABLE migration_source.balance_blind_box_simulation_batches(id bigint,session_id bigint,results_json text);
	 INSERT INTO migration_source.balance_blind_box_simulation_sessions VALUES(1,7,'active',1000,2000);
	 INSERT INTO migration_source.balance_blind_box_simulation_batches VALUES(1,1,'[{"simulation":true,"credit_amount":999999999}]');
	 CREATE TABLE migration_source.community_resources(id bigint,status text,reward_quota bigint,description text);
	 INSERT INTO migration_source.community_resources VALUES(1,'pending',0,'PRIVATE-ARCHIVE-ONLY'),(2,'approved',500,'PRIVATE-ARCHIVE-ONLY');
	 CREATE TABLE migration_source.quota_data(id bigint,quota bigint);
	 INSERT INTO migration_source.quota_data VALUES(1,9999999);
	 CREATE TABLE migration_source.setups(id bigint,version text);
	 INSERT INTO migration_source.setups VALUES(1,'old-release');
	 CREATE TABLE migration_source.wallet_quota_conversions(id bigint,user_id bigint,status text,direction text,source_quota bigint,target_quota bigint,
	 standard_quota_before bigint,standard_quota_after bigint,claude_quota_before bigint,claude_quota_after bigint);
	 INSERT INTO migration_source.wallet_quota_conversions VALUES(1,7,'completed','standard_to_claude',400,100,400,0,400,500);`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOfflineSourceCoverageArchivesRetiredContractsWithoutRegrant(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedArchivedSourceContracts(t, source)
	ctx := context.Background()
	m := NewImporter(readonlySource(t, source), target, crypto)
	r, err := m.Import(ctx, false)
	if err != nil || len(r.Issues) != 0 || len(r.UnmappedSources) != 0 || r.Applied {
		t.Fatalf("preview=%+v err=%v", r, err)
	}
	for name, want := range map[string]int64{
		"archived_source_history.gateway_execution_attempts":            1,
		"archived_source_history.gateway_route_plans":                   1,
		"archived_source_history.gateway_usage_evidence":                1,
		"archived_source_history.balance_blind_box_simulation_batches":  1,
		"archived_source_history.balance_blind_box_simulation_sessions": 1,
		"archived_source_history.community_resources":                   2,
		"archived_source_history.setups":                                1,
		"archived_source_history.wallet_quota_conversions":              1,
		"rebuilt_read_projections.quota_data":                           1,
		"retired_features.wallet_quota_conversions":                     1,
	} {
		if r.Counts[name] != want {
			t.Fatalf("report count %s=%d want %d", name, r.Counts[name], want)
		}
	}
	if r.Amounts["retired_features.wallet_quota_conversions.source_quota_v2_units"] != "400" ||
		r.Amounts["retired_features.wallet_quota_conversions.target_quota_v2_units"] != "100" {
		t.Fatal("dual-wallet conversion evidence lost its original units")
	}
	encoded, err := json.Marshal(r)
	if err != nil || strings.Contains(string(encoded), "PRIVATE-ARCHIVE-ONLY") {
		t.Fatal("archive report exposes source content")
	}
	for range 2 {
		if r, err = m.Import(ctx, true); err != nil || !r.Applied {
			t.Fatalf("apply=%+v err=%v", r, err)
		}
		if r, err = m.Check(ctx); err != nil || len(r.Issues) != 0 {
			t.Fatalf("check=%+v err=%v", r, err)
		}
	}
	var balance, entries, rewardLots int64
	if err = target.QueryRow(ctx, `SELECT balance,(SELECT count(*) FROM v3_billing.ledger_entries),
	 (SELECT count(*) FROM v3_billing.funding_lots) FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet'`).Scan(&balance, &entries, &rewardLots); err != nil || balance != 1000 || entries != 1 || rewardLots != 0 {
		t.Fatalf("archived evidence created money: balance=%d entries=%d lots=%d err=%v", balance, entries, rewardLots, err)
	}
	var archived int
	if err = source.QueryRow(ctx, `SELECT count(*) FROM migration_source.community_resources`).Scan(&archived); err != nil || archived != 2 {
		t.Fatal("source archive was changed")
	}
	if _, err = source.Exec(ctx, `CREATE TABLE migration_source.new_unmapped_state(id bigint); INSERT INTO migration_source.new_unmapped_state VALUES(1)`); err != nil {
		t.Fatal(err)
	}
	// Renew the read-only role so its grants include the newly created table.
	m = NewImporter(readonlySource(t, source), target, crypto)
	if r, err = m.Import(ctx, true); err == nil || r.Applied || len(r.UnmappedSources) != 1 {
		t.Fatalf("new unknown state was accepted: %+v %v", r, err)
	}
}

func TestOfflineSourceCoverageRequiresSettledEvidenceAndCompletedConversions(t *testing.T) {
	for _, mutation := range []string{
		`UPDATE gateway.request_executions SET status='provider_completed'`,
		`DROP TABLE gateway.request_executions`,
		`UPDATE gateway.execution_attempts SET execution_id='missing'`,
		`UPDATE gateway.route_plans SET route_plan_id='missing'`,
		`UPDATE gateway.usage_evidence SET execution_id=NULL`,
		`UPDATE migration_source.wallet_quota_conversions SET status='pending'`,
	} {
		t.Run(mutation, func(t *testing.T) {
			source, target, crypto := importTestDB(t)
			seedArchivedSourceContracts(t, source)
			ctx := context.Background()
			if _, err := source.Exec(ctx, mutation); err != nil {
				t.Fatal(err)
			}
			m := NewImporter(readonlySource(t, source), target, crypto)
			r, err := m.Import(ctx, false)
			if err != nil || len(r.Issues) == 0 {
				t.Fatalf("unsafe preview=%+v err=%v", r, err)
			}
			if r, err = m.Import(ctx, true); err == nil || r.Applied {
				t.Fatal("unsettled or unproven source evidence applied")
			}
			var users int
			if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users`).Scan(&users); err != nil || users != 0 {
				t.Fatal("blocked import wrote users")
			}
		})
	}
}

func TestOfflineSourceCoverageExecutionEvidenceSupportsV2FlatAliases(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedArchivedSourceContracts(t, source)
	ctx := context.Background()
	// Move the four tables to emulate the flat names used by old v2 installs.
	if _, err := source.Exec(ctx, `ALTER TABLE gateway.request_executions SET SCHEMA migration_source;
	 ALTER TABLE migration_source.request_executions RENAME TO gateway_request_executions;
	 ALTER TABLE gateway.execution_attempts SET SCHEMA migration_source;
	 ALTER TABLE migration_source.execution_attempts RENAME TO gateway_execution_attempts;
	 ALTER TABLE gateway.route_plans SET SCHEMA migration_source;
	 ALTER TABLE migration_source.route_plans RENAME TO gateway_route_plans;
	 ALTER TABLE gateway.usage_evidence SET SCHEMA migration_source;
	 ALTER TABLE migration_source.usage_evidence RENAME TO gateway_usage_evidence`); err != nil {
		t.Fatal(err)
	}
	r, err := NewImporter(readonlySource(t, source), target, crypto).Import(ctx, false)
	if err != nil || len(r.Issues) != 0 || len(r.UnmappedSources) != 0 || r.Counts["archived_source_history.gateway_usage_evidence"] != 1 {
		t.Fatalf("flat source contracts=%+v err=%v", r, err)
	}
}
