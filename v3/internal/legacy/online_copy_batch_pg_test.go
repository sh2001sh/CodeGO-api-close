//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type onlineCopyBatchTracer struct {
	reads, fetches, inserts, maxInsertRows int
	invalidInsert                          bool
	logs                                   bool
}

func (tracer *onlineCopyBatchTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, query pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(query.SQL, "DECLARE codego_online_copy_page ") {
		tracer.logs = strings.Contains(query.SQL, `"migration_source"."logs"`)
		if tracer.logs {
			tracer.reads++
		}
	}
	if strings.HasPrefix(query.SQL, "FETCH ") && tracer.logs {
		tracer.fetches++
	}
	if strings.HasPrefix(query.SQL, "INSERT INTO "+onlineStage("v3_audit.events")) {
		tracer.inserts++
		data, ok := query.Args[0].([]byte)
		var rows []json.RawMessage
		if !ok || json.Unmarshal(data, &rows) != nil || len(rows) > 512 || len(data) > 4<<20 && len(rows) != 1 {
			tracer.invalidInsert = true
		}
		tracer.maxInsertRows = max(tracer.maxInsertRows, len(rows))
	}
	return ctx
}

func (*onlineCopyBatchTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestOnlineCopy4096PagesKeepExact512RowProjectionBatches(t *testing.T) {
	m, source, target, opts := onlineMigrationFixture(t, false)
	ctx := context.Background()
	onlineRecoveryExec(t, source, `INSERT INTO migration_source.logs
 SELECT 10000+g,7,1700010000+g,2,'copy batch','alice','key','chat-model',1,1,1,0,false,13,11,'default','','copy-batch-'||g,'','{}'
 FROM generate_series(1,8192)g;
 UPDATE migration_source.logs SET created_at=1700020000,request_id='copy-inner-repeat' WHERE id IN(10001,10513);
 UPDATE migration_source.logs SET created_at=1700030000,request_id='copy-page-repeat' WHERE id IN(14092,14093)`)
	if _, err := m.PrepareOnline(ctx, opts, true); err != nil {
		t.Fatal(err)
	}
	tracer := &onlineCopyBatchTracer{}
	tracedPool := func(original *pgxpool.Pool) *pgxpool.Pool {
		config := original.Config().Copy()
		config.ConnConfig.Tracer = tracer
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		return pool
	}
	clone := *m
	clone.source, clone.pool = tracedPool(source), tracedPool(target)
	started := time.Now()
	if report, err := clone.CopyOnline(ctx, opts); err != nil || report.Phase != "copied" || report.Tables["logs"] != 8196 {
		t.Fatalf("copy report=%+v err=%v", report, err)
	}
	elapsed := time.Since(started)
	if tracer.reads != 4 || tracer.fetches != 18 || tracer.inserts != 17 || tracer.maxInsertRows != 512 || tracer.invalidInsert {
		t.Fatalf("copy pages=%d fetches=%d insert batches=%d largest=%d invalid=%t", tracer.reads, tracer.fetches, tracer.inserts, tracer.maxInsertRows, tracer.invalidInsert)
	}
	var count, amount int64
	if err := target.QueryRow(ctx, "SELECT count(*),sum(amount) FROM "+onlineStage("v3_billing.usage_logs")+" WHERE id>10000").Scan(&count, &amount); err != nil || count != 8192 || amount != 16384 {
		t.Fatalf("copy projection count=%d amount=%d err=%v", count, amount, err)
	}
	for _, id := range []int64{10001, 10513, 14092, 14093} {
		var actual, expected string
		if err := target.QueryRow(ctx, "SELECT request_id FROM "+onlineStage("v3_billing.usage_logs")+" WHERE id=$1", id).Scan(&actual); err != nil {
			t.Fatal(err)
		}
		if err := source.QueryRow(ctx, "SELECT request_id||':v2-log:'||id FROM migration_source.logs WHERE id=$1", id).Scan(&expected); err != nil || actual != expected {
			t.Fatalf("boundary duplicate id=%d request=%q expected=%q err=%v", id, actual, expected, err)
		}
	}
	var cursor, copied int64
	var complete bool
	if err := target.QueryRow(ctx, "SELECT (cursor->>'id')::bigint,copied,complete FROM v3_migration_online.progress WHERE name='logs'").Scan(&cursor, &copied, &complete); err != nil || cursor != 18192 || copied != 8196 || !complete {
		t.Fatalf("copy checkpoint cursor=%d copied=%d complete=%t err=%v", cursor, copied, complete, err)
	}
	if _, err := m.VerifyOnline(ctx, opts); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryAssertNoMoney(t, target)
	t.Logf("logs=8196 source_pages=%d fetches=%d target_insert_batches=%d max_insert_rows=%d elapsed=%s rows_per_second=%.0f", tracer.reads, tracer.fetches, tracer.inserts, tracer.maxInsertRows, elapsed, 8196/elapsed.Seconds())
}

func TestOnlineCopy4096ReadPagesPreserveByteLimitAndOversizeRejection(t *testing.T) {
	pool := onlineCaptureTestPool(t)
	ctx := context.Background()
	onlineRecoveryExec(t, pool, `CREATE TABLE copy_byte_rows(id bigint PRIMARY KEY,payload text);
 INSERT INTO copy_byte_rows VALUES(1,repeat('x',2300000)),(2,repeat('y',2300000)),(3,'tail')`)
	read, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(ctx) }()
	spec := onlineSpec{source: "public.copy_byte_rows", keys: []string{"id"}}
	var cursor json.RawMessage
	var ids []int64
	var sizes []int
	for {
		inputs, err := onlineReadBatch(ctx, read, spec, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if len(inputs) == 0 {
			break
		}
		sizes = append(sizes, len(inputs))
		bytes := 0
		for _, input := range inputs {
			bytes += len(input.key) + len(input.raw)
			var key struct{ ID int64 }
			if err := json.Unmarshal(input.key, &key); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, key.ID)
		}
		if bytes > 4<<20 && len(inputs) != 1 {
			t.Fatalf("copy source byte bound escaped bytes=%d rows=%d", bytes, len(inputs))
		}
		cursor = inputs[len(inputs)-1].key
	}
	if len(ids) != 3 || ids[0] != 1 || ids[1] != 2 || ids[2] != 3 || len(sizes) != 2 || sizes[0] != 1 || sizes[1] != 2 {
		t.Fatalf("byte boundary lost or duplicated rows ids=%v sizes=%v", ids, sizes)
	}
	if err := read.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	onlineRecoveryExec(t, pool, "INSERT INTO copy_byte_rows VALUES(4,repeat('z',65*1024*1024))")
	read, err = pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(ctx) }()
	if _, err := onlineReadBatch(ctx, read, spec, json.RawMessage(`{"id":3}`)); err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("oversized source row accepted: %v", err)
	}
	t.Logf("4MiB source page boundary preserves IDs=%v page_rows=%v; 65MiB row rejected", ids, sizes)
}

