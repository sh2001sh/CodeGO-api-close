package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// A new capture epoch is required. This never treats a paused journal as
// continuous, and never changes the native accounts or posts opening money.
type OnlineReconcilePrepareOptions struct {
	OnlineOptions
	PreviousRunID, ExpectedSourceIdentity, ExpectedTargetShape, ExpectedCaptureHash string
	PreviousCaptureSchema                                                           string
	ExpectedPreviousCaptureEventsOID                                                uint32
}

type OnlineReconcileReport struct {
	RunID     string `json:"run_id"`
	Phase     string `json:"phase"`
	Applied   bool   `json:"applied"`
	Scanned   int64  `json:"scanned_rows"`
	Unchanged int64  `json:"unchanged_rows"`
	Repaired  int64  `json:"repaired_rows"`
	Pruned    int64  `json:"pruned_keys"`
}

// Cursor and counters commit with each target repair. After a snapshot rotation
// every source key is still scanned; new capture events remain unacknowledged
// until this entire baseline completes and ordinary Sync starts.
type onlineReconcileCursor struct {
	Version      int             `json:"version"`
	Source       json.RawMessage `json:"source,omitempty"`
	SourceDone   bool            `json:"source_done"`
	Target       int             `json:"target"`
	TargetCursor json.RawMessage `json:"target_cursor,omitempty"`
	Scanned      int64           `json:"scanned"`
	Unchanged    int64           `json:"unchanged"`
	Repaired     int64           `json:"repaired"`
	Pruned       int64           `json:"pruned"`
}

func ValidateOnlineReconcilePrepareOptions(opts OnlineReconcilePrepareOptions) error {
	if !onlineCaptureRunID.MatchString(opts.RunID) || !onlineCaptureRunID.MatchString(opts.PreviousRunID) || opts.RunID == opts.PreviousRunID {
		return errors.New("legacy: retained reconciliation requires distinct previous and new run IDs")
	}
	for _, value := range []string{opts.ExpectedSourceIdentity, opts.ExpectedTargetShape, opts.ExpectedCaptureHash} {
		if !onlineReviewedSHA.MatchString(value) {
			return errors.New("legacy: retained reconciliation requires reviewed source, target and capture hashes")
		}
	}
	if !onlineCaptureRunID.MatchString(opts.PreviousCaptureSchema) || len(opts.PreviousCaptureSchema) > 63 || !strings.HasPrefix(opts.PreviousCaptureSchema, "v3_") || opts.PreviousCaptureSchema == onlineCaptureSchema || opts.ExpectedPreviousCaptureEventsOID == 0 {
		return errors.New("legacy: retained reconciliation requires a reviewed retired capture schema and journal OID")
	}
	return nil
}

