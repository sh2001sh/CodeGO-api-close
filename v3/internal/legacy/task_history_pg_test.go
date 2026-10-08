//go:build pgintegration

package legacy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/workflow"
)

func seedTaskHistory(t *testing.T, source *pgxpool.Pool) {
	t.Helper()
	_, err := source.Exec(context.Background(), `CREATE TABLE migration_source.tasks(
	 id bigint PRIMARY KEY,task_id text,user_id bigint,channel_id bigint,quota bigint,status text,
	 platform text,"group" text,action text,progress text,properties jsonb,private_data jsonb,data jsonb,
	 fail_reason text,created_at bigint,updated_at bigint);
	 INSERT INTO migration_source.tasks VALUES
	 (99,'historical-video',7,13,5,'SUCCESS','openai_video','default','generate','100%',
	 '{"origin_model_name":"video-model","upstream_model_name":"upstream-video"}',
	 '{"key":"MUST-NOT-MIGRATE","upstream_task_id":"upstream-99","result_url":"https://example.invalid/video.mp4"}',
	 '{"id":"upstream-99","url":"https://example.invalid/video.mp4"}','',1700000000,1700000001),
	 (100,'historical-music',7,13,0,'FAILURE','suno','default','MUSIC','100%',
	 '{"origin_model_name":"suno_music"}','{"key":"MUST-NOT-MIGRATE"}','{}','provider failed',1700000000,1700000001);
	 CREATE SCHEMA workflow;
	 CREATE TABLE workflow.task_workflows(workflow_id text,public_task_id text,status text,terminal_state text,result_meta jsonb);
	 INSERT INTO workflow.task_workflows VALUES('wf-99','historical-video','succeeded','succeeded','{"url":"https://example.invalid/video.mp4"}');
	 CREATE TABLE workflow.task_snapshots(snapshot_id text,workflow_id text,provider_state text,raw_payload jsonb);
	 INSERT INTO workflow.task_snapshots VALUES('snap-99','wf-99','SUCCESS','{"progress":100}');
	 CREATE TABLE workflow.task_terminal_results(terminal_result_id text,workflow_id text,terminal_state text,settlement_status text,result_meta jsonb);
	 INSERT INTO workflow.task_terminal_results VALUES('result-99','wf-99','succeeded','settled','{"url":"https://example.invalid/video.mp4"}');`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOfflineTaskHistoryQueryIdempotencyAndReconciliation(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedTaskHistory(t, source)
	ctx := context.Background()
	m := NewImporter(readonlySource(t, source), target, crypto)
	report, err := m.Import(ctx, false)
	if err != nil || len(report.Issues) != 0 || report.Counts["history.tasks"] != 2 {
		t.Fatalf("preview=%+v err=%v", report, err)
	}
	for i := 0; i < 2; i++ {
		if report, err = m.Import(ctx, true); err != nil || !report.Applied {
			t.Fatalf("import %d=%+v err=%v", i, report, err)
		}
	}
	repo := &workflow.PostgresRepository{Pool: target}
	task, err := repo.GetOwned(ctx, "historical-video", 7)
	if err != nil || !task.Historical || task.ID != "historical-video" || task.Status != "completed" || task.Model != "video-model" || task.URL != "https://example.invalid/video.mp4" || task.ActualCredits != 10 || task.KeyID != 0 || task.CredentialID != 0 || task.CostState != "settled" {
		t.Fatalf("historical task=%+v err=%v", task, err)
	}
	if _, err = repo.GetOwned(ctx, "historical-video", 8); !errors.Is(err, workflow.ErrNotFound) {
		t.Fatalf("foreign task owner allowed: %v", err)
	}
	if task, err = repo.GetOwned(ctx, "historical-music", 7); err != nil || task.Status != "failed" || task.CostState != "refunded" || task.Error != "provider failed" {
		t.Fatalf("failed task=%+v err=%v", task, err)
	}
	pending, err := repo.Pending(ctx, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("historical task became runnable: %+v %v", pending, err)
	}
	var history string
	var historyCount, nativeCount, ledgerCount int
	var balance int64
	if err = target.QueryRow(ctx, `SELECT count(*),string_agg(to_jsonb(t)::text,'') FROM v3_workflow.legacy_tasks t`).Scan(&historyCount, &history); err != nil {
		t.Fatal(err)
	}
	if historyCount != 2 || strings.Contains(history, "MUST-NOT-MIGRATE") || !strings.Contains(history, "snap-99") || !strings.Contains(history, "result-99") {
		t.Fatal("history missing workflow evidence, duplicated or exposed a credential")
	}
	if err = target.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_workflow.tasks),
	 (SELECT count(*) FROM v3_billing.ledger_entries),balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet'`).Scan(&nativeCount, &ledgerCount, &balance); err != nil || nativeCount != 0 || ledgerCount != 1 || balance != 1000 {
		t.Fatalf("history created financial/runtime work: %d %d %d %v", nativeCount, ledgerCount, balance, err)
	}
	if report, err = m.Check(ctx); err != nil || len(report.Issues) != 0 || report.Counts["check:history:tasks:matched"] != 2 {
		t.Fatalf("check=%+v err=%v", report, err)
	}
	if _, err = target.Exec(ctx, `UPDATE v3_workflow.legacy_tasks SET result_url='https://example.invalid/changed' WHERE id='historical-video'`); err != nil {
		t.Fatal(err)
	}
	if report, err = m.Check(ctx); err == nil || len(report.Issues) == 0 {
		t.Fatal("changed task result passed reconciliation")
	}
	if _, err = m.Import(ctx, true); err == nil {
		t.Fatal("reimport silently overwrote changed task history")
	}
}

