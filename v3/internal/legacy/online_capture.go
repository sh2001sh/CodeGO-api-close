package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const onlineCaptureSchema = "v3_migration_capture"
const onlineCaptureRowTrigger = "codego_online_capture_row"
const onlineCaptureTruncateTrigger = "codego_online_capture_no_truncate"
const onlineCaptureFenceTrigger = "codego_online_capture_write_fence"
const onlineCaptureNoPKLimit = 100000

var onlineCaptureRunID = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

type OnlineCaptureTable struct {
	Name              string   `json:"name"`
	Relation          string   `json:"relation"`
	RelationOID       uint32   `json:"relation_oid"`
	Keys              []string `json:"keys"`
	SchemaFingerprint string   `json:"schema_fingerprint"`
	EstimatedRows     int64    `json:"estimated_rows"`
}

type OnlineCaptureReport struct {
	RunID          string               `json:"run_id"`
	Applied        bool                 `json:"applied"`
	Sealed         bool                 `json:"sealed"`
	TargetIdentity string               `json:"target_identity,omitempty"`
	Tables         []OnlineCaptureTable `json:"tables"`
}

// ValidateOnlineCaptureSource rejects an administrative pool connected to any
// other real database, even if it has the same schema and capture run ID. Host
// aliases are allowed; database identity requires pg_control_system privileges.
func ValidateOnlineCaptureSource(ctx context.Context, source, admin *pgxpool.Pool) error {
	if source == nil || admin == nil {
		return errors.New("legacy: capture requires source and administrative pools")
	}
	identity := func(pool *pgxpool.Pool) (string, error) {
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return "", err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		value, err := onlineDatabaseIdentity(ctx, tx)
		if err != nil {
			return "", err
		}
		return value, tx.Commit(ctx)
	}
	actual, err := identity(source)
	if err != nil {
		return err
	}
	administrative, err := identity(admin)
	if err != nil {
		return err
	}
	if actual != administrative {
		return errors.New("legacy: source administrative connection refers to a different database")
	}
	return nil
}

