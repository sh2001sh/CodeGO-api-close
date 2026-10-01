//go:build pgintegration

package legacy

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/security"
)

func TestSecurityMigrationNativeGuardAndAuditConsumers(t *testing.T) {
	client := migrationSecurityRedis(t)
	source, target, crypto := importTestDB(t)
	seedSecurityFixture(t, source)
	ctx := context.Background()
	reader := readonlySource(t, source)
	if _, err := reader.Exec(ctx, `UPDATE migration_source.account_request_abuse_states SET blocked=false`); err == nil {
		t.Fatal("source reader can change restrictions")
	}
	tx, err := reader.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := loadSecurityData(ctx, tx, sources)
	if err != nil {
		t.Fatal(err)
	}
	r := Report{}
	d.validate(&r)
	if len(r.Issues) != 0 || r.Counts["security.security_audit_events"] != 2 || r.Counts["security.account_request_abuse_states"] != 2 || r.Counts["security.historical_deleted_user_references"] != 1 {
		t.Fatalf("security preflight counts=%v issues=%v", r.Counts, r.Issues)
	}
	var count int64
	if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_security.security_audit_events`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("preflight wrote target: count=%d err=%v", count, err)
	}
	users, err := loadUsers(ctx, tx, sources)
	if err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(reader, target, crypto)
	for i := 0; i < 2; i++ {
		if err = pgx.BeginFunc(ctx, target, func(out pgx.Tx) error {
			if err := importer.importUsers(ctx, out, users); err != nil {
				return err
			}
			return importer.importSecurityData(ctx, out, d)
		}); err != nil {
			t.Fatalf("security apply %d: %v", i, err)
		}
	}
	checked := Report{}
	check := func() error {
		return pgx.BeginTxFunc(ctx, target, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(out pgx.Tx) error { return importer.checkSecurityData(ctx, out, d, &checked) })
	}
	if err = check(); err != nil || len(checked.Issues) != 0 || checked.Counts["check:security"] != 4 {
		t.Fatalf("security read-only check counts=%v issues=%v err=%v", checked.Counts, checked.Issues, err)
	}
	assertImportedSecurityConsumers(t, target, client)
	var sourceStrikes, sourceEvents int64
	if err = source.QueryRow(ctx, `SELECT (SELECT sum(strikes) FROM migration_source.account_request_abuse_states),(SELECT count(*) FROM migration_source.security_audit_events)`).Scan(&sourceStrikes, &sourceEvents); err != nil || sourceStrikes != 3 || sourceEvents != 2 {
		t.Fatalf("source evidence changed: strikes=%d events=%d err=%v", sourceStrikes, sourceEvents, err)
	}
	if _, err = target.Exec(ctx, `UPDATE v3_security.security_audit_events SET review_note='altered' WHERE id='original-string-id'`); err != nil {
		t.Fatal(err)
	}
	checked = Report{}
	if err = check(); err != nil || len(checked.Issues) != 1 {
		t.Fatalf("persistent audit mutation not detected: issues=%v err=%v", checked.Issues, err)
	}
	if err = pgx.BeginFunc(ctx, target, func(out pgx.Tx) error { return importer.importSecurityData(ctx, out, d) }); err == nil {
		t.Fatal("changed retained audit silently overwritten on retry")
	}
	if _, err = target.Exec(ctx, `DELETE FROM v3_security.account_request_abuse_states WHERE user_id=8`); err != nil {
		t.Fatal(err)
	}
	checked = Report{}
	if err = check(); err != nil || len(checked.Issues) < 3 {
		t.Fatalf("dropped operational state not detected: issues=%v err=%v", checked.Issues, err)
	}
}

func assertImportedSecurityConsumers(t *testing.T, target *pgxpool.Pool, client *redis.Client) {
	t.Helper()
	ctx := context.Background()
	g, err := security.New(target, client, security.Config{Enabled: true, Now: func() time.Time { return time.Unix(1800000000, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if err = g.Check(ctx, 7, "same-replayed-request"); err != nil {
			t.Fatalf("old restricted user admission %d: %v", i, err)
		}
	}
	var policy *gateway.UpstreamError
	if err = g.Check(ctx, 7, "same-replayed-request"); !errors.As(err, &policy) || policy.Status != 429 || policy.Code != "account_request_rpm_reached" {
		t.Fatalf("imported restriction/replay not enforced: %v", err)
	}
	policy = nil
	if err = g.Check(ctx, 8, "blocked-request"); !errors.As(err, &policy) || policy.Status != 403 || policy.Code != "account_request_disabled" {
		t.Fatalf("imported blocked state not enforced: %v", err)
	}
	owner := security.Actor{UserID: 7}
	list, err := g.List(ctx, owner, security.Query{PageSize: 100})
	if err != nil || list.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != "original-string-id" {
		t.Fatalf("imported audit owner listing: total=%d err=%v", list.Total, err)
	}
	event, err := g.Get(ctx, owner, "original-string-id")
	if err != nil || event.UserID != 7 || event.TokenID != 11 || event.ChannelID != 13 || event.UpdatedAt != nil || event.ReviewedAt == nil || event.ReviewedAt.Nanosecond() != 654321000 || event.UpstreamErrorBody != "" || event.UpstreamErrorMessage != "" || event.PromptPreview != "" {
		t.Fatalf("owner audit identity/null/time/redaction incorrect: err=%v", err)
	}
	if _, err = g.Get(ctx, owner, "deleted-historical-id"); !errors.Is(err, security.ErrNotFound) {
		t.Fatalf("foreign audit escaped imported owner scope: %v", err)
	}
	admin := security.Actor{UserID: 7, Admin: true}
	event, err = g.Get(ctx, admin, "original-string-id")
	if err != nil || event.UpstreamErrorBody != "dummy-private-body" || event.PromptPreview != "dummy-private-preview" || event.NotificationSuccess != 1 || event.ReviewNote != "original note" {
		t.Fatalf("privileged imported evidence lost: err=%v", err)
	}
	event, err = g.Get(ctx, admin, "deleted-historical-id")
	if err != nil || event.UserID != 9007199254740993 || event.ChannelID != 999 || event.CreatedAt != nil || event.UpdatedAt != nil {
		t.Fatalf("historical deleted references/exact bigint/null lost: err=%v", err)
	}
	var out bytes.Buffer
	if err = g.ExportCSV(ctx, owner, security.Query{}, &out); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&out).ReadAll()
	if err != nil || len(rows) != 2 || rows[1][0] != "original-string-id" || strings.Contains(out.String(), "dummy-private") {
		t.Fatalf("imported owner export exactness/privacy incorrect: err=%v", err)
	}
}

func TestSecurityMigrationMissingOperationalUserIsRejected(t *testing.T) {
	source, _, _ := importTestDB(t)
	seedSecurityFixture(t, source)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `INSERT INTO migration_source.account_request_abuse_states VALUES(999,1,0,0,true,NULL,NULL)`); err != nil {
		t.Fatal(err)
	}
	tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := loadSecurityData(ctx, tx, sources)
	if err != nil {
		t.Fatal(err)
	}
	r := Report{}
	d.validate(&r)
	if len(r.Issues) != 1 || r.Issues[0].Code != "missing_user" || r.Issues[0].ID != 999 {
		t.Fatalf("unsafe operational security reference report: %v", r.Issues)
	}
}
