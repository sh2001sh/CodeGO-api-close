package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Database OIDs alone are only unique within a cluster. The cluster identifier
// also distinguishes equal database names reached through different DSNs.
// EXECUTE on pg_control_system is required; a weaker identity is not substituted.
func onlineDatabaseIdentity(ctx context.Context, tx pgx.Tx) (string, error) {
	var identity string
	if err := tx.QueryRow(ctx, `SELECT s.system_identifier::text||':'||d.oid::text||':'||d.datname FROM pg_control_system() s JOIN pg_database d ON d.datname=current_database()`).Scan(&identity); err != nil {
		return "", fmt.Errorf("legacy: cannot establish online database identity: %w", err)
	}
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:]), nil
}

// Bind every native and private relation, including its OID, instead of just
// the tables copied today. Schema upgrades during a run must start a fresh run;
// otherwise adopting an older LIKE table could silently discard the upgrade.
// Volatile statistics and sequence current values are deliberately excluded.
const onlineTargetShapeSQL = `WITH ns AS (
 SELECT oid,nspname,nspowner,nspacl FROM pg_namespace WHERE left(nspname,3)='v3_'
), rel AS (
 SELECT c.*,n.nspname FROM pg_class c JOIN ns n ON n.oid=c.relnamespace
 WHERE c.relkind IN('r','p','S','v','m','f')
)
SELECT jsonb_build_object(
 'schemas',(SELECT jsonb_agg(to_jsonb(n) ORDER BY n.nspname) FROM ns n),
 'relations',(SELECT jsonb_agg(jsonb_build_object(
  'oid',r.oid,'schema',r.nspname,'name',r.relname,'kind',r.relkind,
  'owner',r.relowner,'acl',r.relacl,'persistence',r.relpersistence,
  'options',r.reloptions,'tablespace',r.reltablespace,'access_method',r.relam,
  'row_security',r.relrowsecurity,'force_row_security',r.relforcerowsecurity,
  'replica_identity',r.relreplident,'partition_key',pg_get_partkeydef(r.oid),
  'partition_bound',pg_get_expr(r.relpartbound,r.oid),
  'parents',(SELECT jsonb_agg(i.inhparent ORDER BY i.inhseqno) FROM pg_inherits i WHERE i.inhrelid=r.oid),
  'columns',(SELECT jsonb_agg(jsonb_build_object(
   'number',a.attnum,'name',a.attname,'type',a.atttypid,'modifier',a.atttypmod,
   'not_null',a.attnotnull,'identity',a.attidentity,'generated',a.attgenerated,
   'dropped',a.attisdropped,'collation',a.attcollation,'acl',a.attacl,
   'storage',a.attstorage,'compression',a.attcompression,
   'default',pg_get_expr(d.adbin,d.adrelid)) ORDER BY a.attnum)
   FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
   WHERE a.attrelid=r.oid AND a.attnum>0),
  'constraints',(SELECT jsonb_agg(jsonb_build_object('oid',c.oid,'name',c.conname,
   'definition',pg_get_constraintdef(c.oid),'validated',c.convalidated,
   'reference_oid',c.confrelid) ORDER BY c.conname) FROM pg_constraint c WHERE c.conrelid=r.oid),
  'indexes',(SELECT jsonb_agg(jsonb_build_object('oid',i.indexrelid,
   'definition',pg_get_indexdef(i.indexrelid),'valid',i.indisvalid,'ready',i.indisready,
   'live',i.indislive,'replica_identity',i.indisreplident,'clustered',i.indisclustered,
   'options',ic.reloptions,'tablespace',ic.reltablespace) ORDER BY ic.relname)
   FROM pg_index i JOIN pg_class ic ON ic.oid=i.indexrelid WHERE i.indrelid=r.oid),
  'triggers',(SELECT jsonb_agg(jsonb_build_object('oid',t.oid,
   'definition',pg_get_triggerdef(t.oid),'enabled',t.tgenabled,
   'function',pg_get_functiondef(t.tgfoid)) ORDER BY t.tgname)
   FROM pg_trigger t WHERE t.tgrelid=r.oid AND NOT t.tgisinternal),
  'rules',(SELECT jsonb_agg(pg_get_ruledef(w.oid) ORDER BY w.rulename)
   FROM pg_rewrite w WHERE w.ev_class=r.oid),
  'policies',(SELECT jsonb_agg(to_jsonb(p) ORDER BY p.polname) FROM pg_policy p WHERE p.polrelid=r.oid),
  'sequence',(SELECT to_jsonb(s) FROM pg_sequence s WHERE s.seqrelid=r.oid)
 ) ORDER BY r.nspname,r.relname) FROM rel r),
 'functions',(SELECT jsonb_agg(jsonb_build_object('oid',p.oid,'owner',p.proowner,
  'acl',p.proacl,'definition',pg_get_functiondef(p.oid)) ORDER BY p.oid)
  FROM pg_proc p JOIN ns n ON n.oid=p.pronamespace WHERE p.prokind<>'a'),
 'incoming_constraints',(SELECT jsonb_agg(jsonb_build_object('oid',c.oid,
  'source',c.conrelid,'reference',c.confrelid,'definition',pg_get_constraintdef(c.oid),
  'validated',c.convalidated) ORDER BY c.oid) FROM pg_constraint c
  WHERE c.contype='f' AND c.confrelid IN(SELECT oid FROM rel)),
 'dependencies',(SELECT jsonb_agg(to_jsonb(d) ORDER BY d.classid,d.objid,d.objsubid,d.refclassid,d.refobjid,d.refobjsubid,d.deptype)
  FROM pg_depend d WHERE d.refclassid='pg_class'::regclass AND d.refobjid IN(SELECT oid FROM rel))
)`

