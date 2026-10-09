//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func onlineNamesSnapshot(t *testing.T, target *pgxpool.Pool, tables []string) string {
	t.Helper()
	var raw []byte
	err := target.QueryRow(context.Background(), `WITH tables AS (SELECT unnest($1::text[]) AS name), objects AS (
 SELECT t.name AS table_name,'constraint' AS kind,c.conname::text AS name FROM tables t JOIN pg_constraint c ON c.conrelid=t.name::regclass
 UNION ALL SELECT t.name,'index',r.relname::text FROM tables t JOIN pg_index i ON i.indrelid=t.name::regclass JOIN pg_class r ON r.oid=i.indexrelid
 UNION ALL SELECT t.name,'sequence',r.relname::text FROM tables t JOIN pg_attribute a ON a.attrelid=t.name::regclass AND a.attidentity<>''
 JOIN pg_depend d ON d.refclassid='pg_class'::regclass AND d.refobjid=t.name::regclass AND d.refobjsubid=a.attnum AND d.classid='pg_class'::regclass AND d.deptype='i'
 JOIN pg_class r ON r.oid=d.objid AND r.relkind='S'
) SELECT COALESCE(jsonb_agg(to_jsonb(o) ORDER BY table_name,kind,name),'[]') FROM objects o`, tables).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestOnlineNamesFinalPreservesNativeObjectsAndRollsBackAtomically(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "final", true: "failed_final"}[fail], func(t *testing.T) {
			m, source, target, opts := onlineMigrationFixture(t, true)
			ctx := context.Background()
			onlineRecoveryExec(t, target, `
 CREATE INDEX native_duplicate_a ON v3_billing.historical_entries(created_at);
 CREATE INDEX native_duplicate_b ON v3_billing.historical_entries(created_at);
 ALTER TABLE v3_billing.historical_entries RENAME CONSTRAINT historical_entries_pkey TO native_history_primary;
 ALTER TABLE v3_billing.historical_entries RENAME CONSTRAINT historical_entries_idempotency_key_key TO native_history_idempotency;
 ALTER SEQUENCE v3_billing.usage_logs_id_seq RENAME TO native_usage_identity_seq`)
			tables := []string{"v3_billing.historical_entries", "v3_billing.usage_logs", "v3_billing.usage_logs_default", "v3_audit.events", "v3_audit.request_audits", "v3_audit.request_attempt_audits", "v3_audit.orphan_request_attempt_history", "v3_billing.funding_lots", "v3_billing.funding_allocations"}
			before := onlineNamesSnapshot(t, target, tables)
			if !strings.Contains(before, "native_usage_identity_seq") || !strings.Contains(before, "usage_logs_default") {
				t.Fatal("native fixture did not include the sequence and partition child")
			}
			onlineMigrationReady(t, m, opts)
			var mapping []byte
			if err := target.QueryRow(ctx, "SELECT object_names FROM v3_migration_online.run").Scan(&mapping); err != nil || len(mapping) < 10 {
				t.Fatalf("prepare did not remember object names mapping=%s err=%v", mapping, err)
			}
			if fail {
				onlineRecoveryExec(t, target, "INSERT INTO v3_identity.users(id,username,email,role,status,group_name,settings)VALUES(7,'different-person','different@example.invalid','user','active','default','{}')")
			}
			if _, err := SealOnlineCapture(ctx, source, opts.RunID); err != nil {
				t.Fatal(err)
			}
			r, err := m.FinalizeOnline(ctx, opts)
			if fail {
				if err == nil || r.Applied {
					t.Fatal("final with identity conflict committed")
				}
				onlineRecoveryAssertNoMoney(t, target)
				var indexExists bool
				if err := target.QueryRow(ctx, "SELECT to_regclass('v3_migration_online.online_log_group_idx') IS NOT NULL").Scan(&indexExists); err != nil || !indexExists {
					t.Fatalf("failed final did not roll back migration-index drop exists=%v err=%v", indexExists, err)
				}
			} else {
				if err != nil || !r.Applied {
					t.Fatalf("final %+v err=%v", r, err)
				}
				if checked, err := m.Check(ctx); err != nil || len(checked.Issues) != 0 {
					t.Fatalf("independent full check %+v err=%v", checked, err)
				}
				var indexExists bool
				if err := target.QueryRow(ctx, "SELECT to_regclass('v3_audit.online_log_group_idx') IS NOT NULL").Scan(&indexExists); err != nil || indexExists {
					t.Fatalf("temporary migration index survived final exists=%v err=%v", indexExists, err)
				}
				var seq string
				if err := target.QueryRow(ctx, "SELECT pg_get_serial_sequence('v3_billing.usage_logs','id')").Scan(&seq); err != nil || seq != "v3_billing.native_usage_identity_seq" {
					t.Fatalf("identity sequence ownership/name changed sequence=%q err=%v", seq, err)
				}
			}
			if after := onlineNamesSnapshot(t, target, tables); after != before {
				t.Fatalf("native constraint/index/sequence names changed before=%s after=%s", before, after)
			}
		})
	}
}