// PrepareRetainedOnline binds a fully installed NEW capture to the SAME unused
// target. The caller must retire the paused capture while preserving its journal
// before installing that new epoch. Previous receipts and progress stay in the
// database; only the new, explicit reconciliation cursor can advance afterwards.
func (m *Importer) PrepareRetainedOnline(ctx context.Context, opts OnlineReconcilePrepareOptions) (OnlineReconcileReport, error) {
	r := OnlineReconcileReport{RunID: opts.RunID}
	if err := ValidateOnlineReconcilePrepareOptions(opts); err != nil {
		return r, err
	}
	if opts.SourceAdmin == nil {
		return r, errors.New("legacy: retained reconciliation requires capture administrator")
	}
	ctx = m.historyContext(ctx)
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
	identity, err := onlineDatabaseIdentity(ctx, source)
	if err != nil || identity != opts.ExpectedSourceIdentity {
		return r, errors.New("legacy: reviewed reconciliation source identity changed")
	}
	capture, err := ValidateOnlineCapture(ctx, source, opts.RunID)
	if err != nil {
		return r, err
	}
	if capture.Sealed {
		return r, errors.New("legacy: retained reconciliation requires an unsealed new capture epoch")
	}
	hash, err := onlineCaptureBindingHash(capture)
	if err != nil || hash != opts.ExpectedCaptureHash {
		return r, errors.New("legacy: reviewed reconciliation capture shape changed")
	}
	sources, err := discoverSources(ctx, source)
	if err != nil {
		return r, err
	}
	specs := onlineSpecs(sources)
	if err := validateOnlineSpecs(capture, specs); err != nil {
		return r, err
	}
	target, err := conn.Begin(ctx)
	if err != nil {
		return r, err
	}
	defer func() { _ = target.Rollback(ctx) }()
	if err := onlineAuthorize(ctx, target, opts.PreviousRunID); err != nil {
		return r, err
	}
	if err := onlineValidateLedgerHistoryMode(ctx, target); err != nil {
		return r, err
	}
	if err := validateHistoryCutoff(ctx, target); err != nil {
		return r, err
	}
	if err := onlineLockTargetSchema(ctx, target); err != nil {
		return r, err
	}
	if err := onlineValidateTargetSchema(ctx, target); err != nil {
		return r, err
	}
	var oldShape, oldHash, phase, targetIdentity string
	if err := target.QueryRow(ctx, "SELECT target_shape,capture_hash,phase,target_identity FROM v3_migration_online.run WHERE singleton").Scan(&oldShape, &oldHash, &phase, &targetIdentity); err != nil {
		return r, err
	}
	actualTarget, err := onlineDatabaseIdentity(ctx, target)
	if err != nil || actualTarget != targetIdentity || oldShape != opts.ExpectedTargetShape || oldHash != opts.ExpectedCaptureHash {
		return r, errors.New("legacy: reviewed retained target identity or shape changed")
	}
	if phase != "copying" && phase != "copied" && phase != "reconciling" {
		return r, errors.New("legacy: retained reconciliation requires an unfinished baseline")
	}
	var previousRun, previousTarget string
	var previousEventsOID, newEventsOID uint32
	var oldTriggers int
	oldConfig := pgx.Identifier{opts.PreviousCaptureSchema, "config"}.Sanitize()
	oldEvents := pgx.Identifier{opts.PreviousCaptureSchema, "events"}.Sanitize()
	if err := source.QueryRow(ctx, "SELECT run_id,COALESCE(target_identity,'') FROM "+oldConfig+" WHERE singleton").Scan(&previousRun, &previousTarget); err != nil {
		return r, err
	}
	if err := source.QueryRow(ctx, `SELECT $1::regclass::oid,'v3_migration_capture.events'::regclass::oid,
	 (SELECT count(*) FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid
	 JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname=$2)`, oldEvents, opts.PreviousCaptureSchema).Scan(&previousEventsOID, &newEventsOID, &oldTriggers); err != nil {
		return r, err
	}
	if previousRun != opts.PreviousRunID || previousTarget != targetIdentity || previousEventsOID != opts.ExpectedPreviousCaptureEventsOID || previousEventsOID == newEventsOID || oldTriggers != 0 {
		return r, errors.New("legacy: retained reconciliation requires the preserved, detached previous journal and a distinct new journal")
	}
	for _, table := range []string{"v3_identity.users", "v3_identity.api_keys", "v3_catalog.channels", "v3_billing.accounts", "v3_billing.ledger_entries", "v3_billing.balance_outbox"} {
		var populated bool
		if err := target.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+")").Scan(&populated); err != nil {
			return r, err
		}
		if populated {
			return r, errors.New("legacy: retained reconciliation cannot adopt native business or money")
		}
	}
	// This is a data receipt in an existing guarded relation, not new schema.
	if _, err := target.Exec(ctx, `INSERT INTO v3_migration_online.progress(name,cursor,complete,copied)
	 SELECT $1,jsonb_build_object('run',to_jsonb(r),'capture_epoch',
	 jsonb_build_object('source_identity',$2::text,'previous_schema',$3::text,
	 'previous_events_oid',$4::bigint,'new_events_oid',$5::bigint),'progress',
	 (SELECT jsonb_agg(to_jsonb(p) ORDER BY p.name) FROM v3_migration_online.progress p
	 WHERE p.name NOT LIKE 'recovery_previous:%')),true,0
	 FROM v3_migration_online.run r WHERE singleton`, "recovery_previous:"+opts.PreviousRunID, identity, opts.PreviousCaptureSchema, previousEventsOID, newEventsOID); err != nil {
		return r, err
	}
	// Preserve the original receipt before refreshing dependencies or reserving
	// identities for newly created users. A failure rolls the receipt back too.
	if _, err := loadOnlineProjectorForEpoch(ctx, source, target, sources, true); err != nil {
		return r, err
	}
	if err := BindOnlineCaptureTarget(ctx, opts.SourceAdmin, opts.RunID, targetIdentity); err != nil {
		return r, err
	}
	if _, err := target.Exec(ctx, "UPDATE v3_migration_online.run SET run_id=$1,phase='reconciling',verified_at=NULL WHERE singleton", opts.RunID); err != nil {
		return r, err
	}
	if _, err := target.Exec(ctx, "SELECT set_config('codego.online_writer',$1,true)", opts.RunID); err != nil {
		return r, err
	}
	for _, spec := range specs {
		changed, err := target.Exec(ctx, `UPDATE v3_migration_online.progress SET cursor='{"version":1}'::jsonb,complete=false,copied=0 WHERE name=$1`, spec.name)
		if err != nil || changed.RowsAffected() != 1 {
			return r, errors.New("legacy: retained reconciliation progress inventory differs")
		}
	}
	if err := target.Commit(ctx); err != nil {
		return r, err
	}
	r.Phase, r.Applied = "reconciling", true
	return r, nil
}

