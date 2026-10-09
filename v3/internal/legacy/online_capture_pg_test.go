//go:build pgintegration

package legacy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const onlineCaptureTestRun = "online-fixture-20261009"

func onlineCaptureTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("V3_MIGRATION_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_MIGRATION_TEST_PG_DSN is required for an isolated fixture")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()
	random := make([]byte, 8)
	if _, err = rand.Read(random); err != nil {
		t.Fatal(err)
	}
	name := "online_capture_" + hex.EncodeToString(random)
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func onlineCaptureTestExec(t *testing.T, pool *pgxpool.Pool, sql string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql); err != nil {
		t.Fatal(err)
	}
}

func onlineCaptureTestInstall(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := SetupOnlineCapture(context.Background(), pool, onlineCaptureTestRun, true); err != nil {
		t.Fatal(err)
	}
}

func onlineCaptureTestValidate(pool *pgxpool.Pool) error {
	return pgx.BeginTxFunc(context.Background(), pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		_, err := ValidateOnlineCapture(context.Background(), tx, onlineCaptureTestRun)
		return err
	})
}

func TestOnlineCapturePreviewAndTransactionalKeys(t *testing.T) {
	pool := onlineCaptureTestPool(t)
	ctx := context.Background()
	onlineCaptureTestExec(t, pool, `CREATE TABLE keyed(id bigint PRIMARY KEY,payload text);
 CREATE TABLE composite(tenant text,"odd key" bigint,payload text,PRIMARY KEY(tenant,"odd key"));
 CREATE TABLE no_pk(payload text);
 CREATE TABLE partitioned(id bigint PRIMARY KEY,payload text) PARTITION BY RANGE(id);
 CREATE TABLE partition_a PARTITION OF partitioned FOR VALUES FROM(0) TO(100)`)
	r, err := SetupOnlineCapture(ctx, pool, onlineCaptureTestRun, false)
	if err != nil || r.Applied || len(r.Tables) != 4 {
		t.Fatalf("preview=%+v err=%v", r, err)
	}
	var exists bool
	if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname='v3_migration_capture')`).Scan(&exists); err != nil || exists {
		t.Fatalf("preview modified source exists=%v err=%v", exists, err)
	}
	onlineCaptureTestInstall(t, pool)
	if err = onlineCaptureTestValidate(pool); err != nil {
		t.Fatal(err)
	}
	if _, err = SetupOnlineCapture(ctx, pool, "different-fixture-run", true); err == nil {
		t.Fatal("another capture run accepted")
	}
	onlineCaptureTestExec(t, pool, `INSERT INTO keyed VALUES(1,'first'); UPDATE keyed SET payload='changed' WHERE id=1; UPDATE keyed SET id=2 WHERE id=1; DELETE FROM keyed WHERE id=2;
 INSERT INTO composite VALUES('tenant',9007199254740993,'large payload irrelevant'); INSERT INTO no_pk VALUES('row'); INSERT INTO partition_a VALUES(5,'direct child')`)
	rows, err := pool.Query(ctx, `SELECT row_key FROM v3_migration_capture.events WHERE table_name='public.keyed' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, string(raw))
	}
	err = rows.Err()
	rows.Close()
	if err != nil || !reflect.DeepEqual(keys, []string{`{"id": 1}`, `{"id": 1}`, `{"id": 1}`, `{"id": 2}`, `{"id": 2}`}) {
		t.Fatalf("dirty old/new keys=%v err=%v", keys, err)
	}
	var raw []byte
	if err = pool.QueryRow(ctx, `SELECT row_key FROM v3_migration_capture.events WHERE table_name='public.composite'`).Scan(&raw); err != nil || !strings.Contains(string(raw), "9007199254740993") || strings.Contains(string(raw), "payload") {
		t.Fatalf("composite key precision/projection=%s err=%v", raw, err)
	}
	if err = pool.QueryRow(ctx, `SELECT row_key FROM v3_migration_capture.events WHERE table_name='public.no_pk'`).Scan(&raw); err != nil || string(raw) != "{}" {
		t.Fatalf("no-PK marker=%s err=%v", raw, err)
	}
	var partitionName string
	if err = pool.QueryRow(ctx, `SELECT table_name FROM v3_migration_capture.events WHERE row_key='{"id":5}'`).Scan(&partitionName); err != nil || partitionName != "public.partitioned" {
		t.Fatalf("partition event root=%s err=%v", partitionName, err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO keyed VALUES(80,'must roll back')`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_migration_capture.events WHERE row_key='{"id":80}'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled back event count=%d err=%v", count, err)
	}
	for _, sql := range []string{`TRUNCATE keyed`, `TRUNCATE partition_a`, `TRUNCATE no_pk`} {
		if _, err = pool.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "forbids TRUNCATE") {
			t.Fatalf("truncate accepted %s err=%v", sql, err)
		}
	}
	if err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role=replica; INSERT INTO keyed VALUES(90,'replica role is still captured')`); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_migration_capture.events WHERE row_key='{"id":90}'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("replica role bypassed ALWAYS capture count=%d err=%v", count, err)
	}
}

