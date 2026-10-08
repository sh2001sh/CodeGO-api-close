//go:build pgintegration

package audit

import (
	"context"
	"testing"
	"time"
)

func TestTerminalRequestRecordIsIdempotentAndRejectsIdentityCollision(t *testing.T) {
	pool := testPool(t)
	s := New(pool, Config{})
	ctx := context.Background()
	now := time.Now().UTC()
	r := RequestRecord{RequestID: "logical-request", Model: "model", Group: "actual-group", Protocol: "openai_chat", RequestType: "stream", Status: "success", Counted: true,
		UserID: 1, KeyID: 2, ChannelID: 3, Attempts: 2, Retries: 1, PromptTokens: 10, CompletionTokens: 5, StartedAt: now, CompletedAt: now.Add(time.Second)}
	for range 3 {
		if err := s.RecordRequest(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	var count, attempts, retries int64
	var group, status string
	if err := pool.QueryRow(ctx, `SELECT count(*),max(attempts_count),max(retry_count),max(group_name),max(status) FROM v3_audit.request_audits`).Scan(&count, &attempts, &retries, &group, &status); err != nil || count != 1 || attempts != 2 || retries != 1 || group != "actual-group" || status != "success" {
		t.Fatalf("terminal retry/replay lost canonical facts: %d %d %d %s %s %v", count, attempts, retries, group, status, err)
	}
	r.UserID = 99
	if err := s.RecordRequest(ctx, r); err == nil {
		t.Fatal("request-ID collision silently replaced another user's record")
	}
	if err := pool.QueryRow(ctx, `SELECT user_id FROM v3_audit.request_audits WHERE request_id=$1`, r.RequestID).Scan(&count); err != nil || count != 1 {
		t.Fatal("collision changed canonical owner", count, err)
	}
}