// ReconcileRetainedOnline scans BOTH source rows and target keys. A source-only
// scan cannot find missing DELETE events. Exact typed comparisons keep identical
// rows untouched; changed pages use the existing transactional replay and amount
// checks. Its result is merely 'copied': Sync and full Verify are still required.
func (m *Importer) ReconcileRetainedOnline(ctx context.Context, opts OnlineOptions) (OnlineReconcileReport, error) {
	ctx = m.historyContext(ctx)
	r := OnlineReconcileReport{RunID: opts.RunID}
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
	if err := onlineAuthorize(ctx, init, opts.RunID); err != nil {
		return r, err
	}
	sources, specs, err := onlineBindings(ctx, source, init, opts.RunID)
	if err != nil {
		return r, err
	}
	if err := init.QueryRow(ctx, "SELECT phase FROM v3_migration_online.run WHERE singleton").Scan(&r.Phase); err != nil {
		return r, err
	}
	if r.Phase != "reconciling" {
		return r, errors.New("legacy: retained reconciliation was not explicitly prepared")
	}
	p, err := loadOnlineProjector(ctx, source, init, sources)
	if err != nil {
		return r, err
	}
	if err := onlineRefreshAccounts(ctx, init, p); err != nil {
		return r, err
	}
	if err := init.Commit(ctx); err != nil {
		return r, err
	}
	for _, spec := range specs {
		for {
			target, err := conn.Begin(ctx)
			if err != nil {
				return r, err
			}
			delta, complete, err := p.reconcilePage(ctx, source, target, opts.RunID, spec)
			if err == nil {
				err = target.Commit(ctx)
			}
			if err != nil {
				_ = target.Rollback(ctx)
				return r, err
			}
			r.Scanned += delta.Scanned
			r.Unchanged += delta.Unchanged
			r.Repaired += delta.Repaired
			r.Pruned += delta.Pruned
			if complete {
				break
			}
		}
	}
	finish, err := conn.Begin(ctx)
	if err != nil {
		return r, err
	}
	defer func() { _ = finish.Rollback(ctx) }()
	if err := onlineAuthorize(ctx, finish, opts.RunID); err != nil {
		return r, err
	}
	if sources["logs"] != "" {
		if err := onlineNormalizeLogs(ctx, finish, nil, true); err != nil {
			return r, err
		}
	}
	if _, err := finish.Exec(ctx, "UPDATE v3_migration_online.run SET phase='copied' WHERE singleton AND phase='reconciling'"); err != nil {
		return r, err
	}
	if err := finish.Commit(ctx); err != nil {
		return r, err
	}
	r.Phase, r.Applied = "copied", true
	return r, nil
}