func onlineTargetSchemaHash(ctx context.Context, target pgx.Tx) (string, error) {
	var searchPath string
	if err := target.QueryRow(ctx, "SELECT current_setting('search_path')").Scan(&searchPath); err != nil {
		return "", err
	}
	if _, err := target.Exec(ctx, "SELECT set_config('search_path','pg_catalog',true)"); err != nil {
		return "", err
	}
	var shape []byte
	if err := target.QueryRow(ctx, onlineTargetShapeSQL).Scan(&shape); err != nil {
		return "", err
	}
	if _, err := target.Exec(ctx, "SELECT set_config('search_path',$1,true)", searchPath); err != nil {
		return "", err
	}
	sum := sha256.Sum256(shape)
	return hex.EncodeToString(sum[:]), nil
}

func onlineValidateTargetSchema(ctx context.Context, target pgx.Tx) error {
	var stored string
	if err := target.QueryRow(ctx, "SELECT target_shape FROM v3_migration_online.run WHERE singleton").Scan(&stored); err != nil {
		return err
	}
	hash, err := onlineTargetSchemaHash(ctx, target)
	if err != nil {
		return err
	}
	if stored == "" || stored != hash {
		return errors.New("legacy: online target schema changed; do not adopt staging from a different schema revision")
	}
	return nil
}

// Locks the existing relations before the final shape check. ALTER/DROP cannot
// race between that check and adoption; normal source traffic is unaffected.
func onlineLockTargetSchema(ctx context.Context, target pgx.Tx) error {
	names, err := onlineDiscoverTargetRelations(ctx, target)
	if err != nil {
		return err
	}
	return onlineLockTargetRelations(ctx, target, names)
}

func onlineDiscoverTargetRelations(ctx context.Context, query interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}) ([]pgx.Identifier, error) {
	rows, err := query.Query(ctx, `SELECT n.nspname,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE left(n.nspname,3)='v3_' AND c.relkind IN('r','p') ORDER BY n.nspname,c.relname`)
	if err != nil {
		return nil, err
	}
	var names []pgx.Identifier
	for rows.Next() {
		var schema, name string
		if err = rows.Scan(&schema, &name); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, pgx.Identifier{schema, name})
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return names, nil
}

// Execute this as the first command of the final SERIALIZABLE transaction.
// LOCK takes no query snapshot: the following shape check sees DDL committed
// before these locks, rather than a stale catalog snapshot from earlier reads.
func onlineLockTargetRelations(ctx context.Context, target pgx.Tx, names []pgx.Identifier) error {
	for _, name := range names {
		if _, err := target.Exec(ctx, "LOCK TABLE "+name.Sanitize()+" IN SHARE ROW EXCLUSIVE MODE NOWAIT"); err != nil {
			return fmt.Errorf("legacy: online target is active or undergoing DDL: %w", err)
		}
	}
	return nil
}
