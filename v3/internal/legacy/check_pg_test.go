//go:build pgintegration

package legacy

import (
	"context"
	"testing"
)

func TestFullCheckDetectsMutationAndMissingCoreRows(t *testing.T) {
	source, target, crypto := importTestDB(t)
	ctx := context.Background()
	importer := NewImporter(readonlySource(t, source), target, crypto)
	if _, err := importer.Import(ctx, true); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if report, err := importer.Check(ctx); err != nil || len(report.Issues) != 0 || report.Counts["check:users"] != 1 {
			t.Fatalf("valid check report=%+v err=%v", report, err)
		}
	}
	if _, err := target.Exec(ctx, `UPDATE v3_identity.api_keys SET allowed_models=ARRAY['wrong-model'] WHERE id=11`); err != nil {
		t.Fatal(err)
	}
	if report, err := importer.Check(ctx); err == nil || len(report.Issues) == 0 {
		t.Fatal("key limit mutation was not detected")
	}
	if _, err := target.Exec(ctx, `UPDATE v3_identity.api_keys SET allowed_models=NULL WHERE id=11`); err != nil {
		t.Fatal(err)
	}
	if _, err := target.Exec(ctx, `DELETE FROM v3_catalog.channel_models WHERE channel_id=13`); err != nil {
		t.Fatal(err)
	}
	if report, err := importer.Check(ctx); err == nil || len(report.Issues) == 0 {
		t.Fatal("missing channel-model association was not detected")
	}
}

func TestOfflineImportRejectsUndrainedAccountingWork(t *testing.T) {
	source, target, crypto := importTestDB(t)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `CREATE TABLE billing.reservations(reservation_id text,status text);
		INSERT INTO billing.reservations VALUES('pending-local-test','open')`); err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(readonlySource(t, source), target, crypto)
	if report, err := importer.Import(ctx, true); err == nil || len(report.Issues) == 0 || report.Applied {
		t.Fatal("open source reservation imported")
	}
	var count int
	if err := target.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users`).Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected pending work left partial rows")
	}
	if _, err := source.Exec(ctx, `UPDATE billing.reservations SET status='released'`); err != nil {
		t.Fatal(err)
	}
	if report, err := importer.Import(ctx, true); err != nil || !report.Applied {
		t.Fatalf("drained accounting work rejected: %+v %v", report, err)
	}
}