func (p *onlineProjector) reconcilePage(ctx context.Context, source, target pgx.Tx, runID string, spec onlineSpec) (OnlineReconcileReport, bool, error) {
	var r OnlineReconcileReport
	if err := onlineAuthorize(ctx, target, runID); err != nil {
		return r, false, err
	}
	var raw []byte
	var complete bool
	if err := target.QueryRow(ctx, "SELECT cursor,complete FROM v3_migration_online.progress WHERE name=$1 FOR UPDATE", spec.name).Scan(&raw, &complete); err != nil {
		return r, false, err
	}
	if complete {
		return r, true, nil
	}
	var cursor onlineReconcileCursor
	if err := json.Unmarshal(raw, &cursor); err != nil || cursor.Version != 1 {
		return r, false, errors.New("legacy: not a retained reconciliation cursor")
	}
	p.target = target
	if !cursor.SourceDone {
		inputs, err := onlineReadBatch(ctx, source, spec, cursor.Source)
		if err != nil {
			return r, false, err
		}
		if len(inputs) == 0 {
			cursor.SourceDone = true
		} else {
			matches, err := p.matchRetainedInputs(ctx, spec, inputs)
			if err != nil {
				return r, false, err
			}
			var changed []onlineInput
			for i, match := range matches {
				if !match {
					changed = append(changed, inputs[i])
				}
			}
			if len(changed) > 0 {
				if err := p.apply(ctx, spec, changed); err != nil {
					return r, false, err
				}
			}
			cursor.Source = inputs[len(inputs)-1].key
			r.Scanned, r.Repaired = int64(len(inputs)), int64(len(changed))
			r.Unchanged = r.Scanned - r.Repaired
		}
	} else {
		tables := reconcileKeyTables(spec)
		if cursor.Target < 0 || cursor.Target > len(tables) {
			return r, false, errors.New("legacy: invalid retained reconciliation target cursor")
		}
		if cursor.Target == len(tables) {
			complete = true
		} else {
			keys, err := reconcileTargetKeys(ctx, target, spec, tables[cursor.Target], cursor.TargetCursor)
			if err != nil {
				return r, false, err
			}
			if len(keys) == 0 {
				cursor.Target++
				cursor.TargetCursor = nil
			} else {
				absent, err := reconcileMissingInputs(ctx, source, spec, keys)
				if err != nil {
					return r, false, err
				}
				// Read more narrow keys per checkpoint, but retain the original
				// replay bounds. All chunks and the cursor share this transaction.
				for start := 0; start < len(absent); {
					end, bytes := start, 2
					for end < len(absent) && end-start < exactBulkRows {
						encoded, err := json.Marshal(absent[end].key)
						if err != nil {
							return r, false, err
						}
						if len(encoded)+2 > onlineRowMaxBytes {
							return r, false, errors.New("legacy: retained reconciliation encoded key exceeds 64 MiB")
						}
						size := len(encoded) + 1
						if end > start && bytes+size > exactBulkBytes {
							break
						}
						bytes += size
						end++
					}
					if err := p.apply(ctx, spec, absent[start:end]); err != nil {
						return r, false, err
					}
					start = end
				}
				cursor.TargetCursor = keys[len(keys)-1]
				r.Pruned = int64(len(absent))
			}
		}
	}
	cursor.Scanned += r.Scanned
	cursor.Unchanged += r.Unchanged
	cursor.Repaired += r.Repaired
	cursor.Pruned += r.Pruned
	data, err := json.Marshal(cursor)
	if err != nil {
		return r, false, err
	}
	_, err = target.Exec(ctx, "UPDATE v3_migration_online.progress SET cursor=$2,complete=$3,copied=$4 WHERE name=$1", spec.name, data, complete, cursor.Scanned)
	return r, complete, err
}

func reconcileKeyTables(spec onlineSpec) []string {
	var tables []string
	for _, table := range spec.targets {
		// Usage is atomically paired with the authoritative event. Its partition
		// key must come from that event, as in ordinary replay, not an id-only scan.
		if table != "v3_billing.usage_logs" {
			tables = append(tables, table)
		}
	}
	return append(tables, "retired_rows")
}