type onlineCopyWireConn struct {
	net.Conn
	bytes *atomic.Int64
}

func (conn *onlineCopyWireConn) Read(data []byte) (int, error) {
	n, err := conn.Conn.Read(data)
	conn.bytes.Add(int64(n))
	return n, err
}

type onlineCopyWideReadTx struct {
	pgx.Tx
	returned int64
}

func (tx *onlineCopyWideReadTx) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	rows, err := tx.Tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &onlineCopyWideRows{Rows: rows, returned: &tx.returned}, nil
}

type onlineCopyWideRows struct {
	pgx.Rows
	returned *int64
	closed   bool
}

func (rows *onlineCopyWideRows) Close() {
	if !rows.closed {
		rows.Rows.Close()
		*rows.returned += rows.Rows.CommandTag().RowsAffected()
		rows.closed = true
	}
}

// Read the original result-based page to compare its drain with the portal.
func onlineCopyLegacyWidePage(ctx context.Context, tx pgx.Tx, limit int, cursor json.RawMessage) ([]onlineInput, error) {
	query := "SELECT to_jsonb(t) FROM public.copy_wide_rows t"
	var args []any
	if len(cursor) > 0 {
		query += " WHERE ROW(t.id)>(SELECT e.id FROM jsonb_populate_record(NULL::public.copy_wide_rows,$1::jsonb)e)"
		args = append(args, cursor)
	}
	rows, err := tx.Query(ctx, query+fmt.Sprintf(" ORDER BY t.id LIMIT %d", limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var inputs []onlineInput
	bytes := 0
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		key, err := onlineKey(raw, []string{"id"})
		if err != nil {
			return nil, err
		}
		if len(inputs) > 0 && bytes+len(key)+len(raw) > 4<<20 {
			break
		}
		inputs = append(inputs, onlineInput{key, raw})
		bytes += len(key) + len(raw)
	}
	rows.Close()
	return inputs, rows.Err()
}

func TestOnlineCopy4096WideReadPageComparison(t *testing.T) {
	pool := onlineCaptureTestPool(t)
	ctx := context.Background()
	onlineRecoveryExec(t, pool, `CREATE TABLE copy_wide_rows(id bigint PRIMARY KEY,payload text);
 INSERT INTO copy_wide_rows SELECT g,repeat('w',20000) FROM generate_series(1,1000)g`)
	var received atomic.Int64
	config := pool.Config().Copy()
	config.MaxConns = 1
	dial := config.ConnConfig.DialFunc
	config.ConnConfig.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return &onlineCopyWireConn{Conn: conn, bytes: &received}, nil
	}
	measured, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(measured.Close)
	for _, limit := range []int{512, 4096, 0} {
		read, err := measured.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		tx := &onlineCopyWideReadTx{Tx: read}
		received.Store(0)
		started := time.Now()
		var cursor json.RawMessage
		var adopted int64
		pages := 0
		for {
			var inputs []onlineInput
			if limit == 0 {
				inputs, err = onlineReadBatch(ctx, tx, onlineSpec{source: "public.copy_wide_rows", keys: []string{"id"}}, cursor)
			} else {
				inputs, err = onlineCopyLegacyWidePage(ctx, tx, limit, cursor)
			}
			if err != nil {
				_ = read.Rollback(ctx)
				t.Fatal(err)
			}
			if len(inputs) == 0 {
				break
			}
			pages++
			for _, input := range inputs {
				var key struct{ ID int64 }
				if err := json.Unmarshal(input.key, &key); err != nil || key.ID != adopted+1 {
					_ = read.Rollback(ctx)
					t.Fatalf("wide row lost/duplicated id=%d expected=%d err=%v", key.ID, adopted+1, err)
				}
				adopted++
			}
			cursor = inputs[len(inputs)-1].key
		}
		elapsed := time.Since(started)
		bytes := received.Load()
		if err := read.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if adopted != 1000 {
			t.Fatalf("wide row count=%d", adopted)
		}
		if limit != 4096 && tx.returned != 2073 {
			t.Fatalf("bounded fetch amplified the original source drain returned=%d", tx.returned)
		}
		t.Logf("wide_read_limit=%d (0=portal) payload_bytes=20000 adopted=%d pages=%d server_rows=%d wire_bytes=%d elapsed=%s rows_per_second=%.0f", limit, adopted, pages, tx.returned, bytes, elapsed, float64(adopted)/elapsed.Seconds())
	}
}

