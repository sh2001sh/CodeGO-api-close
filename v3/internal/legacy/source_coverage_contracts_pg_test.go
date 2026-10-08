//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
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

type archivedEvidenceCaptureTx struct {
	pgx.Tx
	query string
}

func (tx *archivedEvidenceCaptureTx) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	if strings.HasPrefix(query, "WITH archived_execution_parents AS MATERIALIZED") {
		tx.query = query
	}
	return tx.Tx.Query(ctx, query, args...)
}

func TestArchivedExecutionEvidencePreservesEveryOriginalParentAndChildGuard(t *testing.T) {
	source, _, _ := importTestDB(t)
	seedProjectionDrain(t, source)
	for _, mutation := range []string{
		"",
		`UPDATE gateway.execution_attempts SET execution_id=''`,
		`UPDATE gateway.execution_attempts SET execution_id=NULL`,
		`UPDATE gateway.execution_attempts SET execution_id='missing'`,
		`UPDATE gateway.route_plans SET route_plan_id=''`,
		`UPDATE gateway.route_plans SET request_id=NULL`,
		`UPDATE gateway.route_plans SET request_id='wrong-request'`,
		`UPDATE gateway.usage_evidence SET execution_id=NULL`,
		`UPDATE gateway.usage_evidence SET request_id='wrong-request'`,
		`UPDATE gateway.usage_evidence SET actual_amount=99`,
		`UPDATE gateway.request_executions SET user_id=8`,
		`UPDATE gateway.request_executions SET token_id=12`,
		`UPDATE gateway.request_executions SET actual_amount=24`,
		`UPDATE billing.settlements SET delta_amount=-6`,
		`UPDATE billing.reservations SET status='open' WHERE reservation_id='stale-reservation'`,
		`INSERT INTO billing.reservations VALUES('other-open','stale-request','wallet-7',1,'open')`,
	} {
		t.Run(mutation, func(t *testing.T) {
			ctx := context.Background()
			tx, err := source.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if mutation != "" {
				if _, err = tx.Exec(ctx, mutation); err != nil {
					t.Fatal(err)
				}
			}
			sources, err := discoverSources(ctx, tx)
			if err != nil {
				t.Fatal(err)
			}
			predicate, err := executionDrainSQL(ctx, tx, sources, "execution")
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]int64{}
			for _, name := range []string{"execution_attempts", "route_plans", "usage_evidence"} {
				field, linked := "execution_id", ""
				if name == "route_plans" {
					field = "route_plan_id"
				}
				if name != "execution_attempts" {
					linked = " AND child.request_id IS NOT DISTINCT FROM execution.request_id"
				}
				if name == "usage_evidence" {
					linked += " AND child.actual_amount IS NOT DISTINCT FROM execution.actual_amount"
				}
				// Compare against the previous correlated contract, including its
				// exact per-child existential semantics, under the same snapshot.
				var count int64
				err = tx.QueryRow(ctx, `SELECT count(*) FROM `+sources["gateway_"+name]+` child
				 WHERE COALESCE(child.`+field+`,'')='' OR NOT EXISTS(SELECT 1 FROM `+sources["gateway_request_executions"]+` execution
				 WHERE execution.`+field+`=child.`+field+` AND (`+predicate+`)`+linked+`)`).Scan(&count)
				if err != nil {
					t.Fatal(err)
				}
				want["unproven_execution_evidence.gateway_"+name] = count
			}
			r := Report{Counts: map[string]int64{}}
			if err = validateArchivedExecutionEvidence(ctx, tx, sources, map[string]bool{}, &r); err != nil {
				t.Fatal(err)
			}
			for name, count := range want {
				if r.Counts[name] != count {
					t.Fatalf("guard changed %s=%d want=%d mutation=%s", name, r.Counts[name], count, mutation)
				}
			}
			if (mutation == "") != (len(r.Issues) == 0) {
				t.Fatalf("unsafe evidence admitted or valid evidence refused: %+v", r)
			}
		})
	}
}

