//go:build pgintegration

package legacy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/migrations"
)

type retainedBatchTracer struct {
	queries, batches, batchQueries atomic.Int64
	wrongStage                     atomic.Bool
}

func (c *retainedBatchTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.queries.Add(1)
	return ctx
}
func (*retainedBatchTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func (c *retainedBatchTracer) TraceBatchStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	c.batches.Add(1)
	c.batchQueries.Add(int64(data.Batch.Len()))
	for _, query := range data.Batch.QueuedQueries {
		if !strings.Contains(query.SQL, `"v3_migration_online".`) || !strings.Contains(query.SQL, " IS NOT DISTINCT FROM ") {
			c.wrongStage.Store(true)
		}
	}
	return ctx
}
func (*retainedBatchTracer) TraceBatchQuery(context.Context, *pgx.Conn, pgx.TraceBatchQueryData) {}
func (*retainedBatchTracer) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData)     {}

// The old sequential checker, restricted to these synthetic log projections.
// It keeps the same typed SQL, ordering, byte bounds, and deferred request ID.
func retainedSequentialLogChecks(ctx context.Context, p *onlineProjector, inputs []onlineInput) ([]bool, error) {
	matches := make([]bool, len(inputs))
	for i := range matches {
		matches[i] = true
	}
	outputs, err := p.projectBatch(ctx, onlineSpec{name: "logs"}, inputs, nil)
	if err != nil {
		return nil, err
	}
	type item struct {
		owner int
		row   onlineProjection
	}
	groups := map[string][]item{}
	var shapes []string
	for i, out := range outputs {
		if len(out) == 2 && out[0].Values["request_id"] != "" {
			delete(out[1].Values, "request_id")
		}
		for _, row := range out {
			shape := row.Schema + "." + row.Table + ":" + cmRecordShape(cmRecord{values: row.Values})
			if _, exists := groups[shape]; !exists {
				shapes = append(shapes, shape)
			}
			groups[shape] = append(groups[shape], item{i, row})
		}
	}
	for _, shape := range shapes {
		items := groups[shape]
		for len(items) > 0 {
			n, bytes := 0, 2
			var values []map[string]any
			var encodedRows [][]byte
			for n < len(items) && n < exactBulkRows {
				encoded, err := exactBulkEncodeRow(items[n].row.Values)
				if err != nil {
					return nil, err
				}
				if n > 0 && bytes+len(encoded)+1 > exactBulkBytes {
					break
				}
				values = append(values, items[n].row.Values)
				encodedRows = append(encodedRows, encoded)
				bytes += len(encoded) + 1
				n++
			}
			row := items[0].row
			name, columns, data, err := exactBulkEncodedInput(row.Schema, row.Table, row.Keys, values, encodedRows)
			if err != nil {
				return nil, err
			}
			checked, err := checkExactBulkData(ctx, onlineStageTx{Tx: p.target, tables: []string{row.Schema + "." + row.Table}}, name, row.Keys, columns, data)
			if err != nil {
				return nil, err
			}
			if len(checked) != n {
				return nil, errors.New("sequential synthetic comparison count differs")
			}
			for i, match := range checked {
				matches[items[i].owner] = matches[items[i].owner] && match
			}
			items = items[n:]
		}
	}
	// Include the same retired-row read as the production comparator so the
	// benchmark compares equal database work, not one extra round trip in batch.
	keys := make([]json.RawMessage, len(inputs))
	for i, input := range inputs {
		keys[i] = input.key
	}
	data, err := json.Marshal(keys)
	if err != nil {
		return nil, err
	}
	rows, err := p.target.Query(ctx, `SELECT r.row_key,r.metrics FROM v3_migration_online.retired_rows r
	 JOIN jsonb_array_elements($2::jsonb) k(value) ON r.row_key=k.value WHERE r.name=$1`, "logs", data)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if rows.Next() {
		return nil, errors.New("synthetic log benchmark unexpectedly has retired rows")
	}
	return matches, rows.Err()
}

