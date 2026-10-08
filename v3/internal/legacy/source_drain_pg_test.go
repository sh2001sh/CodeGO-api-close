//go:build pgintegration

package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestSourceDrainCollectsAllCanonicalBlockersAndBindsOpaqueRows(t *testing.T) {
	source, target, _ := importTestDB(t)
	seedProjectionDrain(t, source)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `CREATE TABLE gateway.responses_background_jobs(id text,user_id bigint,token_id bigint,status text,private_ciphertext text);
	 INSERT INTO gateway.responses_background_jobs VALUES('rejected-request',7,11,'in_progress','SECRET_SENTINEL_DO_NOT_OUTPUT');
	 ALTER TABLE gateway.request_audits ADD COLUMN private_ciphertext text DEFAULT 'SECRET_SENTINEL_DO_NOT_OUTPUT';`); err != nil {
		t.Fatal(err)
	}
	reader := readonlySource(t, source)
	// The restricted role may read cluster identity, but still has no table DML.
	if _, err := source.Exec(ctx, "GRANT EXECUTE ON FUNCTION pg_control_system() TO "+pgx.Identifier{reader.Config().ConnConfig.User}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	options := SourceDrainOptions{FrozenManifestSHA256: strings.Repeat("a", 64)}
	r, err := DrainSource(ctx, reader, options)
	if !errors.Is(err, ErrSourceDrainBlocked) || !r.Completed || !r.Source.ReadOnly || len(r.Report.Issues) != 2 || r.PendingBackground.Count != 1 || len(r.UndrainedAudits) != 1 || len(r.UndrainedExecutions) != 0 {
		t.Fatalf("background must not hide later canonical blockers: %+v %v", r, err)
	}
	if r.FrozenManifestSHA256 != options.FrozenManifestSHA256 || !drainSHA.MatchString(r.BinarySHA256) || !drainSHA.MatchString(r.Source.IdentitySHA256) || r.Source.Database != source.Config().ConnConfig.Database || r.Source.CompletedAt.Before(r.Source.StartedAt) || !r.Checks["gateway_request_audits"].Completed || r.Report.Counts["source_accounts_checked"] != 1 {
		t.Fatalf("source/binary/check binding incomplete: %+v", r)
	}
	row := r.UndrainedAudits[0]
	if row.RequestID != "rejected-request" || row.BackgroundID != row.RequestID || !row.OwnerKeyMatch || row.BackgroundRowSHA256 != r.PendingBackground.Rows[0].RowSHA256 || !drainSHA.MatchString(row.RowSHA256) {
		t.Fatalf("full audit and dependency row evidence missing: %+v", row)
	}
	var sourceHash string
	if err := source.QueryRow(ctx, `SELECT encode(sha256(convert_to(to_jsonb(a)::text,'UTF8')),'hex') FROM gateway.request_audits a WHERE request_id='rejected-request'`).Scan(&sourceHash); err != nil || row.RowSHA256 != sourceHash {
		t.Fatalf("audit original row SHA differs: %s/%s %v", row.RowSHA256, sourceHash, err)
	}
	encoded, err := json.Marshal(r)
	if err != nil || strings.Contains(string(encoded), "SECRET_SENTINEL") {
		t.Fatalf("source secret leaked into binding report: %v", err)
	}
	if drainDigest([]byte(r.ProjectionAssertionSQL)) != r.ProjectionAssertionSHA256 {
		t.Fatal("post assertion SQL is not SHA-bound")
	}
	for _, forbidden := range []string{"UPDATE ", "INSERT ", "DELETE ", "ALTER ", "COMMIT", "ROLLBACK"} {
		if strings.Contains(strings.ToUpper(r.ProjectionAssertionSQL), forbidden) {
			t.Fatalf("post assertion contains mutation %s", forbidden)
		}
	}
	if _, err := reader.Exec(ctx, `UPDATE gateway.responses_background_jobs SET status='failed'`); err == nil {
		t.Fatal("drain source role unexpectedly acquired table write permission")
	}
	// Same recovery transaction sees its own terminal metadata and can fail
	// atomically before commit if any canonical financial proof is damaged.
	write, err := source.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = write.Rollback(ctx) }()
	if _, err := write.Exec(ctx, `UPDATE gateway.responses_background_jobs SET status='failed'`); err != nil {
		t.Fatal(err)
	}
	if _, err := write.Exec(ctx, r.ProjectionAssertionSQL); err != nil {
		t.Fatalf("formal post assertion cannot see recovery's own writes: %v", err)
	}
	if _, err := write.Exec(ctx, `UPDATE billing.reservations SET status='open' WHERE reservation_id='stale-reservation'`); err != nil {
		t.Fatal(err)
	}
	if _, err := write.Exec(ctx, r.ProjectionAssertionSQL); err == nil || !strings.Contains(err.Error(), "canonical_post_drain_failed") {
		t.Fatalf("post assertion accepted damaged canonical money proof: %v", err)
	}
	if err := write.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := source.QueryRow(ctx, `SELECT status FROM gateway.responses_background_jobs`).Scan(&status); err != nil || status != "in_progress" {
		t.Fatalf("failed post guard did not roll back recovery: %s %v", status, err)
	}
	var targetRows int64
	if err := target.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users`).Scan(&targetRows); err != nil || targetRows != 0 {
		t.Fatalf("drain changed independent target: %d %v", targetRows, err)
	}
}

func TestSourceDrainUsesExportedSnapshotAndReportsFinancialFailure(t *testing.T) {
	source, _, _ := importTestDB(t)
	ctx := context.Background()
	frozen, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = frozen.Rollback(ctx) }()
	var snapshot string
	if err := frozen.QueryRow(ctx, `SELECT pg_export_snapshot()`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec(ctx, `UPDATE billing.balance_snapshots SET reserved_balance=1`); err != nil {
		t.Fatal(err)
	}
	r, err := DrainSource(ctx, source, SourceDrainOptions{ExportedSnapshot: snapshot})
	if err != nil || !r.Completed || r.Source.ExportedSnapshot != snapshot || len(r.Report.Issues) != 0 {
		t.Fatalf("shared snapshot did not preserve frozen state: %+v %v", r, err)
	}
	r, err = DrainSource(ctx, source, SourceDrainOptions{})
	if !errors.Is(err, ErrSourceDrainBlocked) || !r.Completed || len(r.Report.Issues) == 0 || r.Report.Issues[0].Code != "open_account_reservations" {
		t.Fatalf("actual reserved money accepted: %+v %v", r, err)
	}
	if err := frozen.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	r, err = DrainSource(ctx, source, SourceDrainOptions{ExportedSnapshot: snapshot})
	if err == nil || r.Completed || r.ErrorCode != "source_snapshot_failed" {
		t.Fatalf("expired exported snapshot claimed complete: %+v %v", r, err)
	}
}

func TestSourceDrainRejectsSQLFailureAndSafelyQuotesAssertionIdentifiers(t *testing.T) {
	source, _, _ := importTestDB(t)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `CREATE SCHEMA "odd$codego_drain_assert$";
	 CREATE TABLE "odd$codego_drain_assert$".request_audits(request_id text,status text);
	 INSERT INTO "odd$codego_drain_assert$".request_audits VALUES('terminal','failed');`); err != nil {
		t.Fatal(err)
	}
	r, err := DrainSource(ctx, source, SourceDrainOptions{})
	if err != nil || !r.Completed {
		t.Fatalf("quoted source identifier blocked drain: %+v %v", r, err)
	}
	if _, err := source.Exec(ctx, r.ProjectionAssertionSQL); err != nil {
		t.Fatalf("source identifier escaped dollar assertion body: %v", err)
	}
	if _, err := source.Exec(ctx, `CREATE SCHEMA billing_bad;
	 CREATE TABLE billing_bad.reservations(request_id text);
	 INSERT INTO billing_bad.reservations VALUES('shape-missing-status');`); err != nil {
		t.Fatal(err)
	}
	r, err = DrainSource(ctx, source, SourceDrainOptions{})
	if err == nil || r.Completed || r.ErrorCode != "source_drain_incomplete" {
		t.Fatalf("SQL/shape failure claimed complete drain: %+v %v", r, err)
	}
}

func TestSourceDrainHashAlgorithmIsStable(t *testing.T) {
	r, err := NewSourceDrainReport(SourceDrainOptions{})
	if err != nil || !drainSHA.MatchString(r.BinarySHA256) {
		t.Fatalf("running executable SHA unavailable: %+v %v", r, err)
	}
	data := []byte(`["07f7ee2cc8fe","13562dba6a07"]`)
	sum := sha256.Sum256(data)
	if drainDigest(data) != hex.EncodeToString(sum[:]) {
		t.Fatal("pending set digest differs from UTF-8 compact JSON")
	}
}