func TestArchivedExecutionEvidenceClassifiesParentsOnceAndSpillsBoundedly(t *testing.T) {
	source, _, _ := importTestDB(t)
	seedArchivedSourceContracts(t, source)
	ctx := context.Background()
	_, err := source.Exec(ctx, `
	 INSERT INTO gateway.request_executions SELECT 'execution-'||g,'request-'||g,'plan-'||g,'settled' FROM generate_series(2,40000) g;
	 INSERT INTO gateway.request_executions SELECT * FROM gateway.request_executions WHERE execution_id='execution-1';
	 INSERT INTO gateway.execution_attempts SELECT 'attempt-'||g,'execution-'||g,'provider_completed' FROM generate_series(2,40000) g;
	 INSERT INTO gateway.route_plans SELECT 'plan-'||g,'request-'||g,'recorded' FROM generate_series(2,40000) g;
	 INSERT INTO gateway.usage_evidence SELECT 'evidence-'||g,'execution-'||g,'request-'||g,10 FROM generate_series(2,40000) g;
	 INSERT INTO gateway.execution_attempts VALUES('missing','absent','recorded'),('empty','','recorded');
	 INSERT INTO gateway.route_plans VALUES('absent','absent','recorded'),('','request-1','recorded');
	 INSERT INTO gateway.usage_evidence VALUES('missing','absent','absent',10),('empty','','request-1',10);
	 ANALYZE gateway.request_executions; ANALYZE gateway.execution_attempts; ANALYZE gateway.route_plans; ANALYZE gateway.usage_evidence;`)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SET LOCAL work_mem='64kB'`); err != nil {
		t.Fatal(err)
	}
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	capture := &archivedEvidenceCaptureTx{Tx: tx}
	r := Report{Counts: map[string]int64{}}
	if err = validateArchivedExecutionEvidence(ctx, capture, sources, map[string]bool{}, &r); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"execution_attempts", "route_plans", "usage_evidence"} {
		if r.Counts["archived_source_history.gateway_"+name] != 40002 || r.Counts["unproven_execution_evidence.gateway_"+name] != 2 {
			t.Fatalf("duplicate parent multiplied child counts or unsafe identifier accepted: %+v", r.Counts)
		}
	}
	var planJSON []byte
	if err = tx.QueryRow(ctx, `EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON)`+capture.query).Scan(&planJSON); err != nil {
		t.Fatal(err)
	}
	var plan []map[string]any
	if err = json.Unmarshal(planJSON, &plan); err != nil {
		t.Fatal(err)
	}
	var parents, antiJoins, parentReads, spills int
	childReads := map[string]int{}
	var inspect func(map[string]any) bool
	inspect = func(node map[string]any) bool {
		spilled := false
		if node["Relation Name"] == "request_executions" {
			parents++
			if node["Actual Loops"] != float64(1) || node["Actual Rows"] != float64(40001) {
				t.Fatalf("parent classification repeated: %v", node)
			}
		}
		if name, ok := node["Relation Name"].(string); ok && (name == "execution_attempts" || name == "route_plans" || name == "usage_evidence") {
			childReads[name]++
			if node["Actual Loops"] != float64(1) || node["Actual Rows"] != float64(40002) {
				t.Fatalf("child evidence scan repeated: %v", node)
			}
		}
		if node["Node Type"] == "CTE Scan" && node["CTE Name"] == "archived_execution_parents" {
			parentReads++
			if node["Actual Loops"] != float64(1) || node["Actual Rows"] != float64(40001) {
				t.Fatalf("classified parents reread per child: %v", node)
			}
		}
		if node["Node Type"] == "Hash" {
			if node["Hash Batches"].(float64) > 1 {
				spills++
				spilled = true
			}
			if node["Peak Memory Usage"].(float64) > 1024 {
				t.Fatalf("hash work exceeded 1 MiB at work_mem=64 KiB: %v", node)
			}
		}
		if node["Node Type"] == "Sort" {
			if node["Sort Space Type"] == "Disk" {
				spills++
				spilled = true
			} else if node["Sort Space Used"].(float64) > 1024 {
				t.Fatalf("sort work exceeded 1 MiB at work_mem=64 KiB: %v", node)
			}
		}
		if children, ok := node["Plans"].([]any); ok {
			for _, child := range children {
				spilled = inspect(child.(map[string]any)) || spilled
			}
		}
		if node["Join Type"] == "Anti" {
			antiJoins++
			// PG15 can prefer external merge sorts at low work_mem while PG17
			// chooses batched hashes. Both must reuse the parent proof once and
			// spill bounded working sets, rather than rescan it per child.
			if (node["Node Type"] != "Hash Join" && node["Node Type"] != "Merge Join") || node["Actual Loops"] != float64(1) || !spilled {
				t.Fatalf("anti join lacks bounded, single-pass evidence: %v", node)
			}
		}
		return spilled
	}
	inspect(plan[0]["Plan"].(map[string]any))
	if parents != 1 || antiJoins != 3 || parentReads != 3 || spills < 3 || childReads["execution_attempts"] != 1 || childReads["route_plans"] != 1 || childReads["usage_evidence"] != 1 {
		t.Fatalf("bounded reuse plan parents=%d anti_joins=%d parent_reads=%d spills=%d child_reads=%v plan=%s", parents, antiJoins, parentReads, spills, childReads, planJSON)
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