func TestOnlineRetainedComparisonBatchExactBoundariesAndRoundTrips(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineRecoveryExec(t, source, `INSERT INTO migration_source.logs
	 SELECT 10000+g,7,1700100000+g,2,'batch exact','alice','key','chat-model',1,1,1,0,false,13,11,'default','','batch-exact-'||g,'','{"large_integer":9007199254740993}'
	 FROM generate_series(1,8192)g`)
	retainedCopiedFixture(t, m, opts)
	tracer := &retainedBatchTracer{}
	config := target.Config().Copy()
	config.ConnConfig.Tracer = tracer
	measured, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer measured.Close()
	sourceTx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sourceTx.Rollback(ctx) }()
	targetTx, err := measured.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = targetTx.Rollback(ctx) }()
	if err := onlineAuthorize(ctx, targetTx, opts.RunID); err != nil {
		t.Fatal(err)
	}
	sources, specs, err := onlineBindings(ctx, sourceTx, targetTx, opts.RunID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := loadOnlineProjector(ctx, sourceTx, targetTx, sources)
	if err != nil {
		t.Fatal(err)
	}
	var spec onlineSpec
	for _, candidate := range specs {
		if candidate.name == "logs" {
			spec = candidate
		}
	}
	// Across 512-row boundaries and both 4096-row source pages, one money unit
	// and a JSON integer beyond float64 precision must each affect its own owner.
	if _, err := targetTx.Exec(ctx, "UPDATE "+onlineStage("v3_audit.events")+" SET amount=amount+1 WHERE id IN (10512,14100)"); err != nil {
		t.Fatal(err)
	}
	if _, err := targetTx.Exec(ctx, "UPDATE "+onlineStage("v3_audit.events")+` SET metadata=jsonb_set(metadata,'{large_integer}','9007199254740994'::jsonb) WHERE id IN (10513,14101)`); err != nil {
		t.Fatal(err)
	}
	var cursor json.RawMessage
	var bad int
	for page := 0; page < 2; page++ {
		inputs, err := onlineReadBatch(ctx, sourceTx, spec, cursor)
		if err != nil || len(inputs) != onlineCopyBatchRows {
			t.Fatalf("page %d rows=%d err=%v", page, len(inputs), err)
		}
		tracer.queries.Store(0)
		started := time.Now()
		want, err := retainedSequentialLogChecks(ctx, p, inputs)
		sequentialTime, sequentialQueries := time.Since(started), tracer.queries.Load()
		if err != nil {
			t.Fatal(err)
		}
		tracer.queries.Store(0)
		tracer.batches.Store(0)
		tracer.batchQueries.Store(0)
		started = time.Now()
		got, err := p.matchRetainedInputs(ctx, spec, inputs)
		batchTime := time.Since(started)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("page %d exact results differ: %v", page, err)
		}
		if tracer.batches.Load() != 1 || tracer.batchQueries.Load() != sequentialQueries-1 || tracer.queries.Load() != 1 || tracer.wrongStage.Load() {
			t.Fatalf("page %d sequential queries=%d batches=%d batch queries=%d ordinary queries=%d wrongStage=%v", page, sequentialQueries, tracer.batches.Load(), tracer.batchQueries.Load(), tracer.queries.Load(), tracer.wrongStage.Load())
		}
		for i, match := range got {
			if !match {
				var key map[string]int64
				if err := json.Unmarshal(inputs[i].key, &key); err != nil {
					t.Fatal(err)
				}
				if key["id"] != 10512 && key["id"] != 10513 && key["id"] != 14100 && key["id"] != 14101 {
					t.Fatalf("wrong owner mismatched: %d", key["id"])
				}
				bad++
			}
		}
		t.Logf("page=%d rows=%d sequential_exact_queries=%d batched_exact_queries=%d batch_exchanges=1 retired_query=1 sequential_seconds=%.3f batch_seconds=%.3f; same typed results, not end-to-end throughput", page, len(inputs), sequentialQueries-1, tracer.batchQueries.Load(), sequentialTime.Seconds(), batchTime.Seconds())
		cursor = inputs[len(inputs)-1].key
	}
	if bad != 4 {
		t.Fatalf("one-unit and large-integer mismatches=%d want=4", bad)
	}
	// The comparator leaves its connection usable and never commits repairs.
	var one int
	if err := targetTx.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("comparison left pending batch results: %v", err)
	}
	if err := targetTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
}

type retainedLateBatchErrorTx struct{ pgx.Tx }

