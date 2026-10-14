package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Larger records are processed alone; the ordinary batch remains 4 MiB/512
// rows. A single source or encoded projection has an explicit size bound.
const onlineRowMaxBytes = 64 << 20

// Source pages commit up to 4096 rows; projections keep their 512-row/4MiB bounds.
const onlineCopyBatchRows = 4096

func (m *Importer) onlineConnection(ctx context.Context) (*pgxpool.Conn, error) {
	if m.pool == nil || m.source == nil {
		return nil, errors.New("legacy: independent online source and target required")
	}
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	var acquired bool
	if err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", onlineLock).Scan(&acquired); err != nil || !acquired {
		conn.Release()
		if err == nil {
			err = errors.New("legacy: another online migration operation is running")
		}
		return nil, err
	}
	return conn, nil
}

// Explicitly unlock a live coordinator before sending Terminate: PostgreSQL
// releases locks asynchronously on connection close, which could make the next
// operation spuriously fail its try-lock. Always discard the physical connection,
// including after cleanup failure or when it owns a source shared fence instead.
func closeOnlineConnection(conn *pgxpool.Conn) {
	if conn != nil {
		raw := conn.Hijack()
		if !raw.IsClosed() {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			var unlocked bool
			err := raw.QueryRow(cleanup, "SELECT pg_advisory_unlock($1)", onlineLock).Scan(&unlocked)
			cancel()
			if err != nil {
				// Keep diagnostics fixed: connection errors may contain credentials.
				log.Print("legacy: online connection unlock failed; forcing connection closure")
			}
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := raw.Close(cleanup); err != nil {
			log.Print("legacy: online connection closure failed")
		}
	}
}

func onlineReadBatch(ctx context.Context, source pgx.Tx, spec onlineSpec, cursor json.RawMessage) (inputs []onlineInput, err error) {
	if spec.name == "ledger_entries" && ledgerHistoryArchived(ctx) {
		return nil, nil
	}
	quoted := make([]string, len(spec.keys))
	left, right := make([]string, len(spec.keys)), make([]string, len(spec.keys))
	for i, key := range spec.keys {
		quoted[i] = pgx.Identifier{key}.Sanitize()
		left[i] = "t." + quoted[i]
		right[i] = "e." + quoted[i]
	}
	payload := "to_jsonb(t)"
	if financialReconciliation(ctx, spec.name) {
		payload = financialReconcilePayload(spec.name)
	}
	query := "SELECT " + payload + " FROM " + spec.source + " t"
	where := " WHERE "
	if predicate := historyWindow(ctx, spec.name, "t", spec.requests, spec.attempts); predicate != "TRUE" {
		query += where + predicate
		where = " AND "
	}
	var args []any
	if len(cursor) > 0 && string(cursor) != "null" {
		// An uncorrelated typed subquery makes the boundary an InitPlan. A cross
		// join can instead scan the primary key from its beginning on every page.
		query += where + "ROW(" + strings.Join(left, ",") + ")>(SELECT " + strings.Join(right, ",") + " FROM jsonb_populate_record(NULL::" + spec.source + ",$1::jsonb)e)"
		args = append(args, cursor)
	}
	query += " ORDER BY " + strings.Join(left, ",") + " LIMIT " + strconv.Itoa(onlineCopyBatchRows)
	// Fetching the larger LIMIT in one result makes Rows.Close drain every unused
	// wide row after the byte boundary. A forward-only portal bounds that drain
	// to the original 512 rows while target commits cover the larger source page.
	if _, err = source.Exec(ctx, "DECLARE codego_online_copy_page NO SCROLL CURSOR FOR "+query, args...); err != nil {
		return nil, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, closeErr := source.Exec(cleanup, "CLOSE codego_online_copy_page")
		err = errors.Join(err, closeErr)
	}()
	bytes := 0
	for len(inputs) < onlineCopyBatchRows && bytes < exactBulkBytes {
		rows, queryErr := source.Query(ctx, "FETCH FORWARD 512 FROM codego_online_copy_page")
		if queryErr != nil {
			return nil, queryErr
		}
		full := false
		for rows.Next() {
			var raw []byte
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return nil, err
			}
			key, keyErr := onlineKey(raw, spec.keys)
			if keyErr != nil {
				rows.Close()
				return nil, keyErr
			}
			size := len(key) + len(raw)
			if size > onlineRowMaxBytes {
				rows.Close()
				return nil, errors.New("legacy: online source row exceeds 64 MiB limit")
			}
			if len(inputs) > 0 && bytes+size > exactBulkBytes {
				// The next keyset page rereads this row from the same snapshot.
				full = true
				break
			}
			inputs = append(inputs, onlineInput{key, raw})
			bytes += size
			if len(inputs) == onlineCopyBatchRows || bytes >= exactBulkBytes {
				full = true
				break
			}
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return nil, err
		}
		if full || rows.CommandTag().RowsAffected() < 512 {
			break
		}
	}
	return inputs, nil
}