// The deletion pass needs existence, not every wide source value again. Keep
// source TOAST/content out of this second scan and return only missing keys.
func reconcileMissingInputs(ctx context.Context, source pgx.Tx, spec onlineSpec, keys []json.RawMessage) ([]onlineInput, error) {
	data, err := json.Marshal(keys)
	if err != nil {
		return nil, err
	}
	var conditions []string
	for _, key := range spec.keys {
		q := pgx.Identifier{key}.Sanitize()
		conditions = append(conditions, "t."+q+"=e."+q)
	}
	query := "SELECT j.value FROM jsonb_array_elements($1::jsonb) WITH ORDINALITY j(value,n) CROSS JOIN LATERAL jsonb_populate_record(NULL::" + spec.source + ",j.value)e WHERE NOT EXISTS(SELECT 1 FROM " + spec.source + " t WHERE " + strings.Join(conditions, " AND ") + " AND " + historyWindow(ctx, spec.name, "t", spec.requests, spec.attempts) + ") ORDER BY j.n"
	rows, err := source.Query(ctx, query, data)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var absent []onlineInput
	for rows.Next() {
		var key json.RawMessage
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		absent = append(absent, onlineInput{key: key})
	}
	return absent, rows.Err()
}

func reconcileTargetKeys(ctx context.Context, target pgx.Tx, spec onlineSpec, table string, cursor json.RawMessage) ([]json.RawMessage, error) {
	var query string
	var args []any
	if table == "retired_rows" {
		query = "SELECT row_key FROM v3_migration_online.retired_rows WHERE name=$1"
		args = append(args, spec.name)
		if len(cursor) > 0 {
			query += " AND row_key>$2::jsonb"
			args = append(args, cursor)
		}
		query += " ORDER BY row_key LIMIT " + strconv.Itoa(onlineCopyBatchRows)
	} else {
		stage := onlineStage(table)
		var fields, left, right []string
		for _, key := range spec.keys {
			q := pgx.Identifier{key}.Sanitize()
			fields = append(fields, "'"+key+"'", "t."+q)
			left, right = append(left, "t."+q), append(right, "e."+q)
		}
		query = "SELECT jsonb_build_object(" + strings.Join(fields, ",") + ") FROM " + stage + " t"
		if len(cursor) > 0 {
			query += " WHERE ROW(" + strings.Join(left, ",") + ")>(SELECT " + strings.Join(right, ",") + " FROM jsonb_populate_record(NULL::" + stage + ",$1::jsonb)e)"
			args = append(args, cursor)
		}
		query += " ORDER BY " + strings.Join(left, ",") + " LIMIT " + strconv.Itoa(onlineCopyBatchRows)
	}
	rows, err := target.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []json.RawMessage
	bytes := 0
	for rows.Next() {
		var key json.RawMessage
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		if len(key) > onlineRowMaxBytes {
			return nil, errors.New("legacy: retained reconciliation key exceeds 64 MiB")
		}
		if len(keys) > 0 && bytes+len(key) > exactBulkBytes {
			break
		}
		keys = append(keys, key)
		bytes += len(key)
	}
	return keys, rows.Err()
}

