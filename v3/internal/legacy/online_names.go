package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type onlineObjectName struct {
	Kind  string `json:"kind"`
	OID   uint32 `json:"oid"`
	Name  string `json:"name"`
	Table string `json:"table"`
}

// LIKE copies definitions with generated names. Match definitions one-to-one,
// retaining the staged object OIDs so final adoption needs only catalog DDL.
const onlineObjectNamesSQL = `WITH objects AS (
 SELECT false AS staged,$1::text::regclass AS table_oid
 UNION ALL SELECT true,$2::text::regclass
)
SELECT o.staged,'constraint',c.oid,c.conname::text,
 jsonb_build_object('type',c.contype,'definition',pg_get_constraintdef(c.oid))::text AS signature
 FROM objects o JOIN pg_constraint c ON c.conrelid=o.table_oid WHERE c.contype<>'f'
UNION ALL
SELECT o.staged,'index',r.oid,r.relname::text,
 jsonb_build_object('kind',r.relkind,'access_method',r.relam,'unique',i.indisunique,
 'nulls_not_distinct',i.indnullsnotdistinct,'primary',i.indisprimary,'exclusion',i.indisexclusion,
 'key_count',i.indnkeyatts,'column_count',i.indnatts,
 'keys',(SELECT jsonb_agg(CASE WHEN k.attnum=0 THEN '' ELSE a.attname END ORDER BY k.n)
 FROM unnest(i.indkey) WITH ORDINALITY k(attnum,n) LEFT JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=k.attnum),
 'collations',i.indcollation::text,'classes',i.indclass::text,'options',i.indoption::text,
 'expressions',pg_get_expr(i.indexprs,i.indrelid),'predicate',pg_get_expr(i.indpred,i.indrelid),
 'storage',r.reloptions,'tablespace',r.reltablespace)::text AS signature
 FROM objects o JOIN pg_index i ON i.indrelid=o.table_oid JOIN pg_class r ON r.oid=i.indexrelid
 WHERE NOT(o.staged AND $1::text='v3_audit.events' AND r.relname='online_log_group_idx')
UNION ALL
SELECT o.staged,'sequence',s.oid,s.relname::text,a.attname::text AS signature
 FROM objects o JOIN pg_attribute a ON a.attrelid=o.table_oid AND a.attidentity<>''
 JOIN pg_depend d ON d.refclassid='pg_class'::regclass AND d.refobjid=o.table_oid AND d.refobjsubid=a.attnum
 AND d.classid='pg_class'::regclass AND d.deptype='i'
 JOIN pg_class s ON s.oid=d.objid AND s.relkind='S'
ORDER BY 2,5,4`

func onlineRememberObjectNames(ctx context.Context, target pgx.Tx, tables []string) error {
	var remembered []onlineObjectName
	for _, table := range tables {
		if table == "v3_billing.historical_accounts" {
			continue
		}
		if table == "v3_billing.usage_logs" {
			tables = append(tables, table+"_default")
		}
	}
	// The slice may acquire a child above; range over the complete result here.
	for _, table := range tables {
		if table == "v3_billing.historical_accounts" {
			continue
		}
		rows, err := target.Query(ctx, onlineObjectNamesSQL, table, onlineStage(table))
		if err != nil {
			return err
		}
		var originals []onlineObjectName
		var signatures []string
		staged := map[string][]onlineObjectName{}
		for rows.Next() {
			var cloned bool
			var object onlineObjectName
			var signature string
			if err = rows.Scan(&cloned, &object.Kind, &object.OID, &object.Name, &signature); err != nil {
				rows.Close()
				return err
			}
			object.Table = table
			key := object.Kind + "\x00" + signature
			if cloned {
				staged[key] = append(staged[key], object)
			} else {
				originals = append(originals, object)
				signatures = append(signatures, key)
			}
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		for i, original := range originals {
			key := signatures[i]
			matches := staged[key]
			if len(matches) == 0 {
				return fmt.Errorf("legacy: LIKE did not preserve the native %s definition on %s", original.Kind, table)
			}
			original.OID = matches[0].OID
			remembered = append(remembered, original)
			staged[key] = matches[1:]
		}
		for _, matches := range staged {
			if len(matches) != 0 {
				return fmt.Errorf("legacy: unexpected staged object definition on %s", table)
			}
		}
	}
	if remembered == nil {
		remembered = []onlineObjectName{}
	}
	raw, err := json.Marshal(remembered)
	if err != nil {
		return err
	}
	_, err = target.Exec(ctx, "UPDATE v3_migration_online.run SET object_names=$1 WHERE singleton", raw)
	return err
}

func onlineRestoreObjectNames(ctx context.Context, target pgx.Tx) error {
	var raw []byte
	if err := target.QueryRow(ctx, "SELECT object_names FROM v3_migration_online.run WHERE singleton").Scan(&raw); err != nil {
		return err
	}
	var remembered []onlineObjectName
	if err := json.Unmarshal(raw, &remembered); err != nil {
		return err
	}
	for _, object := range remembered {
		if object.Table == "v3_audit.events" {
			// This index serves duplicate repair during copying, then has no
			// runtime consumer. Remove it before restoring native index names.
			if _, err := target.Exec(ctx, "DROP INDEX IF EXISTS v3_audit.online_log_group_idx"); err != nil {
				return err
			}
			break
		}
	}
	for _, object := range remembered {
		var current, schema string
		var sql string
		switch object.Kind {
		case "constraint":
			var table string
			if err := target.QueryRow(ctx, "SELECT c.conname::text,n.nspname||'.'||r.relname FROM pg_constraint c JOIN pg_class r ON r.oid=c.conrelid JOIN pg_namespace n ON n.oid=r.relnamespace WHERE c.oid=$1", object.OID).Scan(&current, &table); err != nil {
				return err
			}
			if table != object.Table {
				return errors.New("legacy: adopted constraint moved to an unexpected table")
			}
			sql = "ALTER TABLE " + pgx.Identifier(strings.Split(object.Table, ".")).Sanitize() + " RENAME CONSTRAINT " + pgx.Identifier{current}.Sanitize() + " TO " + pgx.Identifier{object.Name}.Sanitize()
		case "index", "sequence":
			if err := target.QueryRow(ctx, "SELECT r.relname::text,n.nspname FROM pg_class r JOIN pg_namespace n ON n.oid=r.relnamespace WHERE r.oid=$1", object.OID).Scan(&current, &schema); err != nil {
				return err
			}
			if schema != strings.Split(object.Table, ".")[0] {
				return errors.New("legacy: adopted index/sequence moved to an unexpected schema")
			}
			sql = "ALTER " + strings.ToUpper(object.Kind) + " " + pgx.Identifier{schema, current}.Sanitize() + " RENAME TO " + pgx.Identifier{object.Name}.Sanitize()
		default:
			return errors.New("legacy: invalid adopted object kind")
		}
		if current != object.Name {
			if _, err := target.Exec(ctx, sql); err != nil {
				return err
			}
		}
	}
	return nil
}
