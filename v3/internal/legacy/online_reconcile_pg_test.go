//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// All mutations in this file belong to disposable, synthetic fixture databases.
func retainedPauseFixture(t *testing.T, source *pgxpool.Pool, opts OnlineOptions) OnlineCaptureReport {
	t.Helper()
	ctx := context.Background()
	tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	report, err := ValidateOnlineCapture(ctx, tx, opts.RunID)
	_ = tx.Rollback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range report.Tables {
		name := pgx.Identifier{onlineCaptureSchema, onlineCaptureFunctionName(table.Name)}.Sanitize()
		onlineRecoveryExec(t, source, "CREATE OR REPLACE FUNCTION "+name+"() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,v3_migration_capture AS $$BEGIN RETURN NULL; END$$")
	}
	return report
}

func retainedNewEpochFixture(t *testing.T, source, target *pgxpool.Pool, previous OnlineOptions, capture OnlineCaptureReport) OnlineReconcilePrepareOptions {
	t.Helper()
	ctx := context.Background()
	for _, table := range capture.Tables {
		for _, trigger := range []string{onlineCaptureRowTrigger, onlineCaptureTruncateTrigger} {
			onlineRecoveryExec(t, source, "DROP TRIGGER "+pgx.Identifier{trigger}.Sanitize()+" ON "+table.Relation)
		}
		onlineRecoveryExec(t, source, "DROP TRIGGER IF EXISTS "+pgx.Identifier{onlineCaptureFenceTrigger}.Sanitize()+" ON "+table.Relation)
	}
	onlineRecoveryExec(t, source, "ALTER SCHEMA v3_migration_capture RENAME TO v3_migration_capture_retained_fixture")
	next := OnlineReconcilePrepareOptions{OnlineOptions: OnlineOptions{RunID: "retained-recovery-fixture-20261012", SourceAdmin: source}, PreviousRunID: previous.RunID}
	next.PreviousCaptureSchema = "v3_migration_capture_retained_fixture"
	if err := source.QueryRow(ctx, "SELECT 'v3_migration_capture_retained_fixture.events'::regclass::oid").Scan(&next.ExpectedPreviousCaptureEventsOID); err != nil {
		t.Fatal(err)
	}
	if _, err := SetupOnlineCapture(ctx, source, next.RunID, true); err != nil {
		t.Fatal(err)
	}
	tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	next.ExpectedSourceIdentity, err = onlineDatabaseIdentity(ctx, tx)
	_ = tx.Rollback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.QueryRow(ctx, "SELECT target_shape,capture_hash FROM v3_migration_online.run WHERE singleton").Scan(&next.ExpectedTargetShape, &next.ExpectedCaptureHash); err != nil {
		t.Fatal(err)
	}
	return next
}

func retainedCopiedFixture(t *testing.T, m *Importer, opts OnlineOptions) {
	t.Helper()
	ctx := context.Background()
	if _, err := m.PrepareOnline(ctx, opts, true); err != nil {
		t.Fatal(err)
	}
	if r, err := m.CopyOnline(ctx, opts); err != nil || r.Phase != "copied" {
		t.Fatalf("original copy %+v %v", r, err)
	}
	onlineMigrationSync(t, m, opts)
}

func retainedRowXmin(t *testing.T, target *pgxpool.Pool, id int64) string {
	t.Helper()
	var xmin string
	if err := target.QueryRow(context.Background(), "SELECT xmin::text FROM "+onlineStage("v3_audit.events")+" WHERE id=$1", id).Scan(&xmin); err != nil {
		t.Fatal(err)
	}
	return xmin
}