// The typed comparator is SELECT-only. Qualified/singleton request IDs are the
// one deferred derived field, normalized for the complete group at finish.
func (p *onlineProjector) matchRetainedInputs(ctx context.Context, spec onlineSpec, inputs []onlineInput) ([]bool, error) {
	matches := make([]bool, len(inputs))
	for i := range matches {
		matches[i] = true
	}
	if len(inputs) == 0 || spec.name == "ledger_entries" && ledgerHistoryArchived(ctx) {
		return matches, nil
	}
	var parents map[string]bool
	if spec.name == "request_attempt_audits" {
		var err error
		parents, err = p.attemptParents(ctx, inputs)
		if err != nil {
			return nil, err
		}
	}
	type item struct {
		owner int
		row   onlineProjection
	}
	groups := map[string][]item{}
	var shapes []string
	var keys []json.RawMessage
	for _, input := range inputs {
		if len(input.raw) == 0 {
			return nil, errors.New("legacy: retained comparison requires source rows")
		}
	}
	outputs, err := p.projectBatch(ctx, spec, inputs, parents)
	if err != nil {
		return nil, err
	}
	for i, input := range inputs {
		out := outputs[i]
		// The event projection already contains the validated original request
		// ID. Do not decode the same wide log/metadata again for its usage row.
		if spec.name == "logs" && len(out) == 2 {
			requestID, ok := out[0].Values["request_id"].(string)
			if !ok {
				return nil, errors.New("legacy: retained log request ID projection differs")
			}
			if requestID != "" {
				delete(out[1].Values, "request_id")
			}
		}
		for _, row := range out {
			shape := row.Schema + "." + row.Table + ":" + cmRecordShape(cmRecord{values: row.Values})
			if _, exists := groups[shape]; !exists {
				shapes = append(shapes, shape)
			}
			groups[shape] = append(groups[shape], item{i, row})
		}
		keys = append(keys, input.key)
	}
	// Queue the bounded SELECTs on this same transaction. This changes network
	// round trips only: every typed field and every result owner is still checked
	// before any repair or cursor update can run.
	var batch pgx.Batch
	var checks [][]item
	for _, shape := range shapes {
		items := groups[shape]
		for len(items) > 0 {
			n, bytes := 0, 2
			var values []map[string]any
			var encodedRows [][]byte
			for n < len(items) && n < exactBulkRows {
				encoded, err := exactBulkEncodeRow(items[n].row.Values)
				if err != nil || len(encoded)+2 > onlineRowMaxBytes {
					return nil, errors.New("legacy: retained projection encoding exceeds limit")
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
			// Reuse the bytes already validated and sized above. The SQL still
			// compares every typed field; only the duplicate JSON encoding goes.
			_, columns, data, err := exactBulkEncodedInput(row.Schema, row.Table, row.Keys, values, encodedRows)
			if err != nil {
				return nil, err
			}
			batch.Queue(exactBulkCheckQuery(onlineStage(row.Schema+"."+row.Table), row.Keys, columns), data)
			checks = append(checks, items[:n])
			items = items[n:]
		}
	}
	if len(checks) > 0 {
		err := func() (err error) {
			results := p.target.SendBatch(ctx, &batch)
			// Close consumes every remaining result on failure too. A returned
			// error makes the caller roll back this whole page, not just one check.
			defer func() { err = errors.Join(err, results.Close()) }()
			for _, items := range checks {
				rows, err := results.Query()
				if err != nil {
					return err
				}
				n := 0
				for rows.Next() {
					var match bool
					if err := rows.Scan(&match); err != nil {
						rows.Close()
						return err
					}
					if n >= len(items) {
						rows.Close()
						return errors.New("legacy: retained comparison row count differs")
					}
					matches[items[n].owner] = matches[items[n].owner] && match
					n++
				}
				rows.Close()
				if err := rows.Err(); err != nil {
					return err
				}
				if n != len(items) {
					return errors.New("legacy: retained comparison row count differs")
				}
			}
			return nil
		}()
		if err != nil {
			return nil, err
		}
	}
	data, err := json.Marshal(keys)
	if err != nil {
		return nil, err
	}
	rows, err := p.target.Query(ctx, `SELECT r.row_key,r.metrics FROM v3_migration_online.retired_rows r
	 JOIN jsonb_array_elements($2::jsonb) k(value) ON r.row_key=k.value WHERE r.name=$1`, spec.name, data)
	if err != nil {
		return nil, err
	}
	retired := map[string]map[string]string{}
	for rows.Next() {
		var key, raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var keyValues map[string]json.RawMessage
		var metrics map[string]string
		if err := json.Unmarshal(key, &keyValues); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal(raw, &metrics); err != nil {
			rows.Close()
			return nil, err
		}
		canonical, _ := json.Marshal(keyValues)
		retired[string(canonical)] = metrics
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, input := range inputs {
		want, err := p.retiredInputMetrics(spec, input.raw)
		if err != nil {
			return nil, err
		}
		actual := retired[string(input.key)]
		if len(want) == 0 && len(actual) == 0 {
			continue
		}
		matches[i] = matches[i] && reflect.DeepEqual(want, actual)
	}
	return matches, nil
}
