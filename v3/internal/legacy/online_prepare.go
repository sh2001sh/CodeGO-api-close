package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const onlineLock int64 = 738301032

func onlineCaptureBindingHash(c OnlineCaptureReport) (string, error) {
	// Estimates change under normal writes. OIDs and table shapes bind the
	// running source; recovery bundles deliberately use their own shape hash.
	type shape struct {
		Name string
		OID  uint32
		Keys []string
		SHA  string
	}
	var tables []shape
	for _, t := range c.Tables {
		tables = append(tables, shape{t.Name, t.RelationOID, t.Keys, t.SchemaFingerprint})
	}
	b, err := json.Marshal(tables)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func (m *Importer) PrepareOnline(ctx context.Context, opts OnlineOptions, apply bool) (OnlineReport, error) {
	r := OnlineReport{RunID: opts.RunID, Phase: "preview", Tables: map[string]int64{}}
	if m.source == nil || m.pool == nil {
		return r, errors.New("legacy: independent online source and target required")
	}
	admin := m.source
	if apply {
		admin = opts.SourceAdmin
		if admin == nil {
			return r, errors.New("legacy: online capture setup requires its source admin connection")
		}
		if err := ValidateOnlineCaptureSource(ctx, m.source, admin); err != nil {
			return r, err
		}
	}
	identityTx, err := m.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return r, err
	}
	defer func() { _ = identityTx.Rollback(ctx) }()
	targetIdentity, identityErr := onlineDatabaseIdentity(ctx, identityTx)
	if identityErr != nil {
		return r, identityErr
	}
	var sourceName, targetName string
	if err = m.source.QueryRow(ctx, "SELECT current_database()").Scan(&sourceName); err != nil {
		return r, err
	}
	if err = identityTx.QueryRow(ctx, "SELECT current_database()").Scan(&targetName); err != nil {
		return r, err
	}
	if sourceName == targetName {
		return r, errors.New("legacy: online target must use an independent database name")
	}
	// Reject an already used target before installing or permanently binding
	// any source capture. Repeat these checks under the target writer lock below.
	var prepared bool
	if err = identityTx.QueryRow(ctx, "SELECT to_regclass('v3_migration_online.run') IS NOT NULL").Scan(&prepared); err != nil {
		return r, err
	}
	if prepared {
		var runID, phase, identity string
		if err = identityTx.QueryRow(ctx, "SELECT run_id,phase,target_identity FROM v3_migration_online.run WHERE singleton").Scan(&runID, &phase, &identity); err != nil {
			return r, err
		}
		if runID != opts.RunID || phase == "finalized" || identity != targetIdentity {
			return r, errors.New("legacy: online target belongs to another or completed migration")
		}
		if err = onlineValidateTargetSchema(ctx, identityTx); err != nil {
			return r, err
		}
	} else {
		for _, table := range []string{"v3_identity.users", "v3_identity.api_keys", "v3_catalog.channels", "v3_billing.accounts", "v3_billing.ledger_entries"} {
			var populated bool
			if err = identityTx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+")").Scan(&populated); err != nil {
				return r, err
			}
			if populated {
				return r, errors.New("legacy: online preparation requires an unused target")
			}
		}
	}
	_ = identityTx.Rollback(ctx)
	capture, err := SetupOnlineCapture(ctx, admin, opts.RunID, apply)
	if err != nil {
		return r, err
	}
	if apply {
		if err = BindOnlineCaptureTarget(ctx, admin, opts.RunID, targetIdentity); err != nil {
			return r, err
		}
	}
	source, err := m.source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return r, err
	}
	defer func() { _ = source.Rollback(ctx) }()
	sources, err := discoverSources(ctx, source)
	if err != nil {
		return r, err
	}
	specs := onlineSpecs(sources)
	if err = validateOnlineSpecs(capture, specs); err != nil {
		return r, err
	}
	if !apply {
		for _, s := range specs {
			r.Tables[s.name] = 0
		}
		return r, nil
	}
	hash, err := onlineCaptureBindingHash(capture)
	if err != nil {
		return r, err
	}
	target, err := m.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return r, err
	}
	defer func() { _ = target.Rollback(ctx) }()
	if _, err = target.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", onlineLock); err != nil {
		return r, err
	}
	var sourceDB, targetDB string
	if err = source.QueryRow(ctx, "SELECT current_database()").Scan(&sourceDB); err != nil {
		return r, err
	}
	if err = target.QueryRow(ctx, "SELECT current_database()").Scan(&targetDB); err != nil {
		return r, err
	}
	if sourceDB == targetDB {
		return r, errors.New("legacy: online target must use an independent database name")
	}
	var existing bool
	if err = target.QueryRow(ctx, "SELECT to_regclass('v3_migration_online.run') IS NOT NULL").Scan(&existing); err != nil {
		return r, err
	}
	if existing {
		var id, stored, phase string
		if err = target.QueryRow(ctx, "SELECT run_id,capture_hash,phase FROM v3_migration_online.run WHERE singleton").Scan(&id, &stored, &phase); err != nil {
			return r, err
		}
		if id != opts.RunID || stored != hash || phase == "finalized" {
			return r, errors.New("legacy: online target belongs to another capture or completed migration")
		}
		if err = onlineValidateTargetSchema(ctx, target); err != nil {
			return r, err
		}
		r.Phase = phase
		r.Applied = true
		return r, nil
	}
	for _, table := range []string{"v3_identity.users", "v3_identity.api_keys", "v3_catalog.channels", "v3_billing.accounts", "v3_billing.ledger_entries"} {
		var populated bool
		if err = target.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+")").Scan(&populated); err != nil {
			return r, err
		}
		if populated {
			return r, errors.New("legacy: online preparation requires an unused target")
		}
	}
	_, err = target.Exec(ctx, `CREATE SCHEMA v3_migration_online;
	 CREATE TABLE v3_migration_online.run(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),run_id text NOT NULL,capture_hash text NOT NULL,target_identity text NOT NULL,target_shape text NOT NULL DEFAULT '',object_names jsonb NOT NULL DEFAULT '[]',phase text NOT NULL,dependencies jsonb NOT NULL DEFAULT '{}',verified_at timestamptz);
	 CREATE TABLE v3_migration_online.progress(name text PRIMARY KEY,cursor jsonb,complete boolean NOT NULL DEFAULT false,copied bigint NOT NULL DEFAULT 0);
	 CREATE TABLE v3_migration_online.account_ids(id bigint PRIMARY KEY,owner_type text NOT NULL,owner_id bigint NOT NULL,kind text NOT NULL,UNIQUE(owner_type,owner_id,kind));
	 CREATE TABLE v3_migration_online.totals(name text PRIMARY KEY,value numeric NOT NULL);
	 CREATE TABLE v3_migration_online.retired_rows(name text NOT NULL,row_key jsonb NOT NULL,metrics jsonb NOT NULL,PRIMARY KEY(name,row_key));
	 CREATE TABLE v3_migration_online.foreign_keys(table_name text NOT NULL,constraint_name text NOT NULL,definition text NOT NULL,PRIMARY KEY(table_name,constraint_name));
	 CREATE SCHEMA v3_migration_previous;
	 REVOKE ALL ON SCHEMA v3_migration_online,v3_migration_previous FROM PUBLIC`)
	if err != nil {
		return r, err
	}
	if _, err = target.Exec(ctx, "INSERT INTO v3_migration_online.run(run_id,capture_hash,target_identity,phase)VALUES($1,$2,$3,'copying')", opts.RunID, hash, targetIdentity); err != nil {
		return r, err
	}
	var tables []string
	for _, s := range specs {
		tables = append(tables, s.targets...)
		if _, err = target.Exec(ctx, "INSERT INTO v3_migration_online.progress(name)VALUES($1)", s.name); err != nil {
			return r, err
		}
	}
	// Small historical account evidence is refreshed independently of the large ledger.
	tables = append(tables, "v3_billing.historical_accounts")
	for _, table := range tables {
		// LIKE preserves columns and indexes, but not custom access policies,
		// owners or grants. Refuse these rather than silently discard them.
		var unsupported bool
		if err = target.QueryRow(ctx, `SELECT c.relowner<>(SELECT oid FROM pg_roles WHERE rolname=current_user) OR c.relacl IS NOT NULL OR c.relrowsecurity OR c.relforcerowsecurity OR c.relpersistence<>'p' OR c.reloptions IS NOT NULL OR c.reltablespace<>0 OR COALESCE((SELECT amname FROM pg_am WHERE oid=c.relam),'heap')<>'heap' OR EXISTS(SELECT 1 FROM pg_policy WHERE polrelid=c.oid) OR EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=c.oid AND attacl IS NOT NULL) OR EXISTS(SELECT 1 FROM pg_rewrite WHERE ev_class=c.oid) FROM pg_class c WHERE c.oid=$1::regclass`, table).Scan(&unsupported); err != nil {
			return r, err
		}
		if unsupported {
			return r, fmt.Errorf("legacy: online native table %s has unsupported custom access rules", table)
		}
		if table == "v3_billing.usage_logs" {
			if err = target.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_partition_tree('v3_billing.usage_logs') WHERE level>0 AND (level<>1 OR relid<>'v3_billing.usage_logs_default'::regclass))`).Scan(&unsupported); err != nil {
				return r, err
			}
			if unsupported {
				return r, errors.New("legacy: online target already has custom usage partitions; use an unused schema")
			}
		}
		var populated bool
		if err = target.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+")").Scan(&populated); err != nil {
			return r, err
		}
		if populated {
			return r, fmt.Errorf("legacy: online native table %s is already populated", table)
		}
		query := "CREATE TABLE " + onlineStage(table) + " (LIKE " + table + " INCLUDING ALL)"
		if table == "v3_billing.usage_logs" {
			query += " PARTITION BY RANGE(created_at)"
		}
		if _, err = target.Exec(ctx, query); err != nil {
			return r, err
		}
		if table == "v3_billing.usage_logs" {
			if _, err = target.Exec(ctx, "CREATE TABLE "+onlineStage(table+"_default")+" PARTITION OF "+onlineStage(table)+" DEFAULT"); err != nil {
				return r, err
			}
		}
		if _, err = target.Exec(ctx, `INSERT INTO v3_migration_online.foreign_keys SELECT $1::text,c.conname,pg_get_constraintdef(c.oid) FROM pg_constraint c WHERE c.conrelid=($1::text)::regclass AND c.contype='f'`, table); err != nil {
			return r, err
		}
		// Inbound relationships outside the swap set cannot follow a different
		// table OID. Reject a new schema dependency rather than detach it.
		var external bool
		if err = target.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE contype='f' AND confrelid=$1::regclass AND conrelid::regclass::text<>ALL($2::text[]))`, table, tables).Scan(&external); err != nil {
			return r, err
		}
		if external {
			return r, fmt.Errorf("legacy: online table %s has an unsupported incoming relationship", table)
		}
		var triggers bool
		if err = target.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=$1::regclass AND NOT tgisinternal)", table).Scan(&triggers); err != nil {
			return r, err
		}
		if triggers {
			return r, fmt.Errorf("legacy: online table %s has an unsupported application trigger", table)
		}
	}
	if sources["logs"] != "" {
		if _, err = target.Exec(ctx, "CREATE INDEX online_log_group_idx ON "+onlineStage("v3_audit.events")+"(created_at,user_id,request_id) WHERE event_type=2 AND request_id<>''"); err != nil {
			return r, err
		}
	}
	// Only this migration's transactions may mutate staging. The run token is
	// set locally, never in the environment of a serving application.
	_, err = target.Exec(ctx, `CREATE FUNCTION v3_migration_online.guard() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$ BEGIN
	 IF current_setting('codego.online_writer',true) IS DISTINCT FROM (SELECT run_id FROM v3_migration_online.run WHERE singleton) THEN RAISE EXCEPTION 'online staging requires migration ownership'; END IF; RETURN NULL; END $$`)
	if err != nil {
		return r, err
	}
	if _, err = target.Exec(ctx, "SELECT set_config('codego.online_writer',$1,true)", opts.RunID); err != nil {
		return r, err
	}
	guarded := []string{}
	for _, table := range append(tables, "v3_billing.usage_logs_default") {
		if table == "v3_billing.usage_logs_default" && sources["logs"] == "" {
			continue
		}
		guarded = append(guarded, onlineStage(table))
	}
	for _, name := range []string{"run", "progress", "account_ids", "totals", "retired_rows", "foreign_keys"} {
		guarded = append(guarded, pgx.Identifier{"v3_migration_online", name}.Sanitize())
	}
	for _, table := range guarded {
		if _, err = target.Exec(ctx, "CREATE TRIGGER online_staging_guard BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON "+table+" FOR EACH STATEMENT EXECUTE FUNCTION v3_migration_online.guard(); ALTER TABLE "+table+" ENABLE ALWAYS TRIGGER online_staging_guard"); err != nil {
			return r, err
		}
	}
	if err = onlineRememberObjectNames(ctx, target, tables); err != nil {
		return r, err
	}
	shape, err := onlineTargetSchemaHash(ctx, target)
	if err != nil {
		return r, err
	}
	if _, err = target.Exec(ctx, "UPDATE v3_migration_online.run SET target_shape=$1 WHERE singleton", shape); err != nil {
		return r, err
	}
	if err = target.Commit(ctx); err != nil {
		return r, err
	}
	r.Phase = "copying"
	r.Applied = true
	return r, nil
}

