//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"
)

func TestOnlineTargetSchemaDriftRefusesSyncAndFinalization(t *testing.T) {
	cases := []struct{ name, sql string }{
		{"native_new_column", "ALTER TABLE v3_audit.events ADD COLUMN release_column text NOT NULL DEFAULT 'v3'"},
		{"staged_new_column", "ALTER TABLE " + onlineStage("v3_audit.events") + " ADD COLUMN unexpected text"},
		{"native_check", "ALTER TABLE v3_audit.events ADD CONSTRAINT release_check CHECK(amount>=0)"},
		{"native_index", "CREATE INDEX release_index ON v3_audit.events(content)"},
		{"stage_index", "CREATE INDEX release_index ON " + onlineStage("v3_audit.events") + "(content)"},
		{"native_rls", "ALTER TABLE v3_audit.events ENABLE ROW LEVEL SECURITY"},
		{"native_acl", "GRANT SELECT ON v3_audit.events TO PUBLIC"},
		{"stage_guard", "ALTER TABLE " + onlineStage("v3_audit.events") + " DISABLE TRIGGER online_staging_guard"},
		{"guard_function", "CREATE OR REPLACE FUNCTION v3_migration_online.guard() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$ BEGIN RETURN NULL; END $$"},
		{"native_oid", "ALTER TABLE v3_audit.events RENAME TO previous_events; CREATE TABLE v3_audit.events (LIKE v3_audit.previous_events INCLUDING ALL)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, source, target, opts := onlineMigrationFixture(t, false)
			ctx := context.Background()
			onlineMigrationReady(t, m, opts)
			if _, err := target.Exec(ctx, tc.sql); err != nil {
				t.Fatal(err)
			}
			if _, err := m.SyncOnline(ctx, opts); err == nil || !strings.Contains(err.Error(), "target schema changed") {
				t.Fatalf("sync accepted target DDL drift: %v", err)
			}
			if _, err := SealOnlineCapture(ctx, source, opts.RunID); err != nil {
				t.Fatal(err)
			}
			if _, err := m.FinalizeOnline(ctx, opts); err == nil || !strings.Contains(err.Error(), "target schema changed") {
				t.Fatalf("finalization accepted target DDL drift: %v", err)
			}
			onlineRecoveryAssertNoMoney(t, target)
			var sealed bool
			if err := source.QueryRow(ctx, "SELECT sealed FROM v3_migration_capture.config").Scan(&sealed); err != nil || !sealed {
				t.Fatalf("refusal released source fence: %t %v", sealed, err)
			}
		})
	}
}

func TestOnlineMetadataRequiresMigrationOwnership(t *testing.T) {
	m, _, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineMigrationReady(t, m, opts)
	for _, sql := range []string{
		"UPDATE v3_migration_online.totals SET value=value+1",
		"DELETE FROM v3_migration_online.foreign_keys",
		"UPDATE v3_migration_online.run SET target_shape=''",
		"UPDATE v3_migration_online.progress SET complete=false",
		"DELETE FROM v3_migration_online.account_ids",
		"DELETE FROM v3_migration_online.retired_rows",
	} {
		if _, err := target.Exec(ctx, sql); err == nil {
			t.Fatal("unowned metadata modification accepted", sql)
		}
	}
	// Replication mode must not bypass the staging or receipt guards.
	tx, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, "SET LOCAL session_replication_role=replica"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE v3_migration_online.totals SET value=value+1"); err == nil {
		t.Fatal("replication mode bypassed the receipt guard")
	}
}

func TestOnlinePrepareRefusesUsedTargetBeforeInstallingCapture(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	if _, err := target.Exec(ctx, "INSERT INTO v3_identity.users(username) VALUES('existing-user')"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PrepareOnline(ctx, opts, true); err == nil || !strings.Contains(err.Error(), "unused target") {
		t.Fatalf("used target was accepted: %v", err)
	}
	var installed bool
	if err := source.QueryRow(ctx, "SELECT to_regclass('v3_migration_capture.config') IS NOT NULL").Scan(&installed); err != nil || installed {
		t.Fatalf("invalid target permanently acquired source capture: %t %v", installed, err)
	}
	same := NewImporter(source, source, m.crypto)
	if _, err := same.PrepareOnline(ctx, opts, true); err == nil || !strings.Contains(err.Error(), "independent database") {
		t.Fatalf("source database accepted as target: %v", err)
	}
	if err := source.QueryRow(ctx, "SELECT to_regclass('v3_migration_capture.config') IS NOT NULL").Scan(&installed); err != nil || installed {
		t.Fatalf("source-as-target attempt installed capture: %t %v", installed, err)
	}
}