// SetupOnlineCapture previews without changing the source. Installation takes
// short, bounded writer locks in one transaction, before a baseline snapshot is
// taken. Events are transactional dirty keys, not an ordered WAL: callers must
// read ALL unacknowledged events, never advance a max(id) watermark.
func SetupOnlineCapture(ctx context.Context, pool *pgxpool.Pool, runID string, apply bool) (OnlineCaptureReport, error) {
	r := OnlineCaptureReport{RunID: runID, Tables: []OnlineCaptureTable{}}
	if pool == nil || !onlineCaptureRunID.MatchString(runID) {
		return r, errors.New("legacy: capture requires a pool and a 16-64 character safe run ID")
	}
	options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	if apply {
		// READ COMMITTED ensures rediscovery sees DDL committed before our locks.
		options = pgx.TxOptions{}
	}
	tx, err := pool.BeginTx(ctx, options)
	if err != nil {
		return r, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)`, onlineCaptureSchema).Scan(&exists); err != nil {
		return r, err
	}
	if exists {
		r, err = ValidateOnlineCapture(ctx, tx, runID)
		if err == nil {
			err = tx.Commit(ctx)
		}
		return r, err
	}
	r.Tables, err = discoverOnlineCaptureTables(ctx, tx)
	if err != nil {
		return r, err
	}
	if err = validateOnlineCaptureNoPK(ctx, tx, r.Tables); err != nil {
		return r, err
	}
	if !apply {
		return r, tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='1s'; SET LOCAL standard_conforming_strings=on`); err != nil {
		return r, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(738301032)`); err != nil {
		return r, err
	}
	// Reject another installer instead of silently adopting a partial namespace.
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)`, onlineCaptureSchema).Scan(&exists); err != nil || exists {
		if err == nil {
			err = errors.New("legacy: capture namespace was concurrently installed; validate it before retrying")
		}
		return r, err
	}
	if len(r.Tables) > 0 {
		relations := make([]string, len(r.Tables))
		for i, table := range r.Tables {
			relations[i] = table.Relation
		}
		if _, err = tx.Exec(ctx, "LOCK TABLE "+strings.Join(relations, ",")+" IN SHARE ROW EXCLUSIVE MODE"); err != nil {
			return r, fmt.Errorf("legacy: source capture locks unavailable within one second: %w", err)
		}
	}
	locked, err := discoverOnlineCaptureTables(ctx, tx)
	if err != nil {
		return r, err
	}
	if !onlineCaptureTablesEqual(r.Tables, locked) {
		return r, errors.New("legacy: source DDL changed while installing capture")
	}
	if err = validateOnlineCaptureNoPK(ctx, tx, locked); err != nil {
		return r, err
	}
	if _, err = tx.Exec(ctx, `CREATE SCHEMA v3_migration_capture;
 CREATE TABLE v3_migration_capture.config(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),run_id text NOT NULL,sealed boolean NOT NULL DEFAULT false,target_identity text);
 CREATE TABLE v3_migration_capture.tables(name text PRIMARY KEY,relation_oid oid NOT NULL UNIQUE,keys text[] NOT NULL,schema_fingerprint text NOT NULL);
 CREATE TABLE v3_migration_capture.events(id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,table_name text NOT NULL REFERENCES v3_migration_capture.tables(name),row_key jsonb NOT NULL,acked boolean NOT NULL DEFAULT false);
 CREATE INDEX online_capture_pending_idx ON v3_migration_capture.events(id) WHERE NOT acked;
 CREATE INDEX online_capture_table_idx ON v3_migration_capture.events(table_name)`); err != nil {
		return r, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO v3_migration_capture.config(run_id) VALUES($1)`, runID); err != nil {
		return r, err
	}
	truncateBody := onlineCaptureTruncateBody()
	if _, err = tx.Exec(ctx, onlineCaptureCreateFunction("reject_truncate", truncateBody)); err != nil {
		return r, err
	}
	if _, err = tx.Exec(ctx, onlineCaptureCreateFunction("write_fence", onlineCaptureFenceBody())); err != nil {
		return r, err
	}
	for _, table := range r.Tables {
		if _, err = tx.Exec(ctx, `INSERT INTO v3_migration_capture.tables(name,relation_oid,keys,schema_fingerprint) VALUES($1,$2,$3,$4)`, table.Name, table.RelationOID, table.Keys, table.SchemaFingerprint); err != nil {
			return r, err
		}
		fn := onlineCaptureFunctionName(table.Name)
		if _, err = tx.Exec(ctx, onlineCaptureCreateFunction(fn, onlineCaptureRowBody(table))); err != nil {
			return r, err
		}
		if _, err = tx.Exec(ctx, "CREATE TRIGGER "+pgx.Identifier{onlineCaptureRowTrigger}.Sanitize()+" AFTER INSERT OR UPDATE OR DELETE ON "+table.Relation+" FOR EACH ROW EXECUTE FUNCTION "+pgx.Identifier{onlineCaptureSchema, fn}.Sanitize()+"()"); err != nil {
			return r, err
		}
		// Row triggers on partitioned roots are cloned by PostgreSQL. Statement
		// TRUNCATE triggers are not; every existing descendant needs its guard.
		descendants, err := onlineCaptureDescendants(ctx, tx, table.RelationOID)
		if err != nil {
			return r, err
		}
		for _, relation := range descendants {
			if _, err = tx.Exec(ctx, "ALTER TABLE "+relation+" ENABLE ALWAYS TRIGGER "+pgx.Identifier{onlineCaptureRowTrigger}.Sanitize()); err != nil {
				return r, err
			}
			if _, err = tx.Exec(ctx, "CREATE TRIGGER "+pgx.Identifier{onlineCaptureTruncateTrigger}.Sanitize()+" BEFORE TRUNCATE ON "+relation+" FOR EACH STATEMENT EXECUTE FUNCTION v3_migration_capture.reject_truncate()"); err != nil {
				return r, err
			}
			if _, err = tx.Exec(ctx, "ALTER TABLE "+relation+" ENABLE ALWAYS TRIGGER "+pgx.Identifier{onlineCaptureTruncateTrigger}.Sanitize()); err != nil {
				return r, err
			}
			if len(table.Keys) == 0 {
				if _, err = tx.Exec(ctx, "CREATE TRIGGER "+pgx.Identifier{onlineCaptureFenceTrigger}.Sanitize()+" BEFORE INSERT OR UPDATE OR DELETE ON "+relation+" FOR EACH STATEMENT EXECUTE FUNCTION v3_migration_capture.write_fence()"); err != nil {
					return r, err
				}
				if _, err = tx.Exec(ctx, "ALTER TABLE "+relation+" ENABLE ALWAYS TRIGGER "+pgx.Identifier{onlineCaptureFenceTrigger}.Sanitize()); err != nil {
					return r, err
				}
			}
		}
	}
	if err = revokeOnlineCaptureGrants(ctx, tx); err != nil {
		return r, err
	}
	r, err = ValidateOnlineCapture(ctx, tx, runID)
	if err != nil {
		return r, err
	}
	return r, tx.Commit(ctx)
}

// ValidateOnlineCapture is SELECT-only. Run it inside the same source snapshot
// used for replay/sealing. Added/dropped tables, columns, keys, partitions and
// altered/disabled trigger functions invalidate the baseline.
func ValidateOnlineCapture(ctx context.Context, tx pgx.Tx, runID string) (OnlineCaptureReport, error) {
	r := OnlineCaptureReport{RunID: runID, Tables: []OnlineCaptureTable{}}
	if tx == nil || !onlineCaptureRunID.MatchString(runID) {
		return r, errors.New("legacy: invalid capture validation arguments")
	}
	var actual string
	var configs int64
	if err := tx.QueryRow(ctx, `SELECT count(*),COALESCE(min(run_id),'') FROM v3_migration_capture.config`).Scan(&configs, &actual); err != nil {
		return r, err
	}
	if configs != 1 || actual != runID {
		return r, errors.New("legacy: capture run ID differs or config is not singleton")
	}
	if err := tx.QueryRow(ctx, `SELECT sealed,COALESCE(target_identity,'') FROM v3_migration_capture.config WHERE singleton`).Scan(&r.Sealed, &r.TargetIdentity); err != nil {
		return r, err
	}
	if err := validateOnlineCaptureStorage(ctx, tx); err != nil {
		return r, err
	}
	tables, err := discoverOnlineCaptureTables(ctx, tx)
	if err != nil {
		return r, err
	}
	// Catalog name expressions inherit C collation; registry text otherwise
	// follows the database locale and can reorder an unchanged source.
	rows, err := tx.Query(ctx, `SELECT name,relation_oid,keys,schema_fingerprint FROM v3_migration_capture.tables ORDER BY name COLLATE "C"`)
	if err != nil {
		return r, err
	}
	var stored []OnlineCaptureTable
	for rows.Next() {
		var table OnlineCaptureTable
		if err = rows.Scan(&table.Name, &table.RelationOID, &table.Keys, &table.SchemaFingerprint); err != nil {
			rows.Close()
			return r, err
		}
		stored = append(stored, table)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	if !onlineCaptureTablesEqual(tables, stored) {
		return r, errors.New("legacy: source table, OID, fields, primary key, partitions or row-security DDL drifted")
	}
	if err = validateOnlineCapturePermissions(ctx, tx); err != nil {
		return r, err
	}
	for _, table := range tables {
		descendants, err := onlineCaptureDescendants(ctx, tx, table.RelationOID)
		if err != nil {
			return r, err
		}
		for _, relation := range descendants {
			if err = validateOnlineCaptureTrigger(ctx, tx, relation, onlineCaptureRowTrigger, onlineCaptureFunctionName(table.Name), 29, onlineCaptureRowBody(table)); err != nil {
				return r, err
			}
			if err = validateOnlineCaptureTrigger(ctx, tx, relation, onlineCaptureTruncateTrigger, "reject_truncate", 34, onlineCaptureTruncateBody()); err != nil {
				return r, err
			}
			if len(table.Keys) == 0 {
				if err = validateOnlineCaptureTrigger(ctx, tx, relation, onlineCaptureFenceTrigger, "write_fence", 30, onlineCaptureFenceBody()); err != nil {
					return r, err
				}
			}
		}
	}
	r.Applied, r.Tables = true, tables
	return r, nil
}

// SealOnlineCapture is an explicit, database-enforced source write fence. SHARE
// locks wait for prior source writers, including low-ID long transactions, then
// atomically publish sealed=true. Do not call it during online preparation.
func SealOnlineCapture(ctx context.Context, pool *pgxpool.Pool, runID string) (OnlineCaptureReport, error) {
	return setOnlineCaptureSeal(ctx, pool, runID, true)
}

// UnsealOnlineCapture is only for explicitly abandoning cutover and reopening
// V2. A failed final import never calls this automatically.
func UnsealOnlineCapture(ctx context.Context, pool *pgxpool.Pool, runID string) (OnlineCaptureReport, error) {
	return setOnlineCaptureSeal(ctx, pool, runID, false)
}

// BindOnlineCaptureTarget prevents two targets sharing the global ack queue.
// Binding is permanent for this capture run; abandoning a target never silently
// allows another target to start beyond changes acknowledged by the first one.
func BindOnlineCaptureTarget(ctx context.Context, pool *pgxpool.Pool, runID, identity string) error {
	if pool == nil || !onlineCaptureRunID.MatchString(runID) || len(identity) < 16 || len(identity) > 128 {
		return errors.New("legacy: invalid capture target binding arguments")
	}
	for _, char := range identity {
		if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' && char != '-' {
			return errors.New("legacy: capture target identity must be a safe stable identifier")
		}
	}
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='1s'`); err != nil {
			return err
		}
		if _, err := ValidateOnlineCapture(ctx, tx, runID); err != nil {
			return err
		}
		var actual string
		var target *string
		if err := tx.QueryRow(ctx, `SELECT run_id,target_identity FROM v3_migration_capture.config WHERE singleton FOR UPDATE`).Scan(&actual, &target); err != nil {
			return err
		}
		if actual != runID || target != nil && *target != identity {
			return errors.New("legacy: capture is permanently bound to another target")
		}
		if target != nil {
			return nil
		}
		tag, err := tx.Exec(ctx, `UPDATE v3_migration_capture.config SET target_identity=$1 WHERE singleton AND run_id=$2 AND target_identity IS NULL`, identity, runID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return errors.New("legacy: capture target binding changed concurrently")
		}
		return nil
	})
}