func TestOfflineTaskHistoryBlocksPendingAndUnsettledWork(t *testing.T) {
	for _, mutation := range []string{
		`UPDATE migration_source.tasks SET status='IN_PROGRESS' WHERE id=99`,
		`UPDATE workflow.task_workflows SET status='running' WHERE workflow_id='wf-99'`,
		`UPDATE workflow.task_terminal_results SET settlement_status='pending' WHERE workflow_id='wf-99'`,
		`DELETE FROM workflow.task_terminal_results`,
		`UPDATE migration_source.tasks SET status='FAILURE' WHERE id=99`,
		`UPDATE migration_source.tasks SET user_id=999 WHERE id=99`,
		`UPDATE migration_source.tasks SET task_id='historical-music' WHERE id=99`,
		`UPDATE workflow.task_snapshots SET snapshot_id=''`,
	} {
		t.Run(mutation, func(t *testing.T) {
			source, target, crypto := importTestDB(t)
			seedTaskHistory(t, source)
			ctx := context.Background()
			if _, err := source.Exec(ctx, mutation); err != nil {
				t.Fatal(err)
			}
			m := NewImporter(readonlySource(t, source), target, crypto)
			r, err := m.Import(ctx, false)
			if err != nil || len(r.Issues) == 0 {
				t.Fatalf("unsafe source preview=%+v err=%v", r, err)
			}
			if r, err = m.Import(ctx, true); err == nil || r.Applied {
				t.Fatal("unsafe async work did not block application")
			}
			var users int
			if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users`).Scan(&users); err != nil || users != 0 {
				t.Fatalf("rejected import wrote target: %d %v", users, err)
			}
		})
	}
}

func TestOfflineSourceCoverageBlocksUnknownPopulatedTable(t *testing.T) {
	source, target, crypto := importTestDB(t)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `CREATE TABLE migration_source.unmapped_future_work(id bigint);
	 CREATE TABLE migration_source.miniprogram_bind_codes(id bigint);
	 INSERT INTO migration_source.miniprogram_bind_codes VALUES(1);
	 CREATE TABLE migration_source.perf_metrics(id bigint);
	 INSERT INTO migration_source.perf_metrics VALUES(1)`); err != nil {
		t.Fatal(err)
	}
	m := NewImporter(readonlySource(t, source), target, crypto)
	r, err := m.Import(ctx, false)
	if err != nil || len(r.UnmappedSources) != 0 || len(r.Issues) != 0 {
		t.Fatalf("empty future table blocked: %+v %v", r, err)
	}
	if r.Counts["retired_features.miniprogram_bind_codes"] != 1 || r.Counts["rebuilt_read_projections.perf_metrics"] != 1 {
		t.Fatal("explicit retirement/read-projection exclusions were not reported")
	}
	if _, err = source.Exec(ctx, `INSERT INTO migration_source.unmapped_future_work VALUES(1)`); err != nil {
		t.Fatal(err)
	}
	r, err = m.Import(ctx, false)
	if err != nil || len(r.UnmappedSources) != 1 || len(r.Issues) != 1 || r.Issues[0].Code != "unmapped_source" {
		t.Fatalf("unknown populated table not reported: %+v %v", r, err)
	}
	if r, err = m.Import(ctx, true); err == nil || r.Applied {
		t.Fatal("unknown populated table silently applied")
	}
}