func onlineCurrentBatches(ctx context.Context, source pgx.Tx, spec onlineSpec, keys []json.RawMessage, visit func([]onlineInput) error) error {
	data, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	conditions := make([]string, len(spec.keys))
	for i, key := range spec.keys {
		q := pgx.Identifier{key}.Sanitize()
		conditions[i] = "t." + q + "=e." + q
	}
	rows, err := source.Query(ctx, "SELECT j.value,to_jsonb(t) FROM jsonb_array_elements($1::jsonb)j(value) CROSS JOIN LATERAL jsonb_populate_record(NULL::"+spec.source+",j.value)e LEFT JOIN "+spec.source+" t ON "+strings.Join(conditions, " AND ")+" AND "+historyWindow(ctx, spec.name, "t", spec.requests, spec.attempts)+" ORDER BY j.value", data)
	if err != nil {
		return err
	}
	return onlineInputBatches(rows, visit)
}

// The source cursor stays open while the independent target transaction applies
// each batch. No acknowledged key or derived relationship is discarded to fit
// the limit; an error rolls back the caller's entire target transaction.
func onlineInputBatches(rows pgx.Rows, visit func([]onlineInput) error) error {
	defer rows.Close()
	var inputs []onlineInput
	bytes := 0
	flush := func() error {
		if len(inputs) == 0 {
			return nil
		}
		if err := visit(inputs); err != nil {
			return err
		}
		clear(inputs)
		inputs = inputs[:0]
		bytes = 0
		return nil
	}
	for rows.Next() {
		var key, raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return err
		}
		if string(raw) == "null" {
			raw = nil
		}
		size := len(key) + len(raw)
		if size > onlineRowMaxBytes {
			return errors.New("legacy: online source row exceeds 64 MiB limit")
		}
		if len(inputs) > 0 && bytes+size > exactBulkBytes {
			if err := flush(); err != nil {
				return err
			}
		}
		inputs = append(inputs, onlineInput{key, raw})
		bytes += size
		if len(inputs) == exactBulkRows || bytes >= exactBulkBytes {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return flush()
}

func (m *Importer) CopyOnline(ctx context.Context, opts OnlineOptions) (OnlineReport, error) {
	ctx = m.historyContext(ctx)
	r := OnlineReport{RunID: opts.RunID, Tables: map[string]int64{}, LedgerHistoryMode: ledgerHistoryMode(ctx), HistoryCutoff: historyCutoffLabel(ctx)}
	conn, err := m.onlineConnection(ctx)
	if err != nil {
		return r, err
	}
	defer closeOnlineConnection(conn)
	source, err := m.source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return r, err
	}
	defer func() { _ = source.Rollback(ctx) }()
	init, err := conn.Begin(ctx)
	if err != nil {
		return r, err
	}
	defer func() { _ = init.Rollback(ctx) }()
	if err = onlineAuthorize(ctx, init, opts.RunID); err != nil {
		return r, err
	}
	sources, specs, err := onlineBindings(ctx, source, init, opts.RunID)
	if err != nil {
		return r, err
	}
	var phase string
	if err = init.QueryRow(ctx, "SELECT phase FROM v3_migration_online.run WHERE singleton").Scan(&phase); err != nil {
		return r, err
	}
	if phase != "copying" {
		r.Phase = phase
		return r, nil
	}
	p, err := loadOnlineProjector(ctx, source, init, sources)
	if err != nil {
		return r, err
	}
	p.copying = true
	if err = onlineRefreshAccounts(ctx, init, p); err != nil {
		return r, err
	}
	if err = init.Commit(ctx); err != nil {
		return r, err
	}
	for _, spec := range specs {
		for {
			target, err := conn.Begin(ctx)
			if err != nil {
				return r, err
			}
			if err = onlineAuthorize(ctx, target, opts.RunID); err != nil {
				_ = target.Rollback(ctx)
				return r, err
			}
			var cursor []byte
			var complete bool
			if err = target.QueryRow(ctx, "SELECT cursor,complete FROM v3_migration_online.progress WHERE name=$1 FOR UPDATE", spec.name).Scan(&cursor, &complete); err != nil {
				_ = target.Rollback(ctx)
				return r, err
			}
			if complete {
				_ = target.Rollback(ctx)
				break
			}
			inputs, err := onlineReadBatch(ctx, source, spec, cursor)
			if err != nil {
				_ = target.Rollback(ctx)
				return r, err
			}
			p.target = target
			if len(inputs) > 0 {
				if err = p.apply(ctx, spec, inputs); err != nil {
					_ = target.Rollback(ctx)
					return r, err
				}
				cursor = inputs[len(inputs)-1].key
				r.Copied += int64(len(inputs))
				r.Tables[spec.name] += int64(len(inputs))
			}
			if _, err = target.Exec(ctx, "UPDATE v3_migration_online.progress SET cursor=$2,complete=$3,copied=copied+$4 WHERE name=$1", spec.name, cursor, len(inputs) == 0, len(inputs)); err != nil {
				_ = target.Rollback(ctx)
				return r, err
			}
			if err = target.Commit(ctx); err != nil {
				return r, err
			}
			if len(inputs) == 0 {
				break
			}
		}
	}
	finish, err := conn.Begin(ctx)
	if err != nil {
		return r, err
	}
	defer func() { _ = finish.Rollback(ctx) }()
	if err = onlineAuthorize(ctx, finish, opts.RunID); err != nil {
		return r, err
	}
	if sources["logs"] != "" {
		if err = onlineNormalizeLogs(ctx, finish, nil, true); err != nil {
			return r, err
		}
	}
	if _, err = finish.Exec(ctx, "UPDATE v3_migration_online.run SET phase='copied' WHERE singleton"); err != nil {
		return r, err
	}
	if err = finish.Commit(ctx); err != nil {
		return r, err
	}
	r.Phase = "copied"
	r.Applied = true
	return r, nil
}