type onlineCopyCancelAfterDeclareTx struct {
	pgx.Tx
	cancel context.CancelFunc
	closed bool
}

func (tx *onlineCopyCancelAfterDeclareTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	tag, err := tx.Tx.Exec(ctx, query, args...)
	if err == nil && strings.HasPrefix(query, "DECLARE codego_online_copy_page ") {
		tx.cancel()
	}
	if query == "CLOSE codego_online_copy_page" {
		tx.closed = true
	}
	return tag, err
}

func TestOnlineCopy4096CancellationClosesSourcePortal(t *testing.T) {
	pool := onlineCaptureTestPool(t)
	ctx := context.Background()
	onlineRecoveryExec(t, pool, "CREATE TABLE copy_cancel_rows(id bigint PRIMARY KEY); INSERT INTO copy_cancel_rows VALUES(1)")
	read, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(ctx) }()
	var pid int
	if err := read.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	defer cancel()
	tx := &onlineCopyCancelAfterDeclareTx{Tx: read, cancel: cancel}
	if _, err := onlineReadBatch(cancelled, tx, onlineSpec{source: "public.copy_cancel_rows", keys: []string{"id"}}, nil); err == nil || !tx.closed {
		t.Fatalf("cancelled fetch accepted or cursor cleanup omitted err=%v close_attempted=%t", err, tx.closed)
	}
	var portals int
	if err := read.QueryRow(ctx, "SELECT count(*) FROM pg_cursors WHERE name='codego_online_copy_page'").Scan(&portals); err == nil {
		if portals != 0 {
			t.Fatalf("cancelled fetch leaked portals=%d", portals)
		}
	} else if !read.Conn().PgConn().IsClosed() {
		t.Fatalf("cursor cleanup left an unusable open connection: %v", err)
	}
	_ = read.Rollback(ctx)
	observer, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig.Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = observer.Close(ctx) }()
	var snapshots int
	if err := observer.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE pid=$1 AND xact_start IS NOT NULL", pid).Scan(&snapshots); err != nil || snapshots != 0 {
		t.Fatalf("cancelled source retained snapshot count=%d err=%v", snapshots, err)
	}
	t.Log("cancellation after DECLARE closes the portal or its connection and releases the source snapshot")
}

