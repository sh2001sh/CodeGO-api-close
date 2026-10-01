//go:build pgintegration

package security

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
)

func testQueueContracts(t *testing.T, g *Guard, pool *pgxpool.Pool) {
	t.Helper()
	t.Run("durable queue rollback replay and no identity lock inversion", func(t *testing.T) {
		ctx := context.Background()
		lock, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = lock.Rollback(ctx) }()
		if _, err = lock.Exec(ctx, `SELECT id FROM v3_identity.users WHERE id=90 FOR UPDATE`); err != nil {
			t.Fatal(err)
		}
		fields := map[string]string{billing.FieldRequestID: "queue-lock-proof", billing.FieldUserID: "90", billing.FieldChannelID: "90", billing.FieldModel: "queue-model", billing.FieldPromptTokens: "100", billing.FieldCachedTokens: "0", billing.FieldEstimated: "0", billing.FieldTerminal: "completed"}
		enqueueCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		tx, err := pool.Begin(enqueueCtx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err = g.RecordUsageTx(enqueueCtx, tx, fields); err != nil {
			t.Fatalf("financial enqueue waited on identity lock: %v", err)
		}
		if err = g.RecordUsageTx(enqueueCtx, tx, fields); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(enqueueCtx); err != nil {
			t.Fatal(err)
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_security.usage_queue WHERE request_id='queue-lock-proof'`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("pending dedup %d %v", count, err)
		}
		if err = lock.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if err = g.processUsage(ctx, 1000); err != nil {
			t.Fatal(err)
		}
		if err = g.processUsage(ctx, 1000); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `SELECT requests FROM v3_security.usage_samples WHERE user_id=90`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("collector replay double counted %d %v", count, err)
		}
		tx, err = pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		fields[billing.FieldRequestID] = "queue-rollback"
		if err = g.RecordUsageTx(ctx, tx, fields); err != nil {
			t.Fatal(err)
		}
		if err = tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_security.usage_queue WHERE request_id='queue-rollback'`).Scan(&count); err != nil || count != 0 {
			t.Fatal("financial rollback retained sample")
		}
		if _, err = pool.Exec(ctx, `INSERT INTO v3_security.usage_queue VALUES('old-backlog',91,90,'queue-model',100,0,$1)`, g.cfg.Now().Unix()-301); err != nil {
			t.Fatal(err)
		}
		if err = g.processUsage(ctx, 1000); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_security.usage_samples WHERE user_id=91`).Scan(&count); err != nil || count != 0 {
			t.Fatal("stale backlog classified as current traffic")
		}
	})
}
