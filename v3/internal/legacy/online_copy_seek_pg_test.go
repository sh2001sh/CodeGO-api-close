//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type onlineCopySeekTx struct {
	pgx.Tx
	query string
	args  []any
}

func (tx *onlineCopySeekTx) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	if !strings.HasPrefix(query, "FETCH ") {
		tx.query, tx.args = query, args
	}
	return tx.Tx.Query(ctx, query, args...)
}

func (tx *onlineCopySeekTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if strings.HasPrefix(query, "DECLARE codego_online_copy_page ") {
		tx.query, tx.args = query, args
	}
	return tx.Tx.Exec(ctx, query, args...)
}

func TestOnlineCopySeeksTypedCursorWithoutScanningEarlierRows(t *testing.T) {
	for _, locale := range []string{"C", "en_US.UTF-8"} {
		t.Run(locale, func(t *testing.T) {
			pool := onlineCaptureCollationPool(t, locale)
			ctx := context.Background()
			onlineRecoveryExec(t, pool, `CREATE TABLE seek_id(id bigint PRIMARY KEY,payload text);
 CREATE TABLE seek_text(id text PRIMARY KEY,payload text);
 CREATE TABLE seek_pair(group_id bigint,model text,payload text,PRIMARY KEY(group_id,model));
 INSERT INTO seek_id SELECT g,'payload' FROM generate_series(1,100000)g;
 INSERT INTO seek_text SELECT 'request-'||lpad(g::text,6,'0'),'payload' FROM generate_series(1,100000)g;
 INSERT INTO seek_text VALUES('request-A','upper'),('request-a','lower'),('request_a','underscore');
 INSERT INTO seek_pair SELECT g,'model-'||lpad(m::text,3,'0'),'payload' FROM generate_series(1,1000)g CROSS JOIN generate_series(1,100)m;
 ANALYZE seek_id; ANALYZE seek_text; ANALYZE seek_pair`)
			read, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = read.Rollback(ctx) }()
			for _, test := range []struct {
				name, table, cursor, reference string
				keys                           []string
				seek                           bool
			}{
				{"empty", "seek_id", "", "SELECT to_jsonb(t) FROM seek_id t ORDER BY id LIMIT 4096", []string{"id"}, false},
				{"null", "seek_id", "null", "SELECT to_jsonb(t) FROM seek_id t ORDER BY id LIMIT 4096", []string{"id"}, false},
				{"high_id", "seek_id", `{"id":90000}`, "SELECT to_jsonb(t) FROM seek_id t WHERE id>90000 ORDER BY id LIMIT 4096", []string{"id"}, true},
				{"last_id", "seek_id", `{"id":100000}`, "SELECT to_jsonb(t) FROM seek_id t WHERE id>100000 ORDER BY id LIMIT 4096", []string{"id"}, true},
				{"high_text", "seek_text", `{"id":"request-090000"}`, "SELECT to_jsonb(t) FROM seek_text t WHERE id>'request-090000' ORDER BY id LIMIT 4096", []string{"id"}, true},
				{"text_collation", "seek_text", `{"id":"request-A"}`, "SELECT to_jsonb(t) FROM seek_text t WHERE id>'request-A' ORDER BY id LIMIT 4096", []string{"id"}, false},
				{"same_prefix_pair", "seek_pair", `{"group_id":900,"model":"model-050"}`, "SELECT to_jsonb(t) FROM seek_pair t WHERE ROW(group_id,model)>ROW(900::bigint,'model-050'::text) ORDER BY group_id,model LIMIT 4096", []string{"group_id", "model"}, true},
				{"end_prefix_pair", "seek_pair", `{"group_id":900,"model":"model-100"}`, "SELECT to_jsonb(t) FROM seek_pair t WHERE ROW(group_id,model)>ROW(900::bigint,'model-100'::text) ORDER BY group_id,model LIMIT 4096", []string{"group_id", "model"}, true},
			} {
				t.Run(test.name, func(t *testing.T) {
					traced := &onlineCopySeekTx{Tx: read}
					got, err := onlineReadBatch(ctx, traced, onlineSpec{source: pgx.Identifier{"public", test.table}.Sanitize(), keys: test.keys}, json.RawMessage(test.cursor))
					if err != nil {
						t.Fatal(err)
					}
					rows, err := read.Query(ctx, test.reference)
					if err != nil {
						t.Fatal(err)
					}
					var want []json.RawMessage
					for rows.Next() {
						var raw []byte
						if err := rows.Scan(&raw); err != nil {
							rows.Close()
							t.Fatal(err)
						}
						want = append(want, raw)
					}
					rows.Close()
					if err := rows.Err(); err != nil {
						t.Fatal(err)
					}
					if len(got) != len(want) {
						t.Fatalf("page rows=%d want=%d", len(got), len(want))
					}
					for i := range got {
						if !reflect.DeepEqual(got[i].raw, want[i]) {
							t.Fatalf("page differs at row %d", i)
						}
					}
					if !test.seek {
						return
					}
					var encoded []byte
					if err := read.QueryRow(ctx, "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+traced.query, traced.args...).Scan(&encoded); err != nil {
						t.Fatal(err)
					}
					var plans []map[string]any
					if err := json.Unmarshal(encoded, &plans); err != nil {
						t.Fatal(err)
					}
					indexed, visited := false, float64(0)
					var walk func(map[string]any)
					walk = func(plan map[string]any) {
						if plan["Relation Name"] == test.table {
							condition, _ := plan["Index Cond"].(string)
							indexed = indexed || strings.Contains(condition, ">")
							count, _ := plan["Actual Rows"].(float64)
							filtered, _ := plan["Rows Removed by Filter"].(float64)
							loops, _ := plan["Actual Loops"].(float64)
							visited += (count + filtered) * loops
						}
						children, _ := plan["Plans"].([]any)
						for _, child := range children {
							walk(child.(map[string]any))
						}
					}
					walk(plans[0]["Plan"].(map[string]any))
					if !indexed || visited > 4096 {
						t.Fatalf("cursor rescanned earlier rows: index_boundary=%t visited=%.0f plan=%s", indexed, visited, encoded)
					}
					t.Logf("index_boundary=%t visited=%.0f execution_ms=%v", indexed, visited, plans[0]["Execution Time"])
				})
			}
			bad, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = bad.Rollback(ctx) }()
			if _, err := onlineReadBatch(ctx, bad, onlineSpec{source: "public.seek_id", keys: []string{"id"}}, json.RawMessage(`{"id":"invalid-bigint"}`)); err == nil {
				t.Fatal("invalid typed cursor was accepted")
			}
		})
	}
}