func validateOnlineSpecs(capture OnlineCaptureReport, specs []onlineSpec) error {
	for _, spec := range specs {
		found := false
		for _, table := range capture.Tables {
			if table.Relation == spec.source {
				found = true
				if strings.Join(table.Keys, "\x00") != strings.Join(spec.keys, "\x00") {
					return fmt.Errorf("legacy: online %s requires its complete expected primary key", spec.name)
				}
			}
		}
		if !found {
			return fmt.Errorf("legacy: online source %s is outside capture", spec.name)
		}
	}
	return nil
}

func onlineAuthorize(ctx context.Context, target pgx.Tx, runID string) error {
	var id, phase string
	if err := target.QueryRow(ctx, "SELECT run_id,phase FROM v3_migration_online.run WHERE singleton FOR UPDATE").Scan(&id, &phase); err != nil {
		return err
	}
	if id != runID || phase == "finalized" {
		return errors.New("legacy: online target ownership or phase changed")
	}
	_, err := target.Exec(ctx, "SELECT set_config('codego.online_writer',$1,true)", runID)
	return err
}

func onlineBindings(ctx context.Context, source pgx.Tx, target pgx.Tx, runID string) (map[string]string, []onlineSpec, error) {
	capture, err := ValidateOnlineCapture(ctx, source, runID)
	if err != nil {
		return nil, nil, err
	}
	hash, err := onlineCaptureBindingHash(capture)
	if err != nil {
		return nil, nil, err
	}
	var stored, targetIdentity string
	if err = target.QueryRow(ctx, "SELECT capture_hash,target_identity FROM v3_migration_online.run WHERE singleton AND run_id=$1", runID).Scan(&stored, &targetIdentity); err != nil {
		return nil, nil, err
	}
	if stored != hash {
		return nil, nil, errors.New("legacy: online capture identity changed")
	}
	actualIdentity, err := onlineDatabaseIdentity(ctx, target)
	if err != nil {
		return nil, nil, err
	}
	if targetIdentity != actualIdentity || capture.TargetIdentity != actualIdentity {
		return nil, nil, errors.New("legacy: online capture belongs to a different target database")
	}
	if err = onlineValidateTargetSchema(ctx, target); err != nil {
		return nil, nil, err
	}
	sources, err := discoverSources(ctx, source)
	if err != nil {
		return nil, nil, err
	}
	specs := onlineSpecs(sources)
	return sources, specs, validateOnlineSpecs(capture, specs)
}