func TestOnlineCaptureLateCommitExactAckAndReplay(t *testing.T) {
	pool := onlineCaptureTestPool(t)
	ctx := context.Background()
	onlineCaptureTestExec(t, pool, `CREATE TABLE keyed(id bigint PRIMARY KEY,payload text)`)
	onlineCaptureTestInstall(t, pool)
	low, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = low.Rollback(ctx) }()
	if _, err = low.Exec(ctx, `INSERT INTO keyed VALUES(10,'long transaction')`); err != nil {
		t.Fatal(err)
	}
	var lowID, highID int64
	if err = low.QueryRow(ctx, `SELECT id FROM v3_migration_capture.events WHERE row_key='{"id":10}'`).Scan(&lowID); err != nil {
		t.Fatal(err)
	}
	onlineCaptureTestExec(t, pool, `INSERT INTO keyed VALUES(11,'short transaction')`)
	if err = pool.QueryRow(ctx, `SELECT id FROM v3_migration_capture.events WHERE row_key='{"id":11}'`).Scan(&highID); err != nil || highID <= lowID {
		t.Fatalf("out-of-order IDs low=%d high=%d err=%v", lowID, highID, err)
	}
	if err = AckOnlineCapture(ctx, pool, onlineCaptureTestRun, []int64{highID, highID}); err != nil {
		t.Fatal(err)
	}
	if err = low.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var pending []int64
	if err = pool.QueryRow(ctx, `SELECT array_agg(id ORDER BY id) FROM v3_migration_capture.events WHERE NOT acked`).Scan(&pending); err != nil || !reflect.DeepEqual(pending, []int64{lowID}) {
		t.Fatalf("late low-ID commit lost pending=%v err=%v", pending, err)
	}
	onlineCaptureTestExec(t, pool, `UPDATE keyed SET payload='newer same-key event' WHERE id=11`)
	if err = AckOnlineCapture(ctx, pool, onlineCaptureTestRun, []int64{highID}); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT array_agg(id ORDER BY id) FROM v3_migration_capture.events WHERE NOT acked`).Scan(&pending); err != nil || len(pending) != 2 || pending[0] != lowID || pending[1] <= highID {
		t.Fatalf("new event hidden by old batch ack pending=%v err=%v", pending, err)
	}
	if err = AckOnlineCapture(ctx, pool, onlineCaptureTestRun, []int64{lowID, 999999}); err == nil {
		t.Fatal("acknowledgment of missing event accepted")
	}
	var acked bool
	if err = pool.QueryRow(ctx, `SELECT acked FROM v3_migration_capture.events WHERE id=$1`, lowID).Scan(&acked); err != nil || acked {
		t.Fatalf("failed ack not atomic acked=%v err=%v", acked, err)
	}
	if err = AckOnlineCapture(ctx, pool, "another-fixture-run", []int64{lowID}); err == nil {
		t.Fatal("cross-run acknowledgment accepted")
	}
	var count int64
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_migration_capture.events`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("ack deleted backup evidence count=%d err=%v", count, err)
	}
}

