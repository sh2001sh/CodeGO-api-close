//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
	"github.com/tidwall/gjson"
)

func backgroundRedisSnapshot(t *testing.T, client *redis.Client) map[string]string {
	t.Helper()
	ctx := context.Background()
	result := map[string]string{}
	var cursor uint64
	for {
		keys, next, err := client.Scan(ctx, cursor, "*", 1000).Result()
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range keys {
			kind, err := client.Type(ctx, key).Result()
			if err != nil {
				t.Fatal(err)
			}
			if kind != "hash" || !strings.HasPrefix(client.HGet(ctx, key, "id").Val(), "resp_bg_legacy_") {
				continue
			}
			hash, err := client.HGetAll(ctx, key).Result()
			if err != nil {
				t.Fatal(err)
			}
			events, err := client.LRange(ctx, key+":events", 0, -1).Result()
			if err != nil {
				t.Fatal(err)
			}
			// Redis may reorder a hash while incrementally rehashing it during
			// reads. Compare canonical values, retaining encrypted bytes and
			// ordered events, rather than its internal DUMP serialization.
			jobTTL, err := client.PTTL(ctx, key).Result()
			if err != nil {
				t.Fatal(err)
			}
			eventTTL, err := client.PTTL(ctx, key+":events").Result()
			if err != nil || jobTTL != -1 || eventTTL != -1 {
				t.Fatal("imported terminal assets lost source persistence")
			}
			canonical, err := json.Marshal(struct {
				Hash   map[string]string
				Events []string
			}{hash, events})
			if err != nil {
				t.Fatal(err)
			}
			result[key] = string(canonical)
		}
		cursor = next
		if cursor == 0 {
			return result
		}
	}
}

func TestOfflineBackgroundAssetsPreserveNativeHistoryAndNeverBill(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedSourceBackground(t, source)
	repository, client := migrationBackgroundRedis(t, true)
	ctx := context.Background()
	reader := readonlySource(t, source)
	if _, err := reader.Exec(ctx, `UPDATE gateway.responses_background_jobs SET status='queued'`); err == nil {
		t.Fatal("background source reader can write")
	}
	importer := NewImporter(reader, target, crypto)
	before := backgroundRedisSnapshot(t, client)
	address := client.Options().Addr
	t.Setenv("V3_REDIS_ADDR", "")
	preview, err := importer.Import(ctx, false)
	if err != nil || preview.Applied || len(preview.Issues) != 1 || preview.Issues[0].Code != "background_assets_not_migrated" {
		t.Fatalf("database preview failed to require copied history: %+v %v", preview, err)
	}
	t.Setenv("V3_REDIS_ADDR", address)
	report, err := importer.ImportBackground(ctx, nil, false)
	if err != nil || report.Applied || report.Counts["responses_background_jobs"] != 3 || report.Counts["responses_background_events"] != 6 {
		t.Fatalf("source preview counts: %+v %v", report, err)
	}
	if !reflect.DeepEqual(before, backgroundRedisSnapshot(t, client)) {
		t.Fatal("source dry-run changed Redis")
	}
	for range 2 {
		report, err = importer.ImportBackground(ctx, repository, true)
		if err != nil || !report.Applied || report.Counts["check:responses_background_jobs"] != 3 || report.Counts["check:responses_background_events"] != 6 {
			t.Fatalf("terminal assets apply: %+v %v", report, err)
		}
	}
	before = backgroundRedisSnapshot(t, client)
	if report, err = importer.CheckBackground(ctx, repository); err != nil || report.Applied || report.Counts["check:responses_background_jobs"] != 3 {
		t.Fatalf("background read-only check: %+v %v", report, err)
	}
	if _, err = importer.ImportBackground(ctx, repository, true); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, backgroundRedisSnapshot(t, client)) {
		t.Fatal("check or repeated import rewrote settled assets")
	}
	for _, status := range []string{"completed", "failed", "cancelled"} {
		id := "resp_bg_legacy_" + status
		job, err := repository.GetOwned(ctx, id, 7, 11)
		if err != nil || job.ID != id || job.Status != status || !job.Billed || job.CredentialID != 0 || job.Group != "default" || job.Model != "chat-model" ||
			job.UpstreamID != "resp_original_"+status || job.LastUpstreamSequence != 9 || job.UpdatedAt.Nanosecond() != 123456000 {
			t.Fatalf("native history metadata: %v", err)
		}
		if !strings.Contains(string(job.Snapshot), "PRIVATE_RESULT") || (status == "failed" && gjson.GetBytes(job.Snapshot, "error.message").Str != "ORIGINAL_PRIVATE_ERROR") {
			t.Fatal("native result or original error changed")
		}
		for _, owner := range [][2]int64{{8, 11}, {7, 12}} {
			if _, err = repository.GetOwned(ctx, id, owner[0], owner[1]); !errors.Is(err, live.ErrNotFound) {
				t.Fatal("foreign user or API key can access imported response")
			}
		}
		events, err := repository.Events(ctx, id, 0, 10)
		if err != nil || len(events) != 1 || events[0].Sequence != 1 || events[0].Type != "response."+status || gjson.GetBytes(events[0].Payload, "response.id").Str != id {
			t.Fatal("native original resume cursor or event ID changed")
		}
		if _, err = repository.Cancel(ctx, id, 7, 11); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(before, backgroundRedisSnapshot(t, client)) {
		t.Fatal("terminal native cancellation wrote or rescheduled assets")
	}
	verifyImportedBackgroundHTTP(t, repository)
	if pending, err := repository.Pending(ctx, 32); err != nil || len(pending) != 0 {
		t.Fatal("terminal history entered pending upstream execution")
	}
	var entries, holds int64
	if err = target.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_billing.ledger_entries),(SELECT count(*) FROM v3_billing.reservations)`).Scan(&entries, &holds); err != nil || entries != 0 || holds != 0 {
		t.Fatal("asset copy created a financial posting or hold")
	}
	// The hook has the same source-only transaction used by main Import/Check.
	tx, err := reader.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	checked := Report{Counts: map[string]int64{}, Issues: []Issue{}}
	if err = validateBackgroundCoverage(ctx, tx, sources, &checked); err != nil || len(checked.Issues) != 0 || checked.Counts["check:responses_background_jobs"] != 3 {
		t.Fatalf("database import coverage: %+v %v", checked, err)
	}
	if !reflect.DeepEqual(before, backgroundRedisSnapshot(t, client)) {
		t.Fatal("database coverage check changed assets")
	}
	for range 2 {
		if report, err = importer.Import(ctx, true); err != nil || !report.Applied {
			t.Fatalf("database import rejected copied history: %+v %v", report, err)
		}
	}
	if report, err = importer.Check(ctx); err != nil || len(report.Issues) != 0 || report.Counts["check:responses_background_jobs"] != 3 {
		t.Fatalf("full read-only database check: %+v %v", report, err)
	}
	if err = target.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_billing.ledger_entries),(SELECT count(*) FROM v3_billing.reservations)`).Scan(&entries, &holds); err != nil || entries != 1 || holds != 0 {
		t.Fatal("database import charged background history or failed opening balance idempotency")
	}
	if !reflect.DeepEqual(before, backgroundRedisSnapshot(t, client)) {
		t.Fatal("database migration changed read-only Redis history")
	}
}