func (tx retainedLateBatchErrorTx) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	batch.Queue("SELECT 1/0") // An error after all expected checks must surface from Close.
	return tx.Tx.SendBatch(ctx, batch)
}

func TestOnlineRetainedComparisonBatchLateErrorRollsBackPage(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	retainedCopiedFixture(t, m, opts)
	before := retainedRowXmin(t, target, 51)
	var copiedBefore int64
	if err := target.QueryRow(ctx, "SELECT copied FROM v3_migration_online.progress WHERE name='logs'").Scan(&copiedBefore); err != nil {
		t.Fatal(err)
	}
	sourceTx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sourceTx.Rollback(ctx) }()
	targetTx, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = targetTx.Rollback(ctx) }()
	if err := onlineAuthorize(ctx, targetTx, opts.RunID); err != nil {
		t.Fatal(err)
	}
	sources, specs, err := onlineBindings(ctx, sourceTx, targetTx, opts.RunID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := loadOnlineProjector(ctx, sourceTx, targetTx, sources)
	if err != nil {
		t.Fatal(err)
	}
	var spec onlineSpec
	for _, candidate := range specs {
		if candidate.name == "logs" {
			spec = candidate
		}
	}
	inputs, err := onlineReadBatch(ctx, sourceTx, spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := targetTx.Exec(ctx, "UPDATE "+onlineStage("v3_audit.events")+" SET amount=amount+1 WHERE id=51"); err != nil {
		t.Fatal(err)
	}
	if _, err := targetTx.Exec(ctx, "UPDATE v3_migration_online.progress SET copied=copied+1 WHERE name='logs'"); err != nil {
		t.Fatal(err)
	}
	p.target = retainedLateBatchErrorTx{targetTx}
	if got, err := p.matchRetainedInputs(ctx, spec, inputs); err == nil || got != nil {
		t.Fatalf("late batch error ignored: results=%v err=%v", got, err)
	}
	if err := targetTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var copiedAfter int64
	if err := target.QueryRow(ctx, "SELECT copied FROM v3_migration_online.progress WHERE name='logs'").Scan(&copiedAfter); err != nil || copiedAfter != copiedBefore || retainedRowXmin(t, target, 51) != before {
		t.Fatalf("late error preserved partial page: copied=%d before=%d err=%v", copiedAfter, copiedBefore, err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	t.Log("late server error was surfaced after draining batch; prior page writes and cursor delta rolled back; connection reusable and native money empty")
}

// Measure the real typed PostgreSQL queries on one connection, including pages
// whose wide metadata keeps their input count below the parallel-projection
// threshold. Setup uses an isolated database and actual V3 migration schemas;
// no source production database or native account is involved.
func BenchmarkOnlineRetainedComparisonDatabase(b *testing.B) {
	dsn := os.Getenv("V3_MIGRATION_TEST_PG_DSN")
	if dsn == "" {
		b.Skip("V3_MIGRATION_TEST_PG_DSN not set; use a disposable dedicated database")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		b.Fatal(err)
	}
	name := "migration_retained_benchmark_" + hex.EncodeToString(random)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		b.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		b.Fatal(err)
	}
	config.ConnConfig.Database = name
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		b.Fatal(err)
	}
	defer pool.Close()
	names, err := migrations.Files()
	if err != nil {
		b.Fatal(err)
	}
	for _, name := range names {
		ddl, err := migrations.Read(name)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := pool.Exec(ctx, ddl); err != nil {
			b.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := pool.Exec(ctx, `CREATE SCHEMA v3_migration_online;
	 CREATE TABLE v3_migration_online.v3_audit__events (LIKE v3_audit.events INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES);
	 CREATE TABLE v3_migration_online.v3_billing__usage_logs (LIKE v3_billing.usage_logs INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES);
	 CREATE TABLE v3_migration_online.retired_rows(name text,row_key jsonb,metrics jsonb,PRIMARY KEY(name,row_key))`); err != nil {
		b.Fatal(err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Release()
	for _, metadataBytes := range []int{128, 8192} {
		b.Run(fmt.Sprintf("metadata=%d", metadataBytes), func(b *testing.B) {
			tx, err := conn.Begin(ctx)
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			p := &onlineProjector{target: tx, users: map[int64]bool{7: true}, mappings: map[fundingAccountKey]int64{{"user", "wallet", 7}: 901}, promptAnomalySchema: true}
			var inputs []onlineInput
			bytes := 0
			for i := 0; i < onlineCopyBatchRows; i++ {
				other, err := json.Marshal(map[string]any{"large_integer": int64(9007199254740993), "context": strings.Repeat("x", metadataBytes)})
				if err != nil {
					b.Fatal(err)
				}
				raw, err := json.Marshal(map[string]any{"id": i + 1, "user_id": 7, "type": 2, "created_at": 1700000000 + i, "quota": 17, "prompt_tokens": 23, "completion_tokens": 41, "content": "database benchmark", "request_id": fmt.Sprintf("database-%d", i), "other": string(other)})
				if err != nil {
					b.Fatal(err)
				}
				key := json.RawMessage(fmt.Sprintf(`{"id":%d}`, i+1))
				if bytes+len(raw)+len(key) > exactBulkBytes {
					break
				}
				inputs = append(inputs, onlineInput{key, raw})
				bytes += len(raw) + len(key)
			}
			outputs, err := p.projectBatch(ctx, onlineSpec{name: "logs"}, inputs, nil)
			if err != nil {
				b.Fatal(err)
			}
			for _, table := range []string{"events", "usage_logs"} {
				var rows []map[string]any
				var schema string
				var keys []string
				for _, out := range outputs {
					for _, row := range out {
						if row.Table == table {
							schema, keys = row.Schema, row.Keys
							rows = append(rows, row.Values)
						}
					}
				}
				for len(rows) > 0 {
					n, bytes := 0, 2
					var encoded [][]byte
					for n < len(rows) && n < exactBulkRows {
						data, err := exactBulkEncodeRow(rows[n])
						if err != nil {
							b.Fatal(err)
						}
						if n > 0 && bytes+len(data)+1 > exactBulkBytes {
							break
						}
						encoded = append(encoded, data)
						bytes += len(data) + 1
						n++
					}
					_, columns, data, err := exactBulkEncodedInput(schema, table, keys, rows[:n], encoded)
					if err != nil {
						b.Fatal(err)
					}
					if err := insertExactBulkData(ctx, tx, onlineStage(schema+"."+table), table, keys, columns, data, n); err != nil {
						b.Fatal(err)
					}
					rows = rows[n:]
				}
			}
			for _, dirty := range []bool{false, true} {
				b.Run(fmt.Sprintf("dirty=%v", dirty), func(b *testing.B) {
					if dirty {
						if _, err := tx.Exec(ctx, "UPDATE "+onlineStage("v3_audit.events")+" SET amount=amount+1 WHERE id=1"); err != nil {
							b.Fatal(err)
						}
						if _, err := tx.Exec(ctx, "UPDATE "+onlineStage("v3_audit.events")+` SET metadata=jsonb_set(metadata,'{large_integer}','9007199254740994'::jsonb) WHERE id=2`); err != nil {
							b.Fatal(err)
						}
					}
					want, err := retainedSequentialLogChecks(ctx, p, inputs)
					if err != nil {
						b.Fatal(err)
					}
					for i, match := range want {
						if match != (!dirty || i > 1) {
							b.Fatalf("baseline owner %d dirty=%v match=%v", i, dirty, match)
						}
					}
					for _, mode := range []string{"serial", "batch"} {
						b.Run(mode, func(b *testing.B) {
							b.ReportAllocs()
							b.SetBytes(int64(bytes))
							b.ResetTimer()
							started := time.Now()
							for i := 0; i < b.N; i++ {
								var got []bool
								var err error
								if mode == "serial" {
									got, err = retainedSequentialLogChecks(ctx, p, inputs)
								} else {
									got, err = p.matchRetainedInputs(ctx, onlineSpec{name: "logs"}, inputs)
								}
								if err != nil || !reflect.DeepEqual(got, want) {
									b.Fatalf("%s typed results differ: %v", mode, err)
								}
							}
							b.StopTimer()
							b.ReportMetric(float64(len(inputs)*b.N)/time.Since(started).Seconds(), "rows/s")
							b.ReportMetric(float64(len(inputs)), "rows/op")
						})
					}
				})
			}
			if err := tx.Rollback(ctx); err != nil {
				b.Fatal(err)
			}
		})
	}
}
