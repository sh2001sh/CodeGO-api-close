//go:build pgintegration

package main

import (
	"context"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
	"github.com/sh2001sh/new-api/v3/internal/workflow"
)

func TestWorkerNativeBackgroundAndWorkflowProduceTerminalMetadata(t *testing.T) {
	f := newSummaryWorkerFixture(t)
	ctx := context.Background()
	jobs, err := live.NewRedisBackgroundRepository(f.deps.Redis, "", f.deps.Crypto.DeriveKey("background-jobs"))
	if err != nil {
		t.Fatal(err)
	}
	req := &gateway.Request{ID: "resp_bg_worker_fixture", Received: time.Now().UTC(), Model: "fixture-model", Protocol: gateway.ProtocolResponses,
		Body: []byte(`{"model":"fixture-model","input":"private-prompt"}`), Principal: gateway.Principal{UserID: 1, KeyID: 1, Group: "default"}, Targets: []gateway.Target{f.target}}
	hold, err := billing.NewBackgroundSettler(f.billing, jobs).Reserve(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	job := live.BackgroundJob{ID: req.ID, UserID: 1, KeyID: 1, Group: "default", TargetGroup: "default", Model: req.Model, ChannelID: 1, CredentialID: 1,
		Body: req.Body, Reservation: hold, Status: "queued", Native: true, ClientIP: "127.0.0.1", LastUpstreamSequence: -1, CreatedAt: req.Received, UpdatedAt: req.Received}
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	assertWorkerAuditCount(t, f, 0)
	if err := f.native.live.Reconcile(ctx, 10); err != nil {
		t.Fatal(err)
	}
	stored, err := jobs.GetOwned(ctx, job.ID, 1, 1)
	if err != nil || stored.Status != "completed" || !stored.Billed {
		t.Fatalf("worker background result: %+v %v", stored, err)
	}
	waitWorkerAudit(t, f, 1)
	var status, group, protocol string
	var amount, prompt, completion, attempts int64
	if err := f.deps.PG.QueryRow(ctx, `SELECT status,group_name,protocol,amount,prompt_tokens,completion_tokens,attempts_count FROM v3_audit.request_audits WHERE request_id=$1`, job.ID).
		Scan(&status, &group, &protocol, &amount, &prompt, &completion, &attempts); err != nil || status != "success" || group != "default" || protocol != "responses" || amount != 25 || prompt != 10 || completion != 5 || attempts != 1 {
		t.Fatalf("native background metadata: status=%s group=%s protocol=%s amount=%d prompt=%d output=%d attempts=%d %v", status, group, protocol, amount, prompt, completion, attempts, err)
	}

	for _, id := range []string{"video-success", "video-failed"} {
		target, err := f.deps.ResolveTarget(ctx, 2, 2)
		if err != nil {
			t.Fatal(err)
		}
		target.Group = "default"
		target.MultiplierPPM = 1_000_000
		request := &gateway.Request{ID: "task_worker_" + id, Received: time.Now().UTC(), Model: "fixture-model", Body: []byte(`{"model":"fixture-model","prompt":"private-video-prompt"}`),
			Principal: gateway.Principal{UserID: 1, KeyID: 1, Group: "default"}, Targets: []gateway.Target{target}}
		reserve, err := billing.NewWorkflowSettler(f.billing).Reserve(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		task := workflow.Task{ID: request.ID, UserID: 1, KeyID: 1, Group: "default", TargetGroup: "default", Provider: "openai_video", ChannelID: 2, CredentialID: 2,
			Model: request.Model, UpstreamModel: request.Model, UpstreamID: id, Action: "generate", Status: "submitting", Body: request.Body, Reservation: reserve, CostState: "reserved", CreatedAt: request.Received, UpdatedAt: request.Received}
		repo := &workflow.PostgresRepository{Pool: f.deps.PG.Pool}
		if err := repo.Create(ctx, task); err != nil {
			t.Fatal(err)
		}
		task.Status = "queued"
		if err := repo.Save(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	assertWorkerAuditCount(t, f, 1)
	if n, err := f.native.workflow.Reconcile(ctx, 10); err != nil || n != 2 {
		t.Fatal("worker workflow reconcile", n, err)
	}
	waitWorkerAudit(t, f, 3)
	for _, tc := range []struct {
		id, status string
		amount     int64
		billable   bool
	}{{"video-success", "success", 25, true}, {"video-failed", "failed", 0, false}} {
		var billable bool
		if err := f.deps.PG.QueryRow(ctx, `SELECT status,amount,billable FROM v3_audit.request_audits WHERE request_id=$1`, "task_worker_"+tc.id).Scan(&status, &amount, &billable); err != nil || status != tc.status || amount != tc.amount || billable != tc.billable {
			t.Fatalf("workflow terminal %s: status=%s amount=%d billable=%v %v", tc.id, status, amount, billable, err)
		}
	}
	if err := f.native.live.Reconcile(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if n, err := f.native.workflow.Reconcile(ctx, 10); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	f.native.requests.Close()
	assertWorkerAuditCount(t, f, 3)
	var privateMetadata bool
	if err := f.deps.PG.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_audit.request_audits a WHERE to_jsonb(a)::text LIKE '%private-%')`).Scan(&privateMetadata); err != nil || privateMetadata {
		t.Fatal("native status captured sensitive payload", err)
	}
	holds, err := f.deps.Redis.HGet(ctx, "v3:bal:{1}", "reserved").Result()
	if err != nil || holds != "0" {
		t.Fatal("financial holds left after terminal metadata", holds, err)
	}
	t.Log("real worker assembler: queued jobs emit0; durable Responses success amount25, workflow success amount25 and finalized failure refund0 produce3 idempotent private-free metadata rows")
}

func assertWorkerAuditCount(t *testing.T, f *summaryWorkerFixture, want int) {
	t.Helper()
	var n int
	if err := f.deps.PG.QueryRow(context.Background(), `SELECT count(*) FROM v3_audit.request_audits`).Scan(&n); err != nil || n != want {
		t.Fatalf("worker audit rows=%d want=%d %v", n, want, err)
	}
}
func waitWorkerAudit(t *testing.T, f *summaryWorkerFixture, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var n int
		if err := f.deps.PG.QueryRow(context.Background(), `SELECT count(*) FROM v3_audit.request_audits`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("async worker audits=%d want=%d", n, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
