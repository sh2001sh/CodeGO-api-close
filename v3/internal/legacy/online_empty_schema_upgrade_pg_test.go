//go:build pgintegration

package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/migrations"
)

func onlineEmptyUpgradeFixture(t *testing.T, market bool) (*Importer, *pgxpool.Pool, *pgxpool.Pool, OnlineEmptySchemaUpgradeOptions) {
	t.Helper()
	ctx := context.Background()
	source, target, crypto := importTestDBBefore(t, migrations.ExactPriceMigration)
	historyFixture(t, source)
	if market {
		onlineCaptureTestExec(t, source, `INSERT INTO migration_source.users VALUES(8,'consumer','bcrypt-placeholder',1,1,'default',0,0,'{}')`)
		seedChannelMarketFixture(t, source)
		for _, table := range channelMarketSourceTables {
			if !cmStreamedTable(table) {
				continue
			}
			keys := "id"
			if table == "pelican_artifacts" {
				keys = "group_id,model"
			}
			onlineCaptureTestExec(t, source, "ALTER TABLE "+pgx.Identifier{"marketplace", table}.Sanitize()+" ADD PRIMARY KEY("+keys+")")
		}
	}
	// Track the real predecessor bytes, exactly as the original embedded
	// installer did, while deliberately leaving the new migration unapplied.
	onlineCaptureTestExec(t, target, `CREATE TABLE v3_platform.embedded_schema_revisions(name text PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`)
	names, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name >= migrations.ExactPriceMigration {
			continue
		}
		sql, err := migrations.Read(name)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256([]byte(sql))
		if _, err := target.Exec(ctx, `INSERT INTO v3_platform.embedded_schema_revisions(name,checksum)VALUES($1,$2)`, name, hex.EncodeToString(hash[:])); err != nil {
			t.Fatal(err)
		}
	}
	m := NewImporter(source, target, crypto)
	opts := OnlineEmptySchemaUpgradeOptions{OnlineOptions: OnlineOptions{RunID: "online-empty-schema-upgrade-20261010", SourceAdmin: source}}
	if report, err := m.PrepareOnline(ctx, opts.OnlineOptions, true); err != nil || !report.Applied {
		t.Fatalf("old schema prepare: %+v %v", report, err)
	}
	if err := target.QueryRow(ctx, `SELECT target_shape,capture_hash FROM v3_migration_online.run WHERE singleton`).Scan(&opts.ExpectedTargetShape, &opts.ExpectedCaptureHash); err != nil {
		t.Fatal(err)
	}
	sql, err := migrations.Read(migrations.ExactPriceMigration)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(sql))
	opts.ExpectedMigrationSHA256 = hex.EncodeToString(hash[:])
	return m, source, target, opts
}