func onlineRefreshAccounts(ctx context.Context, target pgx.Tx, p *onlineProjector) error {
	if _, err := target.Exec(ctx, "DELETE FROM "+onlineStage("v3_billing.historical_accounts")); err != nil {
		return err
	}
	rows, err := p.historicalAccounts()
	if err != nil {
		return err
	}
	for len(rows) > 0 {
		n := min(exactBulkRows, len(rows))
		values := make([]map[string]any, n)
		for i := 0; i < n; i++ {
			values[i] = rows[i].Values
		}
		if err = onlineInsertRows(ctx, target, "v3_billing.historical_accounts", []string{"source_account_id"}, values, onlineMetrics{}); err != nil {
			return err
		}
		rows = rows[n:]
	}
	return nil
}

func (m *Importer) SyncOnline(ctx context.Context, opts OnlineOptions) (OnlineReport, error) {
	ctx = m.historyContext(ctx)
	r := OnlineReport{RunID: opts.RunID, Tables: map[string]int64{}, LedgerHistoryMode: ledgerHistoryMode(ctx), HistoryCutoff: historyCutoffLabel(ctx)}
	eventLimit := opts.SyncEventLimit
	if eventLimit == 0 {
		eventLimit = 4096
	}
	if eventLimit < 1 || eventLimit > 16384 {
		return r, errors.New("legacy: online sync event limit must be between 1 and 16384")
	}
	if opts.SourceAdmin == nil {
		return r, errors.New("legacy: online sync requires capture acknowledgement connection")
	}
	if err := ValidateOnlineCaptureSource(ctx, m.source, opts.SourceAdmin); err != nil {
		return r, err
	}
	conn, err := m.onlineConnection(ctx)
	if err != nil {
		return r, err
	}
	defer closeOnlineConnection(conn)
	source, err := m.source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return r, err
	}
	defer func() { _ = source.Rollback(ctx) }()
	target, err := conn.Begin(ctx)
	if err != nil {
		return r, err
	}
	defer func() { _ = target.Rollback(ctx) }()
	if err = onlineAuthorize(ctx, target, opts.RunID); err != nil {
		return r, err
	}
	sources, specs, err := onlineBindings(ctx, source, target, opts.RunID)
	if err != nil {
		return r, err
	}
	if err = target.QueryRow(ctx, "SELECT phase FROM v3_migration_online.run WHERE singleton").Scan(&r.Phase); err != nil {
		return r, err
	}
	if r.Phase != "copied" && r.Phase != "verified" && r.Phase != "sampled" {
		return r, errors.New("legacy: complete the online baseline before synchronizing")
	}
	p, err := loadOnlineProjector(ctx, source, target, sources)
	if err != nil {
		return r, err
	}
	capture, err := ValidateOnlineCapture(ctx, source, opts.RunID)
	if err != nil {
		return r, err
	}
	ids, err := onlineProjectPending(ctx, source, target, sources, specs, p, capture, eventLimit, false, &r)
	if err != nil {
		return r, err
	}

	// Empty snapshots are not a claim of no future writes. Finalize additionally
	// requires the source's database fence and a verified complete baseline.
	if err = target.Commit(ctx); err != nil {
		return r, err
	}
	if err = source.Commit(ctx); err != nil {
		return r, err
	}
	if len(ids) > 0 {
		if err = ValidateOnlineCaptureSource(ctx, m.source, opts.SourceAdmin); err != nil {
			return r, err
		}
		if err = AckOnlineCapture(ctx, opts.SourceAdmin, opts.RunID, ids); err != nil {
			return r, err
		}
	}
	r.Acknowledged = int64(len(ids))
	r.Applied = true
	// Status must not rescan millions of events after every bounded batch.
	// Zero is still exact; positive counts at the limit explicitly mean at
	// least that many committed events. Verify/finalize retain their separate
	// exact quiescence and final-window budget checks.
	if err = m.source.QueryRow(ctx, "SELECT count(*) FROM (SELECT 1 FROM v3_migration_capture.events WHERE NOT acked LIMIT 4096) pending").Scan(&r.Pending); err != nil {
		return r, err
	}
	r.PendingAtLeast = r.Pending == 4096
	return r, nil
}