func TestOfflineBackgroundAssetsRefuseSourceProblemsBeforePublication(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedSourceBackground(t, source)
	repository, client := migrationBackgroundRedis(t, false)
	ctx := context.Background()
	importer := NewImporter(readonlySource(t, source), target, crypto)
	before := backgroundRedisSnapshot(t, client)
	for _, problem := range []string{"pending", "owner", "wrong_secret", "malformed_cipher", "event_gap", "unbilled"} {
		t.Run(problem, func(t *testing.T) {
			switch problem {
			case "pending":
				_, _ = source.Exec(ctx, `UPDATE gateway.responses_background_jobs SET status='in_progress' WHERE status='completed'`)
				defer func() {
					_, _ = source.Exec(ctx, `UPDATE gateway.responses_background_jobs SET status='completed' WHERE status='in_progress'`)
				}()
			case "owner":
				_, _ = source.Exec(ctx, `UPDATE migration_source.tokens SET user_id=8`)
				defer func() { _, _ = source.Exec(ctx, `UPDATE migration_source.tokens SET user_id=7`) }()
			case "wrong_secret":
				t.Setenv("V3_MIGRATION_SOURCE_CRYPTO_SECRET", "wrong-test-secret")
			case "malformed_cipher":
				_, _ = source.Exec(ctx, `UPDATE gateway.responses_background_jobs SET error_ciphertext='enc:v1:malformed' WHERE status='completed'`)
				defer func() {
					_, _ = source.Exec(ctx, `UPDATE gateway.responses_background_jobs SET error_ciphertext='' WHERE status='completed'`)
				}()
			case "event_gap":
				_, _ = source.Exec(ctx, `UPDATE gateway.responses_background_events SET sequence=3 WHERE id=2`)
				defer func() {
					_, _ = source.Exec(ctx, `UPDATE gateway.responses_background_events SET sequence=1 WHERE id=2`)
				}()
			case "unbilled":
				_, _ = source.Exec(ctx, `ALTER TABLE gateway.responses_background_jobs ADD COLUMN billed bool; UPDATE gateway.responses_background_jobs SET billed=false`)
				defer func() { _, _ = source.Exec(ctx, `ALTER TABLE gateway.responses_background_jobs DROP COLUMN billed`) }()
			}
			report, err := importer.ImportBackground(ctx, repository, true)
			encoded, encodeErr := json.Marshal(report)
			if err == nil || report.Applied || encodeErr != nil || strings.Contains(string(encoded), "PRIVATE") || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("invalid source was accepted or sensitive data disclosed")
			}
			if !reflect.DeepEqual(before, backgroundRedisSnapshot(t, client)) {
				t.Fatal("invalid source published part of its terminal history")
			}
		})
	}
}

func TestOfflineBackgroundAssetsRefuseChangedTargetWithoutOverwrite(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedSourceBackground(t, source)
	repository, client := migrationBackgroundRedis(t, false)
	ctx := context.Background()
	sourceJob, sourceEvents := sourceBackgroundFixture(t, "failed")
	asset, err := projectBackground(sourceJob, sourceEvents, backgroundSourceTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	asset.job.UserID = 8
	if err = repository.ImportTerminalJob(ctx, asset.job, asset.events); err != nil {
		t.Fatal(err)
	}
	before := backgroundRedisSnapshot(t, client)
	importer := NewImporter(readonlySource(t, source), target, crypto)
	if _, err = importer.ImportBackground(ctx, repository, true); err == nil {
		t.Fatal("changed target owner was overwritten")
	}
	if _, err = importer.CheckBackground(ctx, repository); err == nil {
		t.Fatal("missing or changed target passed the read-only check")
	}
	if !reflect.DeepEqual(before, backgroundRedisSnapshot(t, client)) {
		t.Fatal("target conflict published new jobs or replaced existing history")
	}
}
