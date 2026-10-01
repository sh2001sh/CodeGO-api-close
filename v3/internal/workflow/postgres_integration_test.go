//go:build pgintegration

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/migrations"
)

func TestPostgresOwnershipLeasesAndRestartPersistence(t *testing.T) {
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error(err)
		}
	}()
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT to_regclass('v3_workflow.tasks') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		sql, err := migrations.Read("20260930193000_workflow.sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	repo := &PostgresRepository{Pool: tx}
	now := time.Now().UTC().Truncate(time.Microsecond)
	task := Task{ID: taskID(), UserID: 17, KeyID: 19, Group: "auto", TargetGroup: "selected-group", Provider: "suno", ChannelID: 23,
		CredentialID: 29, Model: "suno_music", UpstreamModel: "suno_music", Action: "music", Status: "submitting",
		CostState: "reserved", Body: []byte(`{"prompt":"song"}`), Reservation: Reservation{Data: json.RawMessage(`{"account":31}`), EstimatedCredits: 73},
		LeaseID: "submit-owner", CreatedAt: now, UpdatedAt: now}
	if err = repo.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.GetOwned(ctx, task.ID, 18); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user access: %v", err)
	}
	if pending, err := repo.Pending(ctx, 10); err != nil {
		t.Fatal(err)
	} else {
		for _, p := range pending {
			if p.ID == task.ID {
				t.Fatal("worker polled a still submitting task")
			}
		}
	}
	task.UpstreamID = "provider-task"
	task.Status = "queued"
	task.Data = json.RawMessage(`{"task_id":"provider-task"}`)
	if err = repo.Save(ctx, task); err != nil {
		t.Fatal(err)
	}
	reopened := &PostgresRepository{Pool: tx}
	loaded, err := reopened.GetOwned(ctx, task.ID, 17)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UpstreamID != "provider-task" || loaded.Reservation.EstimatedCredits != 73 || string(loaded.Reservation.Data) != `{"account": 31}` {
		t.Fatalf("durable task changed after reopen: %+v", loaded)
	}
	if loaded.Group != "auto" || loaded.TargetGroup != "selected-group" || loaded.Request().Targets[0].Group != "selected-group" || loaded.Request().Principal.Group != "auto" {
		t.Fatal("selected routing group changed after database reopen")
	}
	claimed, err := reopened.Claim(ctx, task.ID, "worker-a", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Claim(ctx, task.ID, "worker-b", now.Add(time.Minute)); !errors.Is(err, ErrConflict) {
		t.Fatalf("second worker claim: %v", err)
	}
	stale := claimed
	stale.LeaseID = "worker-b"
	if err = reopened.Save(ctx, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale owner write: %v", err)
	}
	claimed.Status = "completed"
	claimed.CostState = "settled"
	claimed.ActualCredits = 61
	claimed.Units = 5
	if err = reopened.Save(ctx, claimed); err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Claim(ctx, task.ID, "worker-c", now.Add(time.Minute)); !errors.Is(err, ErrConflict) {
		t.Fatalf("settled task reclaimed: %v", err)
	}
	loaded, err = reopened.GetOwned(ctx, task.ID, 17)
	if err != nil || loaded.ActualCredits != 61 || loaded.CostState != "settled" {
		t.Fatalf("settlement persistence: %+v %v", loaded, err)
	}
	for _, field := range []string{"Secret", "BaseURL", "ProxyURL"} {
		if strings.Contains(columns, field) {
			t.Fatalf("credential field persisted: %s", field)
		}
	}
}