type onlineCopyEndToEndTracer struct {
	source                         bool
	logs, pendingLogCommit         bool
	serverRows                     int64
	pages, maxPageRows, insertRows int
	inserts, commits               int
	invalidInsert                  bool
}

type onlineCopyReadQueryKey struct{}

func (tracer *onlineCopyEndToEndTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if tracer.source {
		if strings.HasPrefix(data.SQL, "DECLARE codego_online_copy_page ") {
			tracer.logs = strings.Contains(data.SQL, `"migration_source"."logs"`)
		}
		isLogs := strings.HasPrefix(data.SQL, "FETCH ") && tracer.logs || strings.HasPrefix(data.SQL, "SELECT to_jsonb(t) FROM ") && strings.Contains(data.SQL, `"migration_source"."logs"`)
		return context.WithValue(ctx, onlineCopyReadQueryKey{}, isLogs)
	}
	if strings.HasPrefix(data.SQL, "UPDATE v3_migration_online.progress SET cursor=") && data.Args[0] == "logs" {
		tracer.pendingLogCommit = true
		tracer.pages++
		tracer.maxPageRows = max(tracer.maxPageRows, data.Args[3].(int))
	}
	if strings.HasPrefix(data.SQL, "INSERT INTO "+onlineStage("v3_audit.events")) {
		var rows []json.RawMessage
		encoded, ok := data.Args[0].([]byte)
		if !ok || json.Unmarshal(encoded, &rows) != nil || len(rows) > 512 || len(encoded) > 4<<20 && len(rows) != 1 {
			tracer.invalidInsert = true
		}
		tracer.inserts++
		tracer.insertRows += len(rows)
	}
	return ctx
}

func (tracer *onlineCopyEndToEndTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if tracer.source {
		// The command tag counts all returned rows, including Rows.Close drains.
		if data.Err == nil && ctx.Value(onlineCopyReadQueryKey{}) == true {
			tracer.serverRows += data.CommandTag.RowsAffected()
		}
		return
	}
	if data.Err == nil && data.CommandTag.String() == "COMMIT" {
		tracer.commits++
		if tracer.pendingLogCommit {
			tracer.pendingLogCommit = false
		}
	}
}