func setOnlineCaptureSeal(ctx context.Context, pool *pgxpool.Pool, runID string, sealed bool) (OnlineCaptureReport, error) {
	r := OnlineCaptureReport{RunID: runID, Tables: []OnlineCaptureTable{}}
	if pool == nil || !onlineCaptureRunID.MatchString(runID) {
		return r, errors.New("legacy: invalid capture seal arguments")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return r, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='1s'`); err != nil {
		return r, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(738301032)`); err != nil {
		return r, err
	}
	r, err = ValidateOnlineCapture(ctx, tx, runID)
	if err != nil {
		return r, err
	}
	if len(r.Tables) > 0 {
		relations := make([]string, len(r.Tables))
		for i, table := range r.Tables {
			relations[i] = table.Relation
		}
		// LOCK TABLE without ONLY also locks every current partition.
		if _, err = tx.Exec(ctx, "LOCK TABLE "+strings.Join(relations, ",")+" IN SHARE MODE"); err != nil {
			return r, fmt.Errorf("legacy: capture seal cannot drain source writers within one second: %w", err)
		}
	}
	r, err = ValidateOnlineCapture(ctx, tx, runID)
	if err != nil {
		return r, err
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_migration_capture.config SET sealed=$1 WHERE singleton AND run_id=$2`, sealed, runID); err != nil {
		return r, err
	}
	if err = tx.Commit(ctx); err != nil {
		return r, err
	}
	r.Sealed = sealed
	return r, nil
}

// AckOnlineCapture acknowledges only these exact committed events. Retaining
// them is essential for a later full backup delta; old acknowledgments cannot
// hide a newer event for the same row, or a late-committing lower sequence ID.
func AckOnlineCapture(ctx context.Context, pool *pgxpool.Pool, runID string, eventIDs []int64) error {
	if pool == nil || !onlineCaptureRunID.MatchString(runID) {
		return errors.New("legacy: invalid capture acknowledgment arguments")
	}
	seen := make(map[int64]struct{}, len(eventIDs))
	for _, id := range eventIDs {
		if id <= 0 {
			return errors.New("legacy: capture event IDs must be positive")
		}
		seen[id] = struct{}{}
	}
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var actual string
		if err := tx.QueryRow(ctx, `SELECT run_id FROM v3_migration_capture.config WHERE singleton FOR SHARE`).Scan(&actual); err != nil {
			return err
		}
		if actual != runID {
			return errors.New("legacy: refusing acknowledgment for another capture run")
		}
		tag, err := tx.Exec(ctx, `UPDATE v3_migration_capture.events SET acked=true WHERE id=ANY($1::bigint[])`, eventIDs)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != int64(len(seen)) {
			return errors.New("legacy: acknowledgment contains absent or uncommitted capture IDs")
		}
		return nil
	})
}

func onlineCaptureDigest(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func onlineCaptureTablesEqual(left, right []OnlineCaptureTable) bool {
	if len(left) != len(right) {
		return false
	}
	for i, table := range left {
		other := right[i]
		if table.Name != other.Name || table.RelationOID != other.RelationOID || table.SchemaFingerprint != other.SchemaFingerprint || !slices.Equal(table.Keys, other.Keys) {
			return false
		}
	}
	return true
}

func discoverOnlineCaptureTables(ctx context.Context, tx pgx.Tx) ([]OnlineCaptureTable, error) {
	// Policy expressions and role names survive logical restore. Catalog OIDs
	// and ownership do not belong in this shared source/backup shape digest.
	rows, err := tx.Query(ctx, `SELECT n.nspname,c.relname,c.oid,
 COALESCE((SELECT array_agg(a.attname ORDER BY k.n) FROM pg_index i CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY k(attnum,n) JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=k.attnum WHERE i.indrelid=c.oid AND i.indisprimary AND i.indisvalid),'{}'::name[])::text[],
 GREATEST(c.reltuples::bigint,0),
 jsonb_build_object('kind',c.relkind,'partition_key',pg_get_partkeydef(c.oid),'relations',
 (WITH RECURSIVE children(oid) AS (SELECT c.oid UNION ALL SELECT i.inhrelid FROM pg_inherits i JOIN children p ON p.oid=i.inhparent)
 SELECT jsonb_agg(jsonb_build_object('schema',rn.nspname,'name',r.relname,'kind',r.relkind,'bound',pg_get_expr(r.relpartbound,r.oid),
 'row_security',r.relrowsecurity,'force_row_security',r.relforcerowsecurity,
 'policies',(SELECT jsonb_agg(jsonb_build_object('name',p.polname,'command',p.polcmd,'permissive',p.polpermissive,
 'roles',(SELECT jsonb_agg(CASE WHEN roles.role_oid=0 THEN jsonb_build_object('public',true) ELSE jsonb_build_object('role',pr.rolname) END ORDER BY CASE WHEN roles.role_oid=0 THEN '' ELSE pr.rolname END) FROM unnest(p.polroles) roles(role_oid) LEFT JOIN pg_roles pr ON pr.oid=roles.role_oid),
 'using',pg_get_expr(p.polqual,p.polrelid),'check',pg_get_expr(p.polwithcheck,p.polrelid)) ORDER BY p.polname) FROM pg_policy p WHERE p.polrelid=r.oid),
 'columns',(SELECT jsonb_agg(jsonb_build_object('n',a.attnum,'name',a.attname,'type',format_type(a.atttypid,a.atttypmod),'notnull',a.attnotnull,'identity',a.attidentity,'generated',a.attgenerated,'collation',(SELECT cn.nspname||'.'||cl.collname FROM pg_collation cl JOIN pg_namespace cn ON cn.oid=cl.collnamespace WHERE cl.oid=a.attcollation),'default',pg_get_expr(d.adbin,d.adrelid)) ORDER BY a.attnum) FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE a.attrelid=r.oid AND a.attnum>0 AND NOT a.attisdropped),
 'constraints',(SELECT jsonb_agg(jsonb_build_object('name',x.conname,'def',pg_get_constraintdef(x.oid),'validated',x.convalidated) ORDER BY x.conname) FROM pg_constraint x WHERE x.conrelid=r.oid),
 'triggers',(SELECT jsonb_agg(jsonb_build_object('definition',pg_get_triggerdef(t.oid),'enabled',t.tgenabled,'function',pg_get_functiondef(t.tgfoid)) ORDER BY t.tgname) FROM pg_trigger t WHERE t.tgrelid=r.oid AND NOT t.tgisinternal AND t.tgname NOT IN('codego_online_capture_row','codego_online_capture_no_truncate','codego_online_capture_write_fence'))) ORDER BY rn.nspname,r.relname)
 FROM children ch JOIN pg_class r ON r.oid=ch.oid JOIN pg_namespace rn ON rn.oid=r.relnamespace))
 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind IN('r','p') AND NOT c.relispartition
 AND n.nspname NOT LIKE 'pg_%' AND n.nspname NOT LIKE 'v3_%' AND n.nspname<>'information_schema' ORDER BY (n.nspname||'.'||c.relname) COLLATE "C"`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tables := []OnlineCaptureTable{}
	for rows.Next() {
		var schema, name string
		var table OnlineCaptureTable
		var signature []byte
		if err = rows.Scan(&schema, &name, &table.RelationOID, &table.Keys, &table.EstimatedRows, &signature); err != nil {
			return nil, err
		}
		// Keep the registry's schema.table form unambiguous; SQL itself always
		// uses quoted identifiers, including unusual primary-key field names.
		for _, component := range []string{schema, name} {
			for _, char := range component {
				if char < 32 || char > 126 || char == '.' {
					return nil, errors.New("legacy: capture relation names require unambiguous ASCII schema.table components")
				}
			}
		}
		table.Name, table.Relation = schema+"."+name, pgx.Identifier{schema, name}.Sanitize()
		table.SchemaFingerprint = onlineCaptureDigest(string(signature))
		tables = append(tables, table)
	}
	return tables, rows.Err()
}

func validateOnlineCaptureNoPK(ctx context.Context, tx pgx.Tx, tables []OnlineCaptureTable) error {
	for _, table := range tables {
		if len(table.Keys) != 0 {
			continue
		}
		var count int64
		var bytes int64
		if err := tx.QueryRow(ctx, `WITH RECURSIVE children(oid) AS (SELECT $1::oid UNION ALL SELECT i.inhrelid FROM pg_inherits i JOIN children p ON p.oid=i.inhparent)
 SELECT COALESCE(sum(pg_total_relation_size(oid)),0)::bigint FROM children`, table.RelationOID).Scan(&bytes); err != nil {
			return err
		}
		if bytes > 64<<20 {
			return fmt.Errorf("legacy: %s exceeds 64 MiB without a primary key; online capture requires a bounded full-table backup delta", table.Name)
		}
		// A bounded scan also catches an unanalyzed large no-PK table. Such
		// tables cannot be incrementally reconstructed from row identities.
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM (SELECT 1 FROM "+table.Relation+" LIMIT 100001) bounded").Scan(&count); err != nil {
			return err
		}
		if count > onlineCaptureNoPKLimit {
			return fmt.Errorf("legacy: %s has more than %d rows without a primary key; online capture requires a bounded full-table backup delta", table.Name, onlineCaptureNoPKLimit)
		}
	}
	return nil
}

func onlineCaptureDescendants(ctx context.Context, tx pgx.Tx, oid uint32) ([]string, error) {
	rows, err := tx.Query(ctx, `WITH RECURSIVE children(oid,depth) AS (SELECT $1::oid,0 UNION ALL SELECT i.inhrelid,p.depth+1 FROM pg_inherits i JOIN children p ON p.oid=i.inhparent)
 SELECT n.nspname,c.relname FROM children p JOIN pg_class c ON c.oid=p.oid JOIN pg_namespace n ON n.oid=c.relnamespace ORDER BY p.depth,c.oid`, oid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var relations []string
	for rows.Next() {
		var schema, name string
		if err = rows.Scan(&schema, &name); err != nil {
			return nil, err
		}
		relations = append(relations, pgx.Identifier{schema, name}.Sanitize())
	}
	return relations, rows.Err()
}

func onlineCaptureLiteral(value string) string {
	return "E'" + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), "'", "''") + "'"
}

func onlineCaptureFunctionName(name string) string {
	return "capture_" + onlineCaptureDigest(name)[:24]
}

func onlineCaptureRowBody(table OnlineCaptureTable) string {
	key := func(record string) string {
		if len(table.Keys) == 0 {
			return "'{}'::jsonb"
		}
		fields := make([]string, 0, 2*len(table.Keys))
		for _, column := range table.Keys {
			fields = append(fields, onlineCaptureLiteral(column), record+"."+pgx.Identifier{column}.Sanitize())
		}
		return "jsonb_build_object(" + strings.Join(fields, ",") + ")"
	}
	return "DECLARE old_key jsonb; new_key jsonb; BEGIN\n" + onlineCaptureCheckFence() + "\n" +
		"IF TG_OP<>'INSERT' THEN old_key := " + key("OLD") + "; END IF;\n" +
		"IF TG_OP<>'DELETE' THEN new_key := " + key("NEW") + "; END IF;\n" +
		"IF TG_OP='DELETE' OR (TG_OP='UPDATE' AND old_key IS DISTINCT FROM new_key) THEN INSERT INTO v3_migration_capture.events(table_name,row_key) VALUES(" + onlineCaptureLiteral(table.Name) + ",old_key); END IF;\n" +
		"IF TG_OP<>'DELETE' THEN INSERT INTO v3_migration_capture.events(table_name,row_key) VALUES(" + onlineCaptureLiteral(table.Name) + ",new_key); END IF;\nRETURN NULL; END"
}

func onlineCaptureTruncateBody() string {
	return "BEGIN RAISE EXCEPTION 'CodeGo online migration capture forbids TRUNCATE' USING ERRCODE='55000'; RETURN NULL; END"
}

func onlineCaptureCheckFence() string {
	// A row lock prevents an old REPEATABLE READ snapshot from observing an
	// unsealed config after the seal committed: PostgreSQL rejects the stale
	// locking read with a serialization failure rather than allowing a write.
	return "IF (SELECT sealed FROM v3_migration_capture.config WHERE singleton FOR SHARE) IS DISTINCT FROM false THEN RAISE EXCEPTION 'CodeGo online migration source is sealed' USING ERRCODE='55000'; END IF;"
}

func onlineCaptureFenceBody() string {
	return "BEGIN " + onlineCaptureCheckFence() + " RETURN NULL; END"
}

func onlineCaptureCreateFunction(name, body string) string {
	return "CREATE FUNCTION " + pgx.Identifier{onlineCaptureSchema, name}.Sanitize() + "() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,v3_migration_capture AS " + onlineCaptureLiteral(body)
}

func validateOnlineCaptureTrigger(ctx context.Context, tx pgx.Tx, relation, name, function string, kind int16, body string) error {
	var enabled string
	var actualKind int16
	var source string
	var safe bool
	err := tx.QueryRow(ctx, `SELECT t.tgenabled::text,t.tgtype,p.prosrc,
 p.prosecdef AND p.prorettype='trigger'::regtype AND p.pronargs=0 AND p.proconfig=ARRAY['search_path=pg_catalog, v3_migration_capture']::text[]
 AND p.proowner=n.nspowner AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql') AND t.tgnargs=0 AND t.tgqual IS NULL AND t.tgattr=''::int2vector
 FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE t.tgrelid=$1::regclass AND t.tgname=$2 AND n.nspname=$3 AND p.proname=$4`, relation, name, onlineCaptureSchema, function).Scan(&enabled, &actualKind, &source, &safe)
	if err != nil || !safe || enabled != "A" || actualKind != kind || onlineCaptureDigest(source) != onlineCaptureDigest(body) {
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return fmt.Errorf("legacy: capture trigger/function definition or enabled state drifted for %s", relation)
	}
	return nil
}

// Remove grants inherited from database ALTER DEFAULT PRIVILEGES as well as
// PostgreSQL's default PUBLIC function EXECUTE. The source writer only invokes
// the trigger; it receives no access to the private journal itself.
func revokeOnlineCaptureGrants(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `SELECT DISTINCT r.rolname FROM pg_roles r WHERE r.oid IN(
 SELECT x.grantee FROM pg_namespace n CROSS JOIN LATERAL aclexplode(COALESCE(n.nspacl,acldefault('n',n.nspowner))) x WHERE n.nspname=$1 AND x.grantee<>n.nspowner
 UNION SELECT x.grantee FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace CROSS JOIN LATERAL aclexplode(COALESCE(c.relacl,acldefault(CASE WHEN c.relkind='S' THEN 's'::"char" ELSE 'r'::"char" END,c.relowner))) x WHERE n.nspname=$1 AND x.grantee<>n.nspowner
 UNION SELECT x.grantee FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace CROSS JOIN LATERAL aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) x WHERE n.nspname=$1 AND x.grantee<>n.nspowner)`, onlineCaptureSchema)
	if err != nil {
		return err
	}
	roles := []string{"PUBLIC"}
	for rows.Next() {
		var role string
		if err = rows.Scan(&role); err != nil {
			rows.Close()
			return err
		}
		roles = append(roles, pgx.Identifier{role}.Sanitize())
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, role := range roles {
		for _, scope := range []string{"SCHEMA v3_migration_capture", "ALL TABLES IN SCHEMA v3_migration_capture", "ALL SEQUENCES IN SCHEMA v3_migration_capture", "ALL FUNCTIONS IN SCHEMA v3_migration_capture"} {
			if _, err = tx.Exec(ctx, "REVOKE ALL ON "+scope+" FROM "+role); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateOnlineCapturePermissions(ctx context.Context, tx pgx.Tx) error {
	var unsafe bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM pg_namespace n CROSS JOIN LATERAL aclexplode(COALESCE(n.nspacl,acldefault('n',n.nspowner))) x WHERE n.nspname=$1 AND x.grantee<>n.nspowner
 UNION ALL SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace CROSS JOIN LATERAL aclexplode(COALESCE(c.relacl,acldefault(CASE WHEN c.relkind='S' THEN 's'::"char" ELSE 'r'::"char" END,c.relowner))) x WHERE n.nspname=$1 AND (c.relowner<>n.nspowner OR x.grantee<>n.nspowner)
 UNION ALL SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace CROSS JOIN LATERAL aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) x WHERE n.nspname=$1 AND (p.proowner<>n.nspowner OR x.grantee<>n.nspowner))`, onlineCaptureSchema).Scan(&unsafe)
	if err != nil {
		return err
	}
	if unsafe {
		return errors.New("legacy: capture ownership or private grants drifted")
	}
	return nil
}