func TestOnlineCaptureRejectsDDLandTriggerDrift(t *testing.T) {
	cases := map[string]string{
		"column":           `ALTER TABLE keyed ADD COLUMN extra text`,
		"table":            `CREATE TABLE added(id bigint PRIMARY KEY)`,
		"drop":             `DROP TABLE keyed`,
		"disabled":         `ALTER TABLE keyed DISABLE TRIGGER codego_online_capture_row`,
		"function":         `DO $$DECLARE fn text; BEGIN SELECT p.proname INTO fn FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='v3_migration_capture' AND p.proname LIKE 'capture_%'; EXECUTE format('CREATE OR REPLACE FUNCTION v3_migration_capture.%I() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,v3_migration_capture AS %L',fn,'BEGIN RETURN NULL; END'); END $$`,
		"grants":           `GRANT INSERT ON v3_migration_capture.events TO PUBLIC`,
		"journal-default":  `ALTER TABLE v3_migration_capture.events ALTER COLUMN acked SET DEFAULT true`,
		"journal-identity": `ALTER TABLE v3_migration_capture.events ALTER COLUMN id DROP IDENTITY`,
		"primary-key":      `ALTER TABLE keyed DROP CONSTRAINT keyed_pkey`,
	}
	for name, sql := range cases {
		t.Run(name, func(t *testing.T) {
			pool := onlineCaptureTestPool(t)
			onlineCaptureTestExec(t, pool, `CREATE TABLE keyed(id bigint PRIMARY KEY,payload text)`)
			onlineCaptureTestInstall(t, pool)
			onlineCaptureTestExec(t, pool, sql)
			if err := onlineCaptureTestValidate(pool); err == nil {
				t.Fatal("capture accepted source drift")
			}
		})
	}
}