// Share the exact bounded Sync projection with verification. In verification
// every pending event visible in the fixed source snapshot must fit the batch;
// a concurrent later commit stays pending for a subsequent Sync.
func onlineProjectPending(ctx context.Context, source, target pgx.Tx, sources map[string]string, specs []onlineSpec, p *onlineProjector, capture OnlineCaptureReport, eventLimit int, requireAll bool, r *OnlineReport) ([]int64, error) {
	var err error
	byName := map[string]onlineSpec{}
	accountEvents := map[string]bool{}
	for _, table := range capture.Tables {
		if table.Relation == sources["accounts"] {
			accountEvents[table.Name] = true
		}
	}
	for _, spec := range specs {
		for _, table := range capture.Tables {
			if table.Relation == spec.source {
				byName[table.Name] = spec
			}
		}
	}
	rows, err := source.Query(ctx, "SELECT id,table_name,row_key FROM v3_migration_capture.events WHERE NOT acked ORDER BY id LIMIT $1", eventLimit)
	if err != nil {
		return nil, err
	}
	var ids []int64
	refreshAccounts := false
	eventBytes := 0
	work := map[string]map[string]json.RawMessage{}
	for rows.Next() {
		var id int64
		var name string
		var key []byte
		if err = rows.Scan(&id, &name, &key); err != nil {
			rows.Close()
			return nil, err
		}
		if len(key) > onlineRowMaxBytes {
			rows.Close()
			return nil, errors.New("legacy: online source key exceeds 64 MiB limit")
		}
		if len(ids) > 0 && eventBytes+len(key) > exactBulkBytes {
			break
		}
		eventBytes += len(key)
		ids = append(ids, id)
		refreshAccounts = refreshAccounts || accountEvents[name]
		if spec, ok := byName[name]; ok {
			if work[spec.name] == nil {
				work[spec.name] = map[string]json.RawMessage{}
			}
			work[spec.name][string(key)] = key
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if requireAll {
		var remaining bool
		if err = source.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM v3_migration_capture.events WHERE NOT acked AND id<>ALL($1::bigint[]))", ids).Scan(&remaining); err != nil {
			return nil, err
		}
		if remaining {
			return nil, errors.New("legacy: online verification catch-up exceeds bounded event budget; synchronize first")
		}
	}
	// Historical account fields come from billing.accounts, not users. Wallet
	// identities are already reserved while loading snapshot dependencies above.
	// Keep unprocessed account events pending; their own batch refreshes metadata.
	if refreshAccounts {
		if err = onlineRefreshAccounts(ctx, target, p); err != nil {
			return nil, err
		}
	}
	// All dirty keys are bounded by the original event batch. Only derived
	// attempt keys can be numerous; stream them twice from this same snapshot.
	keysBySpec := map[string][]json.RawMessage{}
	for name, keys := range work {
		for _, key := range keys {
			keysBySpec[name] = append(keysBySpec[name], key)
		}
	}
	if err = onlineRetentionParents(ctx, source, target, sources, keysBySpec); err != nil {
		return nil, err
	}
	for _, spec := range specs {
		keys := keysBySpec[spec.name]
		if len(keys) > 0 {
			if err = p.delete(ctx, spec, keys); err != nil {
				return nil, err
			}
		}
		if spec.name == "request_attempt_audits" && len(keysBySpec["request_audits"]) > 0 {
			rows, err := onlineRelatedAttemptRows(ctx, source, spec, sources["request_audits"], keysBySpec["request_audits"], keys, false)
			if err != nil {
				return nil, err
			}
			if err = onlineInputBatches(rows, func(inputs []onlineInput) error {
				batchKeys := make([]json.RawMessage, len(inputs))
				for i, input := range inputs {
					batchKeys[i] = input.key
				}
				return p.delete(ctx, spec, batchKeys)
			}); err != nil {
				return nil, err
			}
		}
	}
	// Only now insert projections: every old unique value has been released,
	// including values held by rows in later byte-bounded batches.
	for _, spec := range specs {
		keys := keysBySpec[spec.name]
		insertBatch := func(inputs []onlineInput) error {
			if err := p.insert(ctx, spec, inputs); err != nil {
				return err
			}
			r.Tables[spec.name] += int64(len(inputs))
			return nil
		}
		if len(keys) > 0 {
			if err = onlineCurrentBatches(ctx, source, spec, keys, insertBatch); err != nil {
				return nil, err
			}
		}
		if spec.name == "request_attempt_audits" && len(keysBySpec["request_audits"]) > 0 {
			rows, err := onlineRelatedAttemptRows(ctx, source, spec, sources["request_audits"], keysBySpec["request_audits"], keys, true)
			if err != nil {
				return nil, err
			}
			if err = onlineInputBatches(rows, insertBatch); err != nil {
				return nil, err
			}
		}
	}

	return ids, nil
}

