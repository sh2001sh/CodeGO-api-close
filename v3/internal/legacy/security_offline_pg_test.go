//go:build pgintegration

package legacy

import (
	"context"
	"testing"
)

func TestSecurityFullOfflineImporterKeepsOperationalStateAndScopedEvidence(t *testing.T) {
	client := migrationSecurityRedis(t)
	source, target, crypto := importTestDB(t)
	seedSecurityFixture(t, source)
	reader := readonlySource(t, source)
	importer := NewImporter(reader, target, crypto)
	ctx := context.Background()
	report, err := importer.Import(ctx, false)
	if err != nil || len(report.Issues) != 0 || report.Applied || report.Counts["security.security_audit_events"] != 2 || report.Counts["security.account_request_abuse_states"] != 2 {
		t.Fatalf("full security dry-run counts=%v issues=%v err=%v", report.Counts, report.Issues, err)
	}
	var count int64
	if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_security.account_request_abuse_states`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("full dry-run wrote state: count=%d err=%v", count, err)
	}
	for i := 0; i < 2; i++ {
		report, err = importer.Import(ctx, true)
		if err != nil || !report.Applied || len(report.Issues) != 0 {
			t.Fatalf("full security apply %d: issues=%v err=%v", i, report.Issues, err)
		}
	}
	report, err = importer.Check(ctx)
	if err != nil || len(report.Issues) != 0 || report.Counts["check:security"] != 4 {
		t.Fatalf("full security check counts=%v issues=%v err=%v", report.Counts, report.Issues, err)
	}
	assertImportedSecurityConsumers(t, target, client)
	if _, err = target.Exec(ctx, `UPDATE v3_security.account_request_abuse_states SET blocked=false WHERE user_id=8`); err != nil {
		t.Fatal(err)
	}
	report, err = importer.Check(ctx)
	if err == nil || len(report.Issues) != 1 {
		t.Fatalf("full check missed changed operational restriction: issues=%v err=%v", report.Issues, err)
	}
	if _, err = importer.Import(ctx, true); err == nil {
		t.Fatal("full retry silently replaced changed operational restriction")
	}
	var strikes int64
	if err = target.QueryRow(ctx, `SELECT sum(strikes) FROM v3_security.account_request_abuse_states`).Scan(&strikes); err != nil || strikes != 3 {
		t.Fatalf("failed retry changed persistent restriction counters: strikes=%d err=%v", strikes, err)
	}
}