func TestOnlineCaptureShapeStableAcrossRestoreOIDs(t *testing.T) {
	source, restored := onlineCaptureTestPool(t), onlineCaptureTestPool(t)
	const schema = `CREATE TYPE public.fixture_kind AS ENUM('first','second');
 CREATE TABLE public.keyed(id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,kind public.fixture_kind NOT NULL DEFAULT 'first',name text COLLATE "C",payload text);
 CREATE FUNCTION public.original_trigger() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RETURN NEW; END';
 CREATE TRIGGER original BEFORE INSERT ON public.keyed FOR EACH ROW EXECUTE FUNCTION public.original_trigger()`
	onlineCaptureTestExec(t, source, schema)
	// Deliberately shift the restored custom type and table OIDs.
	onlineCaptureTestExec(t, restored, `CREATE TYPE public.oid_shift AS ENUM('shift'); CREATE TABLE public.oid_shift_table(dummy int);`+schema)
	onlineCaptureTestExec(t, restored, `DROP TABLE public.oid_shift_table`)
	onlineCaptureTestInstall(t, source)
	read := func(pool *pgxpool.Pool) []OnlineCaptureTable {
		t.Helper()
		var tables []OnlineCaptureTable
		if err := pgx.BeginTxFunc(context.Background(), pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
			var err error
			tables, err = discoverOnlineCaptureTables(context.Background(), tx)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return tables
	}
	a, b := read(source), read(restored)
	if len(a) != 1 || len(b) != 1 || a[0].RelationOID == b[0].RelationOID || a[0].SchemaFingerprint != b[0].SchemaFingerprint {
		t.Fatalf("restore shape depended on OIDs/capture triggers: source=%+v restored=%+v", a, b)
	}
	onlineCaptureTestExec(t, source, `ALTER TABLE public.keyed DISABLE TRIGGER original`)
	if err := onlineCaptureTestValidate(source); err == nil {
		t.Fatal("original business trigger DDL not detected")
	}
}

func TestOnlineCaptureRejectsUnanalyzedLargeNoPK(t *testing.T) {
	pool := onlineCaptureTestPool(t)
	onlineCaptureTestExec(t, pool, `CREATE TABLE no_pk AS SELECT g AS value FROM generate_series(1,100001) g`)
	if _, err := SetupOnlineCapture(context.Background(), pool, onlineCaptureTestRun, true); err == nil || !strings.Contains(err.Error(), "without a primary key") {
		t.Fatalf("large unindexed no-PK source accepted err=%v", err)
	}
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname='v3_migration_capture')`).Scan(&exists); err != nil || exists {
		t.Fatalf("failed install left journal exists=%v err=%v", exists, err)
	}
}

func TestOnlineCapturePrivateTriggersAndSourceSeal(t *testing.T) {
	pool := onlineCaptureTestPool(t)
	ctx := context.Background()
	onlineCaptureTestExec(t, pool, `CREATE TABLE keyed(id bigint PRIMARY KEY,payload text); CREATE TABLE no_pk(payload text)`)
	onlineCaptureTestInstall(t, pool)
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	role := "capture_writer_" + hex.EncodeToString(random)
	onlineCaptureTestExec(t, pool, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN PASSWORD 'local-capture-writer'; GRANT USAGE ON SCHEMA public TO "+pgx.Identifier{role}.Sanitize()+"; GRANT SELECT,INSERT,UPDATE,DELETE ON keyed,no_pk TO "+pgx.Identifier{role}.Sanitize())
	config := pool.Config().Copy()
	config.ConnConfig.User, config.ConnConfig.Password = role, "local-capture-writer"
	writer, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err = writer.Exec(ctx, `INSERT INTO keyed VALUES(1,'writer trigger succeeds')`); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Exec(ctx, `INSERT INTO v3_migration_capture.events(table_name,row_key) VALUES('public.keyed','{"id":99}')`); err == nil {
		t.Fatal("source application forged capture event")
	}
	if _, err = writer.Exec(ctx, `SELECT v3_migration_capture.write_fence()`); err == nil {
		t.Fatal("source application can directly execute private function")
	}
	oldSnapshot, err := writer.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = oldSnapshot.Rollback(ctx) }()
	var count int64
	if err = oldSnapshot.QueryRow(ctx, `SELECT count(*) FROM keyed`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	r, err := SealOnlineCapture(ctx, pool, onlineCaptureTestRun)
	if err != nil || !r.Sealed {
		t.Fatalf("seal=%+v err=%v", r, err)
	}
	for _, sql := range []string{`INSERT INTO keyed VALUES(2,'blocked')`, `UPDATE keyed SET payload='blocked' WHERE id=1`, `DELETE FROM keyed WHERE id=1`, `INSERT INTO no_pk VALUES('blocked')`, `UPDATE no_pk SET payload='blocked' WHERE false`} {
		if _, err = writer.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "source is sealed") {
			t.Fatalf("sealed source accepted %s err=%v", sql, err)
		}
	}
	if _, err = oldSnapshot.Exec(ctx, `INSERT INTO keyed VALUES(3,'old snapshot bypass')`); err == nil {
		t.Fatal("old repeatable-read snapshot bypassed committed write fence")
	}
	if err = oldSnapshot.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	r, err = UnsealOnlineCapture(ctx, pool, onlineCaptureTestRun)
	if err != nil || r.Sealed {
		t.Fatalf("unseal=%+v err=%v", r, err)
	}
	if _, err = writer.Exec(ctx, `INSERT INTO keyed VALUES(4,'reopened explicitly')`); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err = pool.QueryRow(ctx, `SELECT row_key FROM v3_migration_capture.events WHERE row_key='{"id":4}'`).Scan(&raw); err != nil || !json.Valid(raw) {
		t.Fatalf("reopened source lost capture err=%v", err)
	}
}

func TestOnlineCaptureLockTimeoutDoesNotPartiallyInstallOrSeal(t *testing.T) {
	pool := onlineCaptureTestPool(t)
	ctx := context.Background()
	onlineCaptureTestExec(t, pool, `CREATE TABLE keyed(id bigint PRIMARY KEY,payload text)`)
	writer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Exec(ctx, `INSERT INTO keyed VALUES(1,'before capture')`); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err = SetupOnlineCapture(ctx, pool, onlineCaptureTestRun, true); err == nil || time.Since(start) > 4*time.Second {
		t.Fatalf("blocked install timeout err=%v duration=%v", err, time.Since(start))
	}
	if err = writer.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	onlineCaptureTestInstall(t, pool)
	writer, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Rollback(ctx) }()
	if _, err = writer.Exec(ctx, `UPDATE keyed SET payload='pending low-ID writer' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	if _, err = SealOnlineCapture(ctx, pool, onlineCaptureTestRun); err == nil || time.Since(start) > 4*time.Second {
		t.Fatalf("seal did not wait for existing writer err=%v duration=%v", err, time.Since(start))
	}
	var sealed bool
	if err = pool.QueryRow(ctx, `SELECT sealed FROM v3_migration_capture.config`).Scan(&sealed); err != nil || sealed {
		t.Fatalf("failed seal partially committed sealed=%v err=%v", sealed, err)
	}
	if err = writer.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = SealOnlineCapture(ctx, pool, onlineCaptureTestRun); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineCaptureRowSecurityChangesRejectSyncAndSeal(t *testing.T) {
	for _, drift := range []struct {
		name string
		sql  string
	}{
		{"enable", "ALTER TABLE migration_source.logs ENABLE ROW LEVEL SECURITY"},
		{"force", "ALTER TABLE migration_source.logs FORCE ROW LEVEL SECURITY"},
		{"using", "ALTER POLICY capture_read ON migration_source.logs USING (user_id=999)"},
		{"check", "ALTER POLICY capture_read ON migration_source.logs WITH CHECK (user_id=999)"},
		{"roles", "ALTER POLICY capture_read ON migration_source.logs TO postgres"},
		{"permissive", "DROP POLICY capture_read ON migration_source.logs; CREATE POLICY capture_read ON migration_source.logs AS RESTRICTIVE USING (true)"},
	} {
		t.Run(drift.name, func(t *testing.T) {
			m, source, target, opts := onlineMigrationFixture(t, false)
			ctx := context.Background()
			onlineCaptureTestExec(t, source, "CREATE POLICY capture_read ON migration_source.logs USING (true)")
			onlineMigrationReady(t, m, opts)
			onlineCaptureTestExec(t, source, "UPDATE migration_source.logs SET quota=55 WHERE id=51")
			onlineCaptureTestExec(t, source, drift.sql)
			if _, err := m.SyncOnline(ctx, opts); err == nil || !strings.Contains(err.Error(), "DDL drifted") {
				t.Fatalf("sync accepted changed row security: %v", err)
			}
			if _, err := SealOnlineCapture(ctx, source, opts.RunID); err == nil || !strings.Contains(err.Error(), "DDL drifted") {
				t.Fatalf("seal accepted changed row security: %v", err)
			}
			var pending int64
			if err := source.QueryRow(ctx, "SELECT count(*) FROM v3_migration_capture.events WHERE NOT acked").Scan(&pending); err != nil || pending != 1 {
				t.Fatalf("row-security drift incorrectly acknowledged source changes pending=%d err=%v", pending, err)
			}
			var amount int64
			if err := target.QueryRow(ctx, "SELECT amount FROM "+onlineStage("v3_audit.events")+" WHERE id=51").Scan(&amount); err != nil || amount != 100 {
				t.Fatalf("row-security drift changed staging amount=%d err=%v", amount, err)
			}
			var sealed bool
			if err := source.QueryRow(ctx, "SELECT sealed FROM v3_migration_capture.config WHERE singleton").Scan(&sealed); err != nil || sealed {
				t.Fatalf("refused seal partially committed sealed=%v err=%v", sealed, err)
			}
		})
	}
}

func TestOnlineCapturePartitionRowSecurityIncludedInFingerprint(t *testing.T) {
	pool := onlineCaptureTestPool(t)
	ctx := context.Background()
	onlineCaptureTestExec(t, pool, "CREATE TABLE partitioned(id bigint PRIMARY KEY) PARTITION BY RANGE(id); CREATE TABLE partition_a PARTITION OF partitioned FOR VALUES FROM (0) TO (100)")
	onlineCaptureTestInstall(t, pool)
	onlineCaptureTestExec(t, pool, "CREATE POLICY direct_partition_read ON partition_a USING (id>0)")
	if _, err := SealOnlineCapture(ctx, pool, onlineCaptureTestRun); err == nil || !strings.Contains(err.Error(), "DDL drifted") {
		t.Fatalf("partition policy drift was ignored: %v", err)
	}
}

func TestOnlineCapturePolicyFingerprintUsesRoleNamesRatherThanOIDs(t *testing.T) {
	pool := onlineCaptureTestPool(t)
	ctx := context.Background()
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	role := "capture_policy_" + hex.EncodeToString(random)
	quoted := pgx.Identifier{role}.Sanitize()
	policy := "CREATE POLICY named_role ON keyed TO " + quoted + " USING (id>0) WITH CHECK (id<100)"
	onlineCaptureTestExec(t, pool, "CREATE ROLE "+quoted+"; CREATE TABLE keyed(id bigint PRIMARY KEY); ALTER TABLE keyed ENABLE ROW LEVEL SECURITY; ALTER TABLE keyed FORCE ROW LEVEL SECURITY; "+policy)
	onlineCaptureTestInstall(t, pool)
	var before, after uint32
	if err := pool.QueryRow(ctx, "SELECT oid FROM pg_roles WHERE rolname=$1", role).Scan(&before); err != nil {
		t.Fatal(err)
	}
	onlineCaptureTestExec(t, pool, "DROP POLICY named_role ON keyed; DROP ROLE "+quoted+"; CREATE ROLE "+quoted+"; "+policy)
	if err := pool.QueryRow(ctx, "SELECT oid FROM pg_roles WHERE rolname=$1", role).Scan(&after); err != nil || before == after {
		t.Fatalf("fixture did not reconstruct role with another OID before=%d after=%d err=%v", before, after, err)
	}
	if err := onlineCaptureTestValidate(pool); err != nil {
		t.Fatalf("equivalent policy/role definitions with new catalog OIDs changed the restore-stable fingerprint: %v", err)
	}
}