// Parent-only changes affect all current children. Reading keys during deletion
// and rows during insertion avoids retaining an unbounded relationship map.
func onlineRelatedAttemptRows(ctx context.Context, source pgx.Tx, spec onlineSpec, parentTable string, parentKeys, directKeys []json.RawMessage, includeRows bool) (pgx.Rows, error) {
	parents, err := json.Marshal(parentKeys)
	if err != nil {
		return nil, err
	}
	if directKeys == nil {
		directKeys = []json.RawMessage{}
	}
	direct, err := json.Marshal(directKeys)
	if err != nil {
		return nil, err
	}
	payload := "NULL::jsonb"
	if includeRows {
		payload = "CASE WHEN " + historyWindow(ctx, spec.name, "a", spec.requests, spec.attempts) + " THEN to_jsonb(a) ELSE NULL::jsonb END"
	}
	return source.Query(ctx, "SELECT jsonb_build_object('attempt_id',a.attempt_id),"+payload+" FROM "+spec.source+" a JOIN jsonb_populate_recordset(NULL::"+parentTable+",$1::jsonb)e ON a.request_id=e.request_id WHERE NOT EXISTS(SELECT 1 FROM jsonb_populate_recordset(NULL::"+spec.source+",$2::jsonb)d WHERE d.attempt_id=a.attempt_id) ORDER BY a.attempt_id", parents, direct)
}

func onlineRequireQuiescent(ctx context.Context, source pgx.Tx) error {
	var pending bool
	if err := source.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM v3_migration_capture.events WHERE NOT acked)").Scan(&pending); err != nil {
		return err
	}
	if pending {
		return errors.New("legacy: online changes remain unacknowledged; synchronize before verification or finalization")
	}
	return nil
}