func validateOnlineCaptureStorage(ctx context.Context, tx pgx.Tx) error {
	contracts := []struct {
		name        string
		columns     []string
		constraints []string
	}{
		{"config", []string{"singleton:boolean:true::true", "run_id:text:true::", "sealed:boolean:true::false", "target_identity:text:false::"}, []string{"CHECK (singleton)", "PRIMARY KEY (singleton)"}},
		{"tables", []string{"name:text:true::", "relation_oid:oid:true::", "keys:text[]:true::", "schema_fingerprint:text:true::"}, []string{"PRIMARY KEY (name)", "UNIQUE (relation_oid)"}},
		{"events", []string{"id:bigint:true:a:", "table_name:text:true::", "row_key:jsonb:true::", "acked:boolean:true::false"}, []string{"FOREIGN KEY (table_name) REFERENCES v3_migration_capture.tables(name)", "PRIMARY KEY (id)"}},
	}
	for _, contract := range contracts {
		var columns, constraints []string
		var safe bool
		err := tx.QueryRow(ctx, `SELECT c.relkind='r' AND c.relowner=n.nspowner,
 (SELECT array_agg(a.attname||':'||format_type(a.atttypid,a.atttypmod)||':'||a.attnotnull::text||':'||a.attidentity::text||':'||COALESCE(pg_get_expr(d.adbin,d.adrelid),'') ORDER BY a.attnum) FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped),
 (SELECT array_agg(pg_get_constraintdef(x.oid) ORDER BY pg_get_constraintdef(x.oid)) FROM pg_constraint x WHERE x.conrelid=c.oid)
 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname=$2`, onlineCaptureSchema, contract.name).Scan(&safe, &columns, &constraints)
		if err != nil {
			return err
		}
		if !safe || !slices.Equal(columns, contract.columns) || !slices.Equal(constraints, contract.constraints) {
			return fmt.Errorf("legacy: private capture storage DDL drifted for %s", contract.name)
		}
	}
	return nil
}