func onlineUpgradeOwnedMutation(t *testing.T, target *pgxpool.Pool, runID, sql string) {
	t.Helper()
	tx, err := target.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(context.Background(), `SELECT set_config('codego.online_writer',$1,true)`, runID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(context.Background(), sql); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func onlineUpgradeSourceDigest(t *testing.T, source *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	rows, err := source.Query(ctx, `SELECT n.nspname,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname IN('migration_source','billing','marketplace','v3_migration_capture') AND c.relkind IN('r','p') ORDER BY 1,2`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []pgx.Identifier
	for rows.Next() {
		var schema, name string
		if err := rows.Scan(&schema, &name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, pgx.Identifier{schema, name})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	for _, table := range tables {
		var raw []byte
		if err := source.QueryRow(ctx, "SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb) FROM "+table.Sanitize()+" t").Scan(&raw); err != nil {
			t.Fatal(err)
		}
		hash.Write([]byte(table.Sanitize()))
		hash.Write(raw)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func onlineUpgradeAssertSourceUnchanged(t *testing.T, source *pgxpool.Pool, before string) {
	t.Helper()
	if after := onlineUpgradeSourceDigest(t, source); after != before {
		t.Fatalf("schema-only upgrade changed source rows/capture: before=%s after=%s", before, after)
	}
}

func TestOnlineEmptySchemaUpgradeAtomicAndPreservesLegacyPrices(t *testing.T) {
	for _, afterCopy := range []bool{false, true} {
		t.Run(map[bool]string{false: "original_legacy_prices", true: "prices_changed_after_copy"}[afterCopy], func(t *testing.T) {
			ctx := context.Background()
			m, source, target, opts := onlineEmptyUpgradeFixture(t, true)
			onlineCaptureTestExec(t, source, `INSERT INTO migration_source.users
 SELECT n,'consumer-'||n,'bcrypt-placeholder',1,1,'default',0,0,'{}' FROM generate_series(9,12) n;
 INSERT INTO marketplace.user_multipliers(id,channel_id,user_id,multiplier,created_at,updated_at) SELECT 94+n,channel_id,8+n,
 CASE n WHEN 1 THEN 1e-14 WHEN 2 THEN 1e-20 WHEN 3 THEN 1e-13 ELSE 1e-8 END,created_at,updated_at
 FROM marketplace.user_multipliers CROSS JOIN generate_series(1,4) n`)
			setPrices := func() {
				onlineCaptureTestExec(t, source, `UPDATE marketplace.groups SET multiplier=0,lifecycle_status='suspended';
 UPDATE marketplace.channels SET status='suspended';
 UPDATE migration_source.channels SET status=2 WHERE id=13;
 UPDATE marketplace.user_multipliers SET multiplier=0.00000001 WHERE id=94;
 UPDATE marketplace.time_range_multipliers SET multiplier=1e-20;
 UPDATE marketplace.multiplier_notices SET previous_multiplier=0.13114514191981,multiplier=0.000000000000000000000000000000000000000000000000000000000000001;
 UPDATE marketplace.bargain_requests SET proposed_multiplier=0.114514191981,status='approved';
 UPDATE marketplace.multiplier_trend_snapshots SET multiplier=0`)
			}
			if !afterCopy {
				setPrices()
			}
			sourceBefore := onlineUpgradeSourceDigest(t, source)
			report, err := m.UpgradeEmptyOnlineSchema(ctx, opts)
			if err != nil || !report.Applied || report.TargetShape == opts.ExpectedTargetShape || report.PreviousTargetShape != opts.ExpectedTargetShape || report.CaptureHash != opts.ExpectedCaptureHash || report.SourceWrites != 0 || report.Phase != "copying" {
				t.Fatalf("atomic upgrade: %+v %v", report, err)
			}
			onlineUpgradeAssertSourceUnchanged(t, source, sourceBefore)
			if _, err := m.UpgradeEmptyOnlineSchema(ctx, opts); err == nil {
				t.Fatal("stale schema approval accepted")
			}
			opts.ExpectedTargetShape = report.TargetShape
			if report, err := m.UpgradeEmptyOnlineSchema(ctx, opts); err != nil || report.Applied || report.TargetShape != opts.ExpectedTargetShape {
				t.Fatalf("approved idempotent retry: %+v %v", report, err)
			}
			onlineUpgradeAssertSourceUnchanged(t, source, sourceBefore)
			if report, err := m.CopyOnline(ctx, opts.OnlineOptions); err != nil || report.Phase != "copied" {
				t.Fatalf("copy after real 107 to 108 upgrade: %+v %v", report, err)
			}
			if afterCopy {
				setPrices()
			}
			onlineMigrationSync(t, m, opts.OnlineOptions)
			if report, err := m.VerifyOnline(ctx, opts.OnlineOptions); err != nil || report.Phase != "verified" {
				t.Fatalf("verify precise live prices: %+v %v", report, err)
			}
			if _, err := SealOnlineCapture(ctx, source, opts.RunID); err != nil {
				t.Fatal(err)
			}
			if report, err := m.FinalizeOnline(ctx, opts.OnlineOptions); err != nil || !report.Applied {
				t.Fatalf("finalize precise prices: %+v %v", report, err)
			}
			if report, err := m.Check(ctx); err != nil || len(report.Issues) != 0 {
				t.Fatalf("independent exact check: %+v %v", report, err)
			}
			var exact bool
			if err := target.QueryRow(ctx, `SELECT
 (SELECT multiplier_ppm=0 AND lifecycle_status='paused' FROM v3_channelmarket.groups WHERE id='legacy-group-201')
 AND (SELECT multiplier=0 FROM v3_catalog.groups WHERE name='fixture_market_internal')
 AND (SELECT multiplier_ppm=0.01 FROM v3_channelmarket.user_multipliers WHERE legacy_id=94)
 AND (SELECT count(*)=5 AND bool_and(multiplier_ppm=CASE user_id WHEN 8 THEN 0.01 WHEN 9 THEN 1e-8 WHEN 10 THEN 1e-14 WHEN 11 THEN 1e-7 WHEN 12 THEN 0.01 END) FROM v3_channelmarket.user_multipliers)
 AND (SELECT previous_ppm=131145.14191981 AND multiplier_ppm=1e-57 FROM v3_channelmarket.multiplier_notices WHERE id=95)
 AND (SELECT proposed_ppm=114514.191981 AND status='accepted' FROM v3_channelmarket.bargain_requests WHERE id='bargain-201')
 AND (SELECT multiplier_ppm=0 FROM v3_channelmarket.multiplier_trend_snapshots WHERE id=96)
 AND (SELECT multiplier_ppm=1e-14 FROM v3_channelmarket.time_range_multipliers WHERE id='window-201')
 AND (SELECT gross_micro=200 AND net_micro=190 FROM v3_channelmarket.settlements WHERE id='settlement-201')`).Scan(&exact); err != nil || !exact {
				t.Fatalf("legacy price/state or money changed: %t %v", exact, err)
			}
			onlineCaptureTestExec(t, target, `UPDATE v3_channelmarket.multiplier_notices SET multiplier_ppm=0 WHERE id=95`)
			if report, err := m.Check(ctx); err == nil || len(report.Issues) == 0 {
				t.Fatal("independent check missed exact fractional price tampering")
			}
		})
	}
}

func TestOnlineEmptySchemaUpgradeRefusesUsedOrDriftingState(t *testing.T) {
	cases := []struct {
		name                 string
		targetSQL, sourceSQL string
		change               func(*OnlineEmptySchemaUpgradeOptions)
	}{
		{name: "progress", targetSQL: `UPDATE v3_migration_online.progress SET copied=1 WHERE name='logs'`},
		{name: "cursor", targetSQL: `UPDATE v3_migration_online.progress SET cursor='{}' WHERE name='logs'`},
		{name: "completed", targetSQL: `UPDATE v3_migration_online.progress SET complete=true WHERE name='logs'`},
		{name: "accounts", targetSQL: `INSERT INTO v3_migration_online.account_ids VALUES(1,'user',7,'wallet')`},
		{name: "totals", targetSQL: `INSERT INTO v3_migration_online.totals VALUES('used',1)`},
		{name: "dependencies", targetSQL: `UPDATE v3_migration_online.run SET dependencies='{"used":true}'`},
		{name: "verified", targetSQL: `UPDATE v3_migration_online.run SET verified_at=now()`},
		{name: "business", targetSQL: `INSERT INTO v3_catalog.groups(name)VALUES('used')`},
		{name: "seeds", targetSQL: `UPDATE v3_commerce.referral_consumption_policy SET spent_credits=0,reward_ppm=0`},
		{name: "staged_business", targetSQL: `INSERT INTO v3_migration_online.v3_audit__events VALUES(1,7,now(),1,'used','','','',0,0,0,0,false,13,11,'default','','','', '{}'::jsonb)`},
		{name: "outbox_seed", targetSQL: `UPDATE v3_platform.cache_invalidation_outbox SET entity_id='business'`},
		{name: "outbox_lease", targetSQL: `UPDATE v3_platform.cache_invalidation_outbox SET attempts=1`},
		{name: "missing_progress", targetSQL: `DELETE FROM v3_migration_online.progress WHERE name='logs'`},
		{name: "foreign_keys", targetSQL: `DELETE FROM v3_migration_online.foreign_keys`},
		{name: "object_names", targetSQL: `UPDATE v3_migration_online.run SET object_names='[]'`},
		{name: "target_identity", targetSQL: `UPDATE v3_migration_online.run SET target_identity='changed'`},
		{name: "shape_drift", targetSQL: `ALTER TABLE v3_catalog.groups ADD COLUMN stray text`},
		{name: "acknowledged", sourceSQL: `UPDATE migration_source.users SET username='new'; UPDATE v3_migration_capture.events SET acked=true`},
		{name: "sealed", sourceSQL: `UPDATE v3_migration_capture.config SET sealed=true`},
		{name: "wrong_shape_approval", change: func(o *OnlineEmptySchemaUpgradeOptions) { o.ExpectedTargetShape = strings.Repeat("a", 64) }},
		{name: "wrong_capture_approval", change: func(o *OnlineEmptySchemaUpgradeOptions) { o.ExpectedCaptureHash = strings.Repeat("a", 64) }},
		{name: "wrong_migration_approval", change: func(o *OnlineEmptySchemaUpgradeOptions) { o.ExpectedMigrationSHA256 = strings.Repeat("a", 64) }},
		{name: "predecessor_checksum", targetSQL: `UPDATE v3_platform.embedded_schema_revisions SET checksum='changed' WHERE name='20261009000107_ledger_history_archive.sql'`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			m, source, target, opts := onlineEmptyUpgradeFixture(t, false)
			if test.targetSQL != "" {
				onlineUpgradeOwnedMutation(t, target, opts.RunID, test.targetSQL)
			}
			if test.sourceSQL != "" {
				onlineCaptureTestExec(t, source, test.sourceSQL)
			}
			if test.change != nil {
				test.change(&opts)
			}
			sourceBefore := onlineUpgradeSourceDigest(t, source)
			var before string
			if err := target.QueryRow(context.Background(), `SELECT target_shape FROM v3_migration_online.run`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if report, err := m.UpgradeEmptyOnlineSchema(context.Background(), opts); err == nil || report.Applied {
				t.Fatalf("unsafe empty upgrade accepted: %+v %v", report, err)
			}
			onlineUpgradeAssertSourceUnchanged(t, source, sourceBefore)
			var unchanged bool
			if err := target.QueryRow(context.Background(), `SELECT target_shape=$1 AND phase='copying'
 AND NOT EXISTS(SELECT 1 FROM v3_platform.embedded_schema_revisions WHERE name=$2)
 AND (SELECT atttypid='int8'::regtype FROM pg_attribute WHERE attrelid='v3_channelmarket.user_multipliers'::regclass AND attname='multiplier_ppm') FROM v3_migration_online.run`, before, migrations.ExactPriceMigration).Scan(&unchanged); err != nil || !unchanged {
				t.Fatalf("failure changed schema or run: %t %v", unchanged, err)
			}
		})
	}
}

func TestOnlineEmptySchemaUpgradeLockConflictsAndDDLFailureRollback(t *testing.T) {
	ctx := context.Background()
	m, source, target, opts := onlineEmptyUpgradeFixture(t, true)
	before := onlineUpgradeSourceDigest(t, source)
	for _, test := range []struct {
		name, sql string
		pool      *pgxpool.Pool
	}{
		{"online_operation", `SELECT pg_advisory_xact_lock(738301032)`, target},
		{"schema_runner", `SELECT pg_advisory_xact_lock(738301030)`, target},
		{"target_business_writer", `LOCK TABLE v3_catalog.groups IN ROW EXCLUSIVE MODE`, target},
		{"source_capture_setup_or_seal", `SELECT pg_advisory_xact_lock(738301032)`, source},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := test.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if _, err := tx.Exec(ctx, test.sql); err != nil {
				t.Fatal(err)
			}
			if report, err := m.UpgradeEmptyOnlineSchema(ctx, opts); err == nil || report.Applied {
				t.Fatalf("concurrent operation accepted: %+v %v", report, err)
			}
			onlineUpgradeAssertSourceUnchanged(t, source, before)
		})
	}
	// The failure occurs after the earlier formal ALTERs have executed, rather
	// than merely rejecting input before DDL starts. The event trigger is outside
	// v3 schemas and does not alter the approved target shape.
	onlineCaptureTestExec(t, target, `CREATE FUNCTION public.fail_exact_price_upgrade() RETURNS event_trigger LANGUAGE plpgsql AS $$
 BEGIN
 IF current_setting('codego.online_writer',true) IS NOT NULL
 AND EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='v3_channelmarket.user_multipliers'::regclass AND attname='multiplier_ppm' AND atttypid='numeric'::regtype)
 THEN RAISE EXCEPTION 'fixture_mid_upgrade_failure'; END IF;
 END $$;
 CREATE EVENT TRIGGER fail_exact_price_upgrade ON ddl_command_start WHEN TAG IN('ALTER TABLE') EXECUTE FUNCTION public.fail_exact_price_upgrade()`)
	if report, err := m.UpgradeEmptyOnlineSchema(ctx, opts); err == nil || report.Applied || !strings.Contains(err.Error(), "fixture_mid_upgrade_failure") {
		t.Fatalf("mid-DDL failure was not exercised: %+v %v", report, err)
	}
	onlineUpgradeAssertSourceUnchanged(t, source, before)
	onlineCaptureTestExec(t, target, `DROP EVENT TRIGGER fail_exact_price_upgrade; DROP FUNCTION public.fail_exact_price_upgrade()`)
	tx, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := onlineTargetSchemaHash(ctx, tx)
	_ = tx.Rollback(ctx)
	if err != nil || actual != opts.ExpectedTargetShape {
		t.Fatalf("failed DDL changed original actual schema: %s %v", actual, err)
	}
	var intact bool
	if err := target.QueryRow(ctx, `SELECT target_shape=$1 AND phase='copying' AND dependencies='{}'::jsonb AND verified_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM v3_platform.embedded_schema_revisions WHERE name=$2)
 AND NOT EXISTS(SELECT 1 FROM v3_migration_online.progress WHERE cursor IS NOT NULL OR complete OR copied<>0)
 FROM v3_migration_online.run`, opts.ExpectedTargetShape, migrations.ExactPriceMigration).Scan(&intact); err != nil || !intact {
		t.Fatalf("failed DDL changed revisions or run: %t %v", intact, err)
	}
	if report, err := m.UpgradeEmptyOnlineSchema(ctx, opts); err != nil || !report.Applied {
		t.Fatalf("rollback did not release migration locks: %+v %v", report, err)
	}
	onlineUpgradeAssertSourceUnchanged(t, source, before)
	t.Log("online/schema/table/source lock conflicts rejected; mid-DDL schema, revisions, ownership and progress rolled back; all source business/capture rows unchanged")
}