func TestOnlineCopyEndToEndPageComparison(t *testing.T) {
	ctx := context.Background()
	for _, width := range []int{3 << 10, 8 << 10, 20 << 10} {
		var elapsed []time.Duration
		for trial := 1; trial <= 3; trial++ {
			t.Run(fmt.Sprintf("%d_bytes_trial_%d", width, trial), func(t *testing.T) {
				m, source, target, opts := onlineMigrationFixture(t, false)
				if _, err := source.Exec(ctx, `INSERT INTO migration_source.logs
 SELECT 10000+g,7,1700010000+g,2,repeat('w',$1),'alice','key','chat-model',1,1,1,0,false,13,11,'default','','copy-e2e-'||g,'','{}'
 FROM generate_series(1,8192)g`, width); err != nil {
					t.Fatal(err)
				}
				if _, err := m.PrepareOnline(ctx, opts, true); err != nil {
					t.Fatal(err)
				}
				var received atomic.Int64
				readTrace := &onlineCopyEndToEndTracer{source: true}
				writeTrace := &onlineCopyEndToEndTracer{}
				measuredPool := func(original *pgxpool.Pool, trace *onlineCopyEndToEndTracer) *pgxpool.Pool {
					config := original.Config().Copy()
					config.MaxConns = 1
					config.ConnConfig.Tracer = trace
					if trace.source {
						dial := config.ConnConfig.DialFunc
						config.ConnConfig.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
							conn, err := dial(ctx, network, address)
							if err != nil {
								return nil, err
							}
							return &onlineCopyWireConn{Conn: conn, bytes: &received}, nil
						}
					}
					pool, err := pgxpool.NewWithConfig(ctx, config)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(pool.Close)
					if err := pool.Ping(ctx); err != nil {
						t.Fatal(err)
					}
					return pool
				}
				clone := *m
				clone.source, clone.pool = measuredPool(source, readTrace), measuredPool(target, writeTrace)
				received.Store(0)
				started := time.Now()
				if report, err := clone.CopyOnline(ctx, opts); err != nil || report.Phase != "copied" || report.Tables["logs"] != 8196 {
					t.Fatalf("copy report=%+v err=%v", report, err)
				}
				duration := time.Since(started)
				elapsed = append(elapsed, duration)
				var count, amount, mismatches int64
				query := "SELECT count(*),sum(amount),(SELECT count(*) FROM " + onlineStage("v3_audit.events") + " WHERE id>10000 AND content<>repeat('w',$1)) FROM " + onlineStage("v3_billing.usage_logs") + " WHERE id>10000"
				if err := target.QueryRow(ctx, query, width).Scan(&count, &amount, &mismatches); err != nil || count != 8192 || amount != 16384 || mismatches != 0 {
					t.Fatalf("projection count=%d amount=%d mismatch=%d err=%v", count, amount, mismatches, err)
				}
				var cursor, copied int64
				var complete bool
				if err := target.QueryRow(ctx, "SELECT (cursor->>'id')::bigint,copied,complete FROM v3_migration_online.progress WHERE name='logs'").Scan(&cursor, &copied, &complete); err != nil || cursor != 18192 || copied != 8196 || !complete || writeTrace.pendingLogCommit || writeTrace.invalidInsert || writeTrace.insertRows != 8196 {
					t.Fatalf("cursor=%d copied=%d complete=%t tracer=%+v err=%v", cursor, copied, complete, writeTrace, err)
				}
				if _, err := m.VerifyOnline(ctx, opts); err != nil {
					t.Fatal(err)
				}
				onlineRecoveryAssertNoMoney(t, target)
				t.Logf("copy_variant=%s payload_bytes=%d trial=%d rows=8196 log_pages=%d max_page_rows=%d target_commits=%d event_insert_batches=%d server_rows=%d source_wire_bytes=%d elapsed=%s rows_per_second=%.0f", os.Getenv("COPY_VARIANT"), width, trial, writeTrace.pages, writeTrace.maxPageRows, writeTrace.commits, writeTrace.inserts, readTrace.serverRows, received.Load(), duration, 8196/duration.Seconds())
			})
		}
		if len(elapsed) != 3 {
			t.Fatal("incomplete copy comparison")
		}
		sort.Slice(elapsed, func(i, j int) bool { return elapsed[i] < elapsed[j] })
		t.Logf("copy_variant=%s payload_bytes=%d median_copy_elapsed=%s median_rows_per_second=%.0f trials=3", os.Getenv("COPY_VARIANT"), width, elapsed[1], 8196/elapsed[1].Seconds())
	}
}