func TestOnlineRetainedPageAuthorizationLocksAndRejections(t *testing.T) {
	m, _, target, opts := onlineMigrationFixture(t, false)
	retainedCopiedFixture(t, m, opts)
	ctx := context.Background()
	p := &onlineProjector{}
	var before, after string
	state := `SELECT md5(jsonb_agg(to_jsonb(p) ORDER BY name)::text) FROM v3_migration_online.progress p`
	if err := target.QueryRow(ctx, state).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"wrong_owner", "finalized", "missing_progress", "bad_cursor", "complete"} {
		t.Run(mode, func(t *testing.T) {
			tx, err := target.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			runID, spec := opts.RunID, onlineSpec{name: "logs"}
			switch mode {
			case "wrong_owner":
				runID = "different-retained-owner"
			case "finalized":
				if err := onlineAuthorize(ctx, tx, opts.RunID); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(ctx, "UPDATE v3_migration_online.run SET phase='finalized' WHERE singleton"); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(ctx, "SELECT set_config('codego.online_writer','',true)"); err != nil {
					t.Fatal(err)
				}
			case "missing_progress":
				spec.name = "missing-retained-progress"
			case "bad_cursor":
				if err := onlineAuthorize(ctx, tx, opts.RunID); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(ctx, `UPDATE v3_migration_online.progress SET complete=false,cursor='{"version":999}' WHERE name='logs'`); err != nil {
					t.Fatal(err)
				}
			}
			// These cases must return before reading or repairing source rows.
			_, complete, err := p.reconcilePage(ctx, nil, tx, runID, spec)
			if mode == "complete" {
				if err != nil || !complete {
					t.Fatalf("authorized completed page: complete=%v err=%v", complete, err)
				}
				var writer string
				if err := tx.QueryRow(ctx, "SELECT current_setting('codego.online_writer',true)").Scan(&writer); err != nil || writer != opts.RunID {
					t.Fatalf("authorized writer setting %q: %v", writer, err)
				}
				for _, query := range []string{
					"SELECT run_id FROM v3_migration_online.run WHERE singleton FOR UPDATE NOWAIT",
					"SELECT name FROM v3_migration_online.progress WHERE name='logs' FOR UPDATE NOWAIT",
				} {
					other, err := target.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					_, err = other.Exec(ctx, query)
					_ = other.Rollback(ctx)
					if err == nil || !strings.Contains(err.Error(), "55P03") {
						t.Fatalf("page did not retain ownership/progress lock: %v", err)
					}
				}
			} else {
				if err == nil || complete {
					t.Fatalf("unsafe page accepted: complete=%v err=%v", complete, err)
				}
				if mode == "missing_progress" && !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("missing progress error changed: %v", err)
				}
				if mode == "wrong_owner" || mode == "finalized" {
					var writer string
					if !strings.Contains(err.Error(), "ownership or phase changed") {
						t.Fatalf("ownership rejection changed: %v", err)
					}
					if err := tx.QueryRow(ctx, "SELECT coalesce(current_setting('codego.online_writer',true),'')").Scan(&writer); err != nil || writer != "" {
						t.Fatalf("refused page enabled writer %q: %v", writer, err)
					}
				}
			}
		})
	}
	if err := target.QueryRow(ctx, state).Scan(&after); err != nil || after != before {
		t.Fatalf("rejected/rolled-back pages changed progress: %v", err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	t.Log("wrong owner/finalized never enabled writer; missing progress and bad cursor rejected; both row locks held and rolled-back progress unchanged")
}

func TestOnlineRetainedReconciliationRepairsMissedInsertUpdateDeleteAndParents(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineRecoveryExec(t, source, `INSERT INTO gateway.request_attempt_audits SELECT 'late-orphan','missing-parent',attempt_no,retry_index,channel_id,model_name,fault_domain,request_type,status,success,status_code,failure_class,stage,started_at,completed_at,duration_ms,created_at FROM gateway.request_attempt_audits`)
	retainedCopiedFixture(t, m, opts)
	unchangedXmin := retainedRowXmin(t, target, 53)
	capture := retainedPauseFixture(t, source, opts)
	var eventsBefore, eventsAfter int64
	onlineRecoveryCount := func() int64 {
		var count int64
		if err := source.QueryRow(ctx, "SELECT count(*) FROM v3_migration_capture.events").Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	eventsBefore = onlineRecoveryCount()
	onlineRecoveryExec(t, source, `UPDATE migration_source.logs SET quota=55 WHERE id=51;
	 DELETE FROM migration_source.logs WHERE id=54;
	 INSERT INTO migration_source.logs SELECT 55,user_id,created_at+1,type,content,username,token_name,model_name,7,prompt_tokens,completion_tokens,use_time,is_stream,channel_id,token_id,"group",ip,'',upstream_request_id,other FROM migration_source.logs WHERE id=51;
	 INSERT INTO gateway.request_audits SELECT 'missing-parent',trace_id,user_id,token_id,model_name,group_name,protocol,request_type,status,counted_in_success_rate,billable,quota,prompt_tokens,completion_tokens,final_channel_id,attempts_count,retry_count,status_code,error_code,started_at,completed_at,created_at,updated_at FROM gateway.request_audits WHERE request_id='kept-request';
	 DELETE FROM gateway.request_audits WHERE request_id='kept-request';
	 UPDATE billing.ledger_entries SET amount=25 WHERE entry_id='credit-original';
	 DELETE FROM billing.ledger_entries WHERE entry_id='retired-points-entry';
	 UPDATE billing.ledger_entries SET amount=9223372036854775806 WHERE entry_id='retired-gpt-entry'`)
	eventsAfter = onlineRecoveryCount()
	if eventsBefore != eventsAfter {
		t.Fatal("gap fixture accidentally captured the missed changes")
	}
	if _, err := m.CopyOnline(ctx, opts); err == nil {
		t.Fatal("paused capture accepted as ordinary continuation")
	}
	next := retainedNewEpochFixture(t, source, target, opts, capture)
	if r, err := m.PrepareRetainedOnline(ctx, next); err != nil || !r.Applied {
		t.Fatalf("retained prepare %+v %v", r, err)
	}
	if _, err := m.SyncOnline(ctx, next.OnlineOptions); err == nil {
		t.Fatal("sync acknowledged new epoch before gap reconciliation")
	}
	if _, err := m.VerifyOnline(ctx, next.OnlineOptions); err == nil {
		t.Fatal("incomplete reconciliation verified")
	}
	r, err := m.ReconcileRetainedOnline(ctx, next.OnlineOptions)
	if err != nil || !r.Applied || r.Phase != "copied" || r.Repaired < 4 || r.Pruned < 3 || r.Unchanged < 2 {
		t.Fatalf("retained repair %+v %v", r, err)
	}
	if retainedRowXmin(t, target, 53) != unchangedXmin {
		t.Fatal("identical event was rewritten")
	}
	var amount int64
	if err := target.QueryRow(ctx, "SELECT amount FROM "+onlineStage("v3_audit.events")+" WHERE id=51").Scan(&amount); err != nil || amount != 110 {
		t.Fatalf("repaired amount=%d err=%v", amount, err)
	}
	onlineRecoveryStageUsage(t, target, 51, "kept-request", 1700000000)
	var pruned, reclassified int64
	if err := target.QueryRow(ctx, "SELECT count(*) FROM "+onlineStage("v3_audit.events")+" WHERE id=54").Scan(&pruned); err != nil || pruned != 0 {
		t.Fatal("missed deletion survived")
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM "+onlineStage("v3_audit.request_attempt_audits")+" WHERE attempt_id='late-orphan'").Scan(&reclassified); err != nil || reclassified != 1 {
		t.Fatal("parent insertion did not reclassify existing orphan")
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM "+onlineStage("v3_audit.orphan_request_attempt_history")+" WHERE attempt_id='kept-attempt'").Scan(&reclassified); err != nil || reclassified != 1 {
		t.Fatal("parent deletion did not reclassify existing attempt")
	}
	onlineMigrationSync(t, m, next.OnlineOptions)
	if _, err := m.VerifyOnline(ctx, next.OnlineOptions); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	var previousID string
	if err := target.QueryRow(ctx, "SELECT cursor->'run'->>'run_id' FROM v3_migration_online.progress WHERE name=$1", "recovery_previous:"+opts.RunID).Scan(&previousID); err != nil || previousID != opts.RunID {
		t.Fatal("previous run receipt lost")
	}
	if _, err := SealOnlineCapture(ctx, source, next.RunID); err != nil {
		t.Fatal(err)
	}
	if report, err := m.FinalizeOnline(ctx, next.OnlineOptions); err != nil || !report.Applied || len(report.Issues) != 0 {
		t.Fatalf("isolated finalization %+v %v", report, err)
	}
	if report, err := m.Check(ctx); err != nil || len(report.Issues) != 0 {
		t.Fatalf("independent full check %+v %v", report, err)
	}
	var balance, openings int64
	if err := target.QueryRow(ctx, "SELECT balance,(SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='opening') FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet'").Scan(&balance, &openings); err != nil || balance != 1000 || openings != 1 {
		t.Fatalf("opening changed balance=%d entries=%d err=%v", balance, openings, err)
	}
	t.Logf("uncaptured gap repaired: scanned=%d unchanged=%d repaired=%d pruned=%d; parent reclassification, duplicate normalization, previous receipt, unchanged xmin and exactly one 1000-unit opening passed", r.Scanned, r.Unchanged, r.Repaired, r.Pruned)
}

func TestOnlineRetainedReconciliationRetiredFundingReceiptDeletion(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, true)
	ctx := context.Background()
	retainedCopiedFixture(t, m, opts)
	capture := retainedPauseFixture(t, source, opts)
	onlineRecoveryExec(t, source, "UPDATE billing.funding_lots SET remaining_amount=9223372036854775806 WHERE lot_id='funding-retired-lot'; DELETE FROM billing.funding_allocations WHERE allocation_id='funding-retired-allocation'")
	next := retainedNewEpochFixture(t, source, target, opts, capture)
	if _, err := m.PrepareRetainedOnline(ctx, next); err != nil {
		t.Fatal(err)
	}
	if r, err := m.ReconcileRetainedOnline(ctx, next.OnlineOptions); err != nil || r.Repaired != 1 || r.Pruned != 1 {
		t.Fatalf("retired funding repair %+v %v", r, err)
	}
	onlineMigrationSync(t, m, next.OnlineOptions)
	if _, err := m.VerifyOnline(ctx, next.OnlineOptions); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	t.Log("retired funding changed and deleted receipts independently verified without posting money")
}

func TestOnlineRetainedReconciliationLateBadPageRollbackAndResume(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineRecoveryExec(t, source, `INSERT INTO migration_source.logs SELECT 10000+g,7,1700010000+g,2,'recovery','alice','key','chat-model',1,1,1,0,false,13,11,'default','','recovery-'||g,'','{}' FROM generate_series(1,8192) g`)
	retainedCopiedFixture(t, m, opts)
	untouched := retainedRowXmin(t, target, 10002)
	capture := retainedPauseFixture(t, source, opts)
	onlineRecoveryExec(t, source, "UPDATE migration_source.logs SET quota=55 WHERE id=10001; UPDATE migration_source.logs SET quota=-1 WHERE id=14200")
	next := retainedNewEpochFixture(t, source, target, opts, capture)
	if _, err := m.PrepareRetainedOnline(ctx, next); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReconcileRetainedOnline(ctx, next.OnlineOptions); err == nil {
		t.Fatal("negative second-page amount accepted")
	}
	var raw []byte
	if err := target.QueryRow(ctx, "SELECT cursor FROM v3_migration_online.progress WHERE name='logs'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var cursor onlineReconcileCursor
	if err := json.Unmarshal(raw, &cursor); err != nil || cursor.Scanned != 4096 || cursor.Repaired != 1 || cursor.Unchanged != 4095 {
		t.Fatalf("failed page committed progress %s %v", raw, err)
	}
	if _, err := m.VerifyOnline(ctx, next.OnlineOptions); err == nil {
		t.Fatal("failed repair accepted as verified")
	}
	onlineRecoveryAssertNoMoney(t, target)
	onlineRecoveryExec(t, source, "UPDATE migration_source.logs SET quota=1 WHERE id=14200; UPDATE migration_source.logs SET quota=57 WHERE id=10001")
	if r, err := m.ReconcileRetainedOnline(ctx, next.OnlineOptions); err != nil || !r.Applied || r.Scanned != 4102 {
		t.Fatalf("repair resume %+v %v", r, err)
	}
	onlineMigrationSync(t, m, next.OnlineOptions)
	if _, err := m.VerifyOnline(ctx, next.OnlineOptions); err != nil {
		t.Fatal(err)
	}
	var amount int64
	if err := target.QueryRow(ctx, "SELECT amount FROM "+onlineStage("v3_audit.events")+" WHERE id=10001").Scan(&amount); err != nil || amount != 114 {
		t.Fatalf("below-cursor new epoch change lost: amount=%d %v", amount, err)
	}
	if retainedRowXmin(t, target, 10002) != untouched {
		t.Fatal("unchanged row rewritten during repair or resume")
	}
	t.Log("4096-row checkpoint retained; invalid next page rolled back; resumed scan and below-cursor new capture replay passed")
}

func TestOnlineRetainedReconciliationReviewedContractRejectsDrift(t *testing.T) {
	for _, failure := range []string{"wrong_target", "wrong_source", "wrong_capture", "wrong_journal", "active_old_schema", "same_run", "native_user", "changed_mapping"} {
		t.Run(failure, func(t *testing.T) {
			m, source, target, opts := onlineMigrationFixture(t, false)
			ctx := context.Background()
			retainedCopiedFixture(t, m, opts)
			capture := retainedPauseFixture(t, source, opts)
			next := retainedNewEpochFixture(t, source, target, opts, capture)
			switch failure {
			case "wrong_target":
				next.ExpectedTargetShape = strings.Repeat("0", 64)
			case "wrong_source":
				next.ExpectedSourceIdentity = strings.Repeat("0", 64)
			case "wrong_capture":
				next.ExpectedCaptureHash = strings.Repeat("0", 64)
			case "wrong_journal":
				next.ExpectedPreviousCaptureEventsOID++
			case "active_old_schema":
				next.PreviousCaptureSchema = onlineCaptureSchema
			case "same_run":
				next.RunID = opts.RunID
			case "native_user":
				onlineRecoveryExec(t, target, "INSERT INTO v3_identity.users(id,username,email,role,status,group_name,settings)VALUES(7,'already-native','native@example.invalid','user','active','default','{}')")
			case "changed_mapping":
				onlineRecoveryExec(t, source, "UPDATE billing.accounts SET account_type='point' WHERE account_id='wallet-7'")
			}
			if _, err := m.PrepareRetainedOnline(ctx, next); err == nil {
				t.Fatal("unsafe retained target was adopted")
			}
			var runID, phase string
			if err := target.QueryRow(ctx, "SELECT run_id,phase FROM v3_migration_online.run WHERE singleton").Scan(&runID, &phase); err != nil || runID != opts.RunID || phase != "copied" {
				t.Fatal("refused preparation changed target ownership")
			}
			onlineRecoveryAssertNoMoney(t, target)
		})
	}
}

func TestOnlineRetainedComparisonIsReadOnlyAndPruningPagesAreBounded(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineRecoveryExec(t, source, `INSERT INTO migration_source.logs SELECT 10000+g,7,1700010000+g,2,'compare','alice','key','chat-model',1,1,1,0,false,13,11,'default','','compare-'||g,'','{}' FROM generate_series(1,2049) g`)
	retainedCopiedFixture(t, m, opts)
	read, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(ctx) }()
	write, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := onlineAuthorize(ctx, write, opts.RunID); err != nil {
		t.Fatal(err)
	}
	sources, specs, err := onlineBindings(ctx, read, write, opts.RunID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := loadOnlineProjector(ctx, read, write, sources)
	_ = write.Rollback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ro, err := target.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ro.Rollback(ctx) }()
	p.target = ro
	for _, spec := range specs {
		if spec.name != "logs" {
			continue
		}
		inputs, err := onlineReadBatch(ctx, read, spec, nil)
		if err != nil {
			t.Fatal(err)
		}
		matches, err := p.matchRetainedInputs(ctx, spec, inputs)
		if err != nil || len(matches) != 2053 {
			t.Fatalf("read-only exact comparison %d %v", len(matches), err)
		}
		for _, match := range matches {
			if !match {
				t.Fatal("identical projection classified dirty")
			}
		}
		var cursor json.RawMessage
		scanned, pages := 0, 0
		for {
			keys, err := reconcileTargetKeys(ctx, ro, spec, spec.targets[0], cursor)
			if err != nil || len(keys) > 512 {
				t.Fatalf("pruning page %d %v", len(keys), err)
			}
			if len(keys) == 0 {
				break
			}
			scanned += len(keys)
			pages++
			cursor = keys[len(keys)-1]
		}
		if scanned != 2053 || pages != 5 {
			t.Fatalf("target key traversal lost rows: scanned=%d pages=%d", scanned, pages)
		}
	}
	t.Log("typed source projection compared in READ ONLY transaction; 2053 target keys traversed in five <=512-row pages")
}

func TestOnlineRetainedCachedLogComparisonDetectsMoneyMetadataAndEmptyRequestChanges(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineRecoveryExec(t, source, `INSERT INTO migration_source.logs
	 SELECT 10000+g,7,1700010000+g,2,'compare','alice','key','chat-model',1,1,1,0,false,13,11,'default','',
	 CASE WHEN g%2=0 THEN '' ELSE 'compare-'||g END,'',
	 jsonb_build_object('precise',9007199254740993::bigint,'payload',repeat('w',8192),'cached_tokens',23)::text
	 FROM generate_series(1,513) g`)
	retainedCopiedFixture(t, m, opts)
	read, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(ctx) }()
	write, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = write.Rollback(ctx) }()
	if err := onlineAuthorize(ctx, write, opts.RunID); err != nil {
		t.Fatal(err)
	}
	sources, specs, err := onlineBindings(ctx, read, write, opts.RunID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := loadOnlineProjector(ctx, read, write, sources)
	if err != nil {
		t.Fatal(err)
	}
	var logs onlineSpec
	for _, spec := range specs {
		if spec.name == "logs" {
			logs = spec
		}
	}
	// Each corruption must be found independently, including the empty source
	// request ID that must remain part of the exact usage comparison.
	for _, tc := range []struct{ label, sql string }{
		{"money", "UPDATE " + onlineStage("v3_billing.usage_logs") + " SET amount=amount+1 WHERE id=10002"},
		{"empty_request", "UPDATE " + onlineStage("v3_billing.usage_logs") + " SET request_id='wrong-empty-request' WHERE id=10002"},
		{"precise_metadata", "UPDATE " + onlineStage("v3_audit.events") + ` SET metadata=jsonb_set(metadata,'{precise}','9007199254740992') WHERE id=10002`},
	} {
		if _, err := write.Exec(ctx, "SAVEPOINT corruption"); err != nil {
			t.Fatal(err)
		}
		if _, err := write.Exec(ctx, tc.sql); err != nil {
			t.Fatal(err)
		}
		var cursor json.RawMessage
		dirty, checked := 0, 0
		for {
			inputs, err := onlineReadBatch(ctx, read, logs, cursor)
			if err != nil {
				t.Fatal(err)
			}
			if len(inputs) == 0 {
				break
			}
			matches, err := p.matchRetainedInputs(ctx, logs, inputs)
			if err != nil || len(matches) != len(inputs) {
				t.Fatalf("%s comparison: %v", tc.label, err)
			}
			for _, match := range matches {
				if !match {
					dirty++
				}
			}
			checked += len(inputs)
			cursor = inputs[len(inputs)-1].key
		}
		if dirty != 1 || checked != 517 {
			t.Fatalf("%s checked=%d dirty=%d", tc.label, checked, dirty)
		}
		if _, err := write.Exec(ctx, "ROLLBACK TO SAVEPOINT corruption"); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("wide mixed empty/nonempty request IDs compared across pages; one-unit money and >2^53 metadata changes each detected exactly once")
}
