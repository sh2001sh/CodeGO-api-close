package legacy

// Online backups preserve the legacy database before transforming it. Delta
// application is deliberately restricted to a disposable, isolated restore.
import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const onlineDeltaMaxLine = 64 << 20

type OnlineDeltaManifest struct {
	RunID             string `json:"run_id"`
	CaptureHash       string `json:"capture_sha256"`
	SHA256            string `json:"sha256"`
	Records           int64  `json:"records"`
	Snapshot          string `json:"snapshot"`
	SequenceSemantics string `json:"sequence_semantics,omitempty"`
}

type OnlineBaseBackupManifest struct {
	OnlineDeltaManifest
	Path                 string   `json:"path"`
	Bytes                int64    `json:"bytes"`
	ExcludedTriggerNames []string `json:"excluded_trigger_names"`
}

type OnlineDeltaApplyOptions struct {
	RunID string
	// Database must exactly match current_database(), and start online_restore_.
	Database string
	// Explicit disk destination avoids filling the system temporary volume.
	SpoolDirectory string
}

type onlineDeltaRecord struct {
	Kind        string               `json:"kind"`
	RunID       string               `json:"run_id,omitempty"`
	Snapshot    string               `json:"snapshot,omitempty"`
	CaptureHash string               `json:"capture_sha256,omitempty"`
	Tables      []OnlineCaptureTable `json:"tables,omitempty"`
	Table       string               `json:"table,omitempty"`
	Key         json.RawMessage      `json:"key,omitempty"`
	Row         json.RawMessage      `json:"row,omitempty"`
	Sequence    *onlineSequence      `json:"sequence,omitempty"`
	Records     int64                `json:"records,omitempty"`
	SHA256      string               `json:"sha256,omitempty"`
}

type onlineSequence struct {
	Name      string `json:"name"`
	Relation  string `json:"relation"`
	LastValue int64  `json:"last_value"`
	IsCalled  bool   `json:"is_called"`
	Increment int64  `json:"increment"`
	Minimum   int64  `json:"minimum"`
	Maximum   int64  `json:"maximum"`
	Start     int64  `json:"start"`
	Cache     int64  `json:"cache"`
	Cycle     bool   `json:"cycle"`
}

func onlineCaptureHash(report OnlineCaptureReport) (string, error) {
	// Relation OIDs are instance-local and cannot identify a restored database.
	tables := append([]OnlineCaptureTable(nil), report.Tables...)
	for i := range tables {
		tables[i].RelationOID = 0
		tables[i].EstimatedRows = 0
	}
	b, err := json.Marshal(struct {
		RunID  string
		Tables []OnlineCaptureTable
	}{report.RunID, tables})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// ExportOnlineDelta exports all captured keys, including previously acknowledged
// events. Cumulative snapshots are replayable and cannot lose a late commit by
// treating a sequence's maximum ID as a commit watermark. Tables without a PK
// are replaced in full. Memory is bounded by one source row, not table size.
func ExportOnlineDelta(ctx context.Context, source *pgxpool.Pool, runID string, writer io.Writer) (OnlineDeltaManifest, error) {
	var manifest OnlineDeltaManifest
	if source == nil || writer == nil {
		return manifest, errors.New("legacy online: source and writer required")
	}
	tx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return manifest, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	report, err := ValidateOnlineCapture(ctx, tx, runID)
	if err != nil {
		return manifest, err
	}
	captureHash, err := onlineCaptureHash(report)
	if err != nil {
		return manifest, err
	}
	var snapshot string
	if err = tx.QueryRow(ctx, `SELECT txid_current_snapshot()::text`).Scan(&snapshot); err != nil {
		return manifest, err
	}
	h := sha256.New()
	records := int64(0)
	write := func(rec onlineDeltaRecord) error {
		b, e := json.Marshal(rec)
		if e != nil {
			return e
		}
		if len(b)+1 > onlineDeltaMaxLine {
			return errors.New("legacy online: source row exceeds 64 MiB delta limit")
		}
		b = append(b, '\n')
		if _, e = io.MultiWriter(writer, h).Write(b); e == nil {
			records++
		}
		return e
	}
	if err = write(onlineDeltaRecord{Kind: "header", RunID: runID, Snapshot: snapshot, CaptureHash: captureHash, Tables: report.Tables}); err != nil {
		return manifest, err
	}
	for _, table := range report.Tables {
		if len(table.Keys) == 0 {
			if err = write(onlineDeltaRecord{Kind: "reset", Table: table.Name}); err != nil {
				return manifest, err
			}
			rows, e := tx.Query(ctx, `SELECT to_jsonb(t) FROM `+table.Relation+` t`)
			if e != nil {
				return manifest, e
			}
			for rows.Next() {
				var row []byte
				if e = rows.Scan(&row); e != nil {
					rows.Close()
					return manifest, e
				}
				if e = write(onlineDeltaRecord{Kind: "row", Table: table.Name, Row: json.RawMessage(row)}); e != nil {
					rows.Close()
					return manifest, e
				}
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return manifest, e
			}
			continue
		}
		parts := make([]string, len(table.Keys))
		for i, key := range table.Keys {
			col := pgx.Identifier{key}.Sanitize()
			parts[i] = `t.` + col + ` = (jsonb_populate_record(NULL::` + table.Relation + `,k.row_key)).` + col
		}
		rows, e := tx.Query(ctx, `WITH k AS (SELECT DISTINCT row_key FROM v3_migration_capture.events WHERE table_name=$1) SELECT k.row_key,to_jsonb(t) FROM k LEFT JOIN `+table.Relation+` t ON `+strings.Join(parts, " AND "), table.Name)
		if e != nil {
			return manifest, e
		}
		for rows.Next() {
			var key, row []byte
			if e = rows.Scan(&key, &row); e != nil {
				rows.Close()
				return manifest, e
			}
			if e = write(onlineDeltaRecord{Kind: "delete", Table: table.Name, Key: json.RawMessage(key)}); e != nil {
				rows.Close()
				return manifest, e
			}
			if len(row) > 0 && string(row) != "null" {
				if e = write(onlineDeltaRecord{Kind: "row", Table: table.Name, Row: json.RawMessage(row)}); e != nil {
					rows.Close()
					return manifest, e
				}
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return manifest, e
		}
	}
	sequences, err := onlineSequences(ctx, tx)
	if err != nil {
		return manifest, err
	}
	for _, sequence := range sequences {
		if err = write(onlineDeltaRecord{Kind: "sequence", Sequence: &sequence}); err != nil {
			return manifest, err
		}
	}
	manifest = OnlineDeltaManifest{RunID: runID, CaptureHash: captureHash, Snapshot: snapshot, Records: records, SHA256: hex.EncodeToString(h.Sum(nil))}
	trailer, err := json.Marshal(onlineDeltaRecord{Kind: "trailer", Records: records, SHA256: manifest.SHA256})
	if err != nil {
		return manifest, err
	}
	trailer = append(trailer, '\n')
	var written int
	written, err = writer.Write(trailer)
	if err == nil && written != len(trailer) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return manifest, err
	}
	return manifest, tx.Commit(ctx)
}

func onlineSequences(ctx context.Context, tx pgx.Tx) ([]onlineSequence, error) {
	rows, err := tx.Query(ctx, `SELECT n.nspname||'.'||c.relname,n.nspname,c.relname,s.seqincrement,s.seqmin,s.seqmax,s.seqstart,s.seqcache,s.seqcycle FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_sequence s ON s.seqrelid=c.oid WHERE n.nspname<>'information_schema' AND n.nspname NOT LIKE 'pg_%' AND n.nspname NOT LIKE 'v3_%' ORDER BY n.nspname,c.relname`)
	if err != nil {
		return nil, err
	}
	var result []onlineSequence
	for rows.Next() {
		var s onlineSequence
		var schema, name string
		if err = rows.Scan(&s.Name, &schema, &name, &s.Increment, &s.Minimum, &s.Maximum, &s.Start, &s.Cache, &s.Cycle); err != nil {
			rows.Close()
			return nil, err
		}
		s.Relation = pgx.Identifier{schema, name}.Sanitize()
		result = append(result, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range result {
		if err = tx.QueryRow(ctx, `SELECT last_value,is_called FROM `+result[i].Relation).Scan(&result[i].LastValue, &result[i].IsCalled); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func onlineScanFile(file *os.File, visit func(onlineDeltaRecord) error) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), onlineDeltaMaxLine)
	for scanner.Scan() {
		var rec onlineDeltaRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			return errors.New("legacy online: malformed delta record")
		}
		if err := visit(rec); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func onlineReadDelta(ctx context.Context, reader io.Reader, spoolDirectory string) (*os.File, onlineDeltaRecord, OnlineDeltaManifest, error) {
	var header onlineDeltaRecord
	var manifest OnlineDeltaManifest
	file, err := os.CreateTemp(spoolDirectory, "codego-online-delta-*.partial")
	if err != nil {
		return nil, header, manifest, err
	}
	success := false
	defer func() {
		if !success {
			_ = file.Close()
			_ = os.Remove(file.Name())
		}
	}()
	h := sha256.New()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), onlineDeltaMaxLine)
	var count int64
	trailer := false
	for scanner.Scan() {
		if err = ctx.Err(); err != nil {
			return nil, header, manifest, err
		}
		line := scanner.Bytes()
		var rec onlineDeltaRecord
		if err = json.Unmarshal(line, &rec); err != nil {
			return nil, header, manifest, errors.New("legacy online: malformed delta record")
		}
		if trailer {
			return nil, header, manifest, errors.New("legacy online: data after trailer")
		}
		if rec.Kind == "trailer" {
			trailer = true
			if rec.Records != count || rec.SHA256 != hex.EncodeToString(h.Sum(nil)) {
				return nil, header, manifest, errors.New("legacy online: delta checksum or count mismatch")
			}
			manifest = OnlineDeltaManifest{RunID: header.RunID, CaptureHash: header.CaptureHash, Snapshot: header.Snapshot, Records: count, SHA256: rec.SHA256}
			continue
		}
		if count == 0 {
			if rec.Kind != "header" || rec.RunID == "" || len(rec.Tables) == 0 {
				return nil, header, manifest, errors.New("legacy online: missing delta header")
			}
			header = rec
		} else if rec.Kind != "delete" && rec.Kind != "row" && rec.Kind != "reset" && rec.Kind != "sequence" {
			return nil, header, manifest, errors.New("legacy online: unexpected delta record")
		}
		if _, err = file.Write(line); err != nil {
			return nil, header, manifest, err
		}
		if _, err = file.Write([]byte{'\n'}); err != nil {
			return nil, header, manifest, err
		}
		_, _ = h.Write(line)
		_, _ = h.Write([]byte{'\n'})
		count++
	}
	if err = scanner.Err(); err != nil {
		return nil, header, manifest, err
	}
	if !trailer {
		return nil, header, manifest, errors.New("legacy online: missing delta trailer")
	}
	expected, err := onlineCaptureHash(OnlineCaptureReport{RunID: header.RunID, Tables: header.Tables})
	if err != nil || expected != header.CaptureHash {
		return nil, header, manifest, errors.New("legacy online: capture checksum mismatch")
	}
	success = true
	return file, header, manifest, nil
}

// ApplyOnlineDelta validates the complete transport before opening a write
// transaction. All deletes precede inserts; temporarily bypassed FK triggers
// are validated against their original definitions before committing.
func ApplyOnlineDelta(ctx context.Context, restored *pgxpool.Pool, reader io.Reader, opts OnlineDeltaApplyOptions) (OnlineDeltaManifest, error) {
	var empty OnlineDeltaManifest
	if restored == nil || reader == nil || opts.RunID == "" || !strings.HasPrefix(opts.Database, "online_restore_") || len(opts.Database) <= len("online_restore_") || !filepath.IsAbs(opts.SpoolDirectory) {
		return empty, errors.New("legacy online: explicit isolated restore database and run ID required")
	}
	file, header, manifest, err := onlineReadDelta(ctx, reader, opts.SpoolDirectory)
	if err != nil {
		return empty, err
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	if header.RunID != opts.RunID {
		return empty, errors.New("legacy online: delta run ID mismatch")
	}
	tx, err := restored.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return empty, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var database string
	if err = tx.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		return empty, err
	}
	if database != opts.Database {
		return empty, errors.New("legacy online: isolated restore database mismatch")
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(738301033)`); err != nil {
		return empty, err
	}
	var externalTriggers bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE NOT t.tgisinternal AND t.tgenabled IN ('A','R') AND n.nspname NOT LIKE 'pg_%' AND n.nspname NOT LIKE 'v3_%')`).Scan(&externalTriggers); err != nil {
		return empty, err
	}
	if externalTriggers {
		return empty, errors.New("legacy online: replica/always business triggers require an explicit isolated restore strategy")
	}
	tables := map[string]OnlineCaptureTable{}
	restoredTables, err := discoverOnlineCaptureTables(ctx, tx)
	if err != nil {
		return empty, err
	}
	if len(restoredTables) != len(header.Tables) {
		return empty, errors.New("legacy online: restored source table coverage mismatch")
	}
	columns := map[string]string{}
	for i, table := range header.Tables {
		if _, exists := tables[table.Name]; exists {
			return empty, errors.New("legacy online: duplicate table")
		}
		actual := restoredTables[i]
		if actual.Name != table.Name || actual.Relation != table.Relation || actual.SchemaFingerprint != table.SchemaFingerprint || strings.Join(actual.Keys, "\x00") != strings.Join(table.Keys, "\x00") {
			return empty, errors.New("legacy online: restored table shape differs from baseline")
		}
		rows, e := tx.Query(ctx, `SELECT a.attname FROM pg_attribute a WHERE a.attrelid=$1::regclass AND a.attnum>0 AND NOT a.attisdropped AND a.attgenerated='' ORDER BY a.attnum`, table.Relation)
		if e != nil {
			return empty, e
		}
		var cols []string
		for rows.Next() {
			var col string
			if e = rows.Scan(&col); e != nil {
				rows.Close()
				return empty, e
			}
			cols = append(cols, pgx.Identifier{col}.Sanitize())
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return empty, e
		}
		columns[table.Name] = strings.Join(cols, ",")
		tables[table.Name] = table
	}
	if _, err = tx.Exec(ctx, `SET LOCAL session_replication_role=replica`); err != nil {
		return empty, errors.New("legacy online: isolated restore requires replication-role privilege")
	}
	resets := map[string]bool{}
	sequences := map[string]bool{}
	actualSequences, err := onlineSequences(ctx, tx)
	if err != nil {
		return empty, err
	}
	batch := &pgx.Batch{}
	batchBytes := 0
	flush := func() error {
		if batch.Len() == 0 {
			return nil
		}
		results := tx.SendBatch(ctx, batch)
		e := results.Close()
		batch = &pgx.Batch{}
		batchBytes = 0
		return e
	}
	queue := func(sql string, arg []byte) error {
		batch.Queue(sql, arg)
		batchBytes += len(arg)
		if batch.Len() >= 128 || batchBytes >= 8<<20 {
			return flush()
		}
		return nil
	}
	err = onlineScanFile(file, func(rec onlineDeltaRecord) error {
		if rec.Kind == "header" || rec.Kind == "row" || rec.Kind == "sequence" {
			return nil
		}
		table, ok := tables[rec.Table]
		if !ok {
			return errors.New("legacy online: unregistered delta table")
		}
		if rec.Kind == "reset" {
			if len(table.Keys) > 0 || resets[table.Name] {
				return errors.New("legacy online: invalid whole-table replacement")
			}
			resets[table.Name] = true
			batch.Queue(`DELETE FROM ` + table.Relation)
			if batch.Len() >= 128 {
				return flush()
			}
			return nil
		}
		if len(table.Keys) == 0 || !json.Valid(rec.Key) || len(rec.Key) == 0 || rec.Key[0] != '{' {
			return errors.New("legacy online: invalid primary key delta")
		}
		var key map[string]json.RawMessage
		if e := json.Unmarshal(rec.Key, &key); e != nil {
			return e
		}
		if len(key) != len(table.Keys) {
			return errors.New("legacy online: incomplete primary key")
		}
		parts := make([]string, len(table.Keys))
		for i, k := range table.Keys {
			v, exists := key[k]
			if !exists || string(v) == "null" {
				return errors.New("legacy online: null primary key")
			}
			col := pgx.Identifier{k}.Sanitize()
			parts[i] = `t.` + col + ` = (jsonb_populate_record(NULL::` + table.Relation + `,$1::jsonb)).` + col
		}
		return queue(`DELETE FROM `+table.Relation+` t WHERE `+strings.Join(parts, " AND "), []byte(rec.Key))
	})
	if err != nil {
		return empty, err
	}
	if err = flush(); err != nil {
		return empty, err
	}
	for _, table := range header.Tables {
		if len(table.Keys) == 0 && !resets[table.Name] {
			return empty, errors.New("legacy online: omitted whole-table snapshot")
		}
	}
	err = onlineScanFile(file, func(rec onlineDeltaRecord) error {
		if rec.Kind != "row" {
			return nil
		}
		table, ok := tables[rec.Table]
		if !ok || !json.Valid(rec.Row) || len(rec.Row) == 0 || rec.Row[0] != '{' {
			return errors.New("legacy online: invalid delta row")
		}
		// Generated columns are recomputed; identity values retain legacy IDs.
		cols := columns[table.Name]
		// Check the complete typed row, including generated fields. Division by
		// zero deliberately aborts if a restored function recomputes differently.
		return queue(`INSERT INTO `+table.Relation+` AS codego_delta_inserted (`+cols+`) OVERRIDING SYSTEM VALUE SELECT `+cols+` FROM jsonb_populate_recordset(NULL::`+table.Relation+`,jsonb_build_array($1::jsonb)) RETURNING 1 / CASE WHEN to_jsonb(codego_delta_inserted)=to_jsonb(jsonb_populate_record(NULL::`+table.Relation+`,$1::jsonb)) THEN 1 ELSE 0 END`, []byte(rec.Row))
	})
	if err != nil {
		return empty, err
	}
	if err = flush(); err != nil {
		return empty, err
	}
	if _, err = tx.Exec(ctx, `SET LOCAL session_replication_role=origin`); err != nil {
		return empty, err
	}
	if err = onlineValidateForeignKeys(ctx, tx, header.Tables); err != nil {
		return empty, err
	}
	err = onlineScanFile(file, func(rec onlineDeltaRecord) error {
		if rec.Kind != "sequence" {
			return nil
		}
		s := rec.Sequence
		if s == nil || sequences[s.Name] {
			return errors.New("legacy online: invalid or duplicate sequence")
		}
		sequences[s.Name] = true
		var actual onlineSequence
		var schema, name string
		e := tx.QueryRow(ctx, `SELECT n.nspname,c.relname,q.seqincrement,q.seqmin,q.seqmax,q.seqstart,q.seqcache,q.seqcycle FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_sequence q ON q.seqrelid=c.oid WHERE n.nspname||'.'||c.relname=$1 AND n.nspname NOT LIKE 'v3\_%' ESCAPE '\'`, s.Name).Scan(&schema, &name, &actual.Increment, &actual.Minimum, &actual.Maximum, &actual.Start, &actual.Cache, &actual.Cycle)
		if e != nil || (pgx.Identifier{schema, name}).Sanitize() != s.Relation || actual.Increment != s.Increment || actual.Minimum != s.Minimum || actual.Maximum != s.Maximum || actual.Start != s.Start || actual.Cache != s.Cache || actual.Cycle != s.Cycle {
			return errors.New("legacy online: restore sequence definition mismatch")
		}
		next, e := onlineSequenceNext(*s)
		if e != nil {
			return e
		}
		// ALTER RESTART is transactional; setval is not. This preserves nextval
		// semantics while representing the sequence with is_called=false.
		_, e = tx.Exec(ctx, `ALTER SEQUENCE `+s.Relation+` RESTART WITH `+fmt.Sprint(next))
		return e
	})
	if err != nil {
		return empty, err
	}
	if len(sequences) != len(actualSequences) {
		return empty, errors.New("legacy online: incomplete sequence coverage")
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	manifest.SequenceSemantics = "nextval preserved; transactional RESTART sets is_called=false"
	return manifest, nil
}

func onlineSequenceNext(s onlineSequence) (int64, error) {
	if s.Increment == 0 || s.Minimum > s.Maximum || s.LastValue < s.Minimum || s.LastValue > s.Maximum {
		return 0, errors.New("legacy online: invalid sequence state")
	}
	if !s.IsCalled {
		return s.LastValue, nil
	}
	next := new(big.Int).Add(big.NewInt(s.LastValue), big.NewInt(s.Increment))
	if next.Cmp(big.NewInt(s.Maximum)) > 0 {
		if !s.Cycle {
			return 0, errors.New("legacy online: exhausted sequence")
		}
		return s.Minimum, nil
	}
	if next.Cmp(big.NewInt(s.Minimum)) < 0 {
		if !s.Cycle {
			return 0, errors.New("legacy online: exhausted sequence")
		}
		return s.Maximum, nil
	}
	return next.Int64(), nil
}

func onlineValidateForeignKeys(ctx context.Context, tx pgx.Tx, tables []OnlineCaptureTable) error {
	for _, table := range tables {
		rows, err := tx.Query(ctx, `SELECT c.conname,pg_get_constraintdef(c.oid),r.relkind='p' FROM pg_constraint c JOIN pg_class r ON r.oid=c.conrelid WHERE c.conrelid=$1::regclass AND c.contype='f'`, table.Relation)
		if err != nil {
			return err
		}
		type constraint struct {
			name, definition string
			partitioned      bool
		}
		var constraints []constraint
		for rows.Next() {
			var c constraint
			if err = rows.Scan(&c.name, &c.definition, &c.partitioned); err != nil {
				rows.Close()
				return err
			}
			constraints = append(constraints, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, c := range constraints {
			// Add a temporary duplicate NOT VALID FK, then explicitly validate it.
			// Existing validated constraints would otherwise skip a recheck.
			h := sha256.Sum256([]byte(table.Name + "/" + c.name))
			name := pgx.Identifier{"v3_online_check_" + hex.EncodeToString(h[:8])}.Sanitize()
			definition := strings.TrimSuffix(c.definition, " NOT VALID")
			if c.partitioned {
				// PostgreSQL disallows NOT VALID on partitioned FK roots. Adding
				// a normal duplicate checks their existing rows immediately.
				if _, err = tx.Exec(ctx, `ALTER TABLE `+table.Relation+` ADD CONSTRAINT `+name+` `+definition); err != nil {
					return err
				}
				if _, err = tx.Exec(ctx, `ALTER TABLE `+table.Relation+` DROP CONSTRAINT `+name); err != nil {
					return err
				}
				continue
			}
			if _, err = tx.Exec(ctx, `ALTER TABLE `+table.Relation+` ADD CONSTRAINT `+name+` `+definition+` NOT VALID`); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `ALTER TABLE `+table.Relation+` VALIDATE CONSTRAINT `+name); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `ALTER TABLE `+table.Relation+` DROP CONSTRAINT `+name); err != nil {
				return err
			}
		}
	}
	return nil
}

// OnlineBaseBackup holds the exported snapshot until pg_dump finishes. pg_dump
// receives its connection string through an environment variable, never argv.
func OnlineBaseBackup(ctx context.Context, source *pgxpool.Pool, runID, destination string) (OnlineBaseBackupManifest, error) {
	var manifest OnlineBaseBackupManifest
	if source == nil || runID == "" || !filepath.IsAbs(destination) {
		return manifest, errors.New("legacy online: source, run ID and absolute backup destination required")
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return manifest, errors.New("legacy online: backup destination already exists or inaccessible")
	}
	partial := destination + ".partial"
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return manifest, err
	}
	_ = file.Close()
	defer func() { _ = os.Remove(partial) }()
	tx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return manifest, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	report, err := ValidateOnlineCapture(ctx, tx, runID)
	if err != nil {
		return manifest, err
	}
	captureHash, err := onlineCaptureHash(report)
	if err != nil {
		return manifest, err
	}
	var snapshot string
	if err = tx.QueryRow(ctx, `SELECT pg_export_snapshot()`).Scan(&snapshot); err != nil {
		return manifest, err
	}
	command := exec.CommandContext(ctx, "pg_dump", "--format=custom", "--no-password", "--exclude-schema=v3_migration_capture", "--snapshot="+snapshot, "--file="+partial)
	// Replace inherited endpoint settings with the pool's effective endpoint.
	// TLS file paths/mode stay in environment variables, never command argv.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "PG") {
			command.Env = append(command.Env, entry)
		}
	}
	dumpEnv, err := onlineDumpEnvironment(source.Config().ConnConfig)
	if err != nil {
		return manifest, err
	}
	command.Env = append(command.Env, dumpEnv...)
	if err = command.Run(); err != nil {
		return manifest, fmt.Errorf("legacy online: pg_dump failed: %w", err)
	}
	file, err = os.Open(partial)
	if err != nil {
		return manifest, err
	}
	h := sha256.New()
	bytes, copyErr := io.Copy(h, file)
	closeErr := file.Close()
	if copyErr != nil {
		return manifest, copyErr
	}
	if closeErr != nil {
		return manifest, closeErr
	}
	if err = tx.Commit(ctx); err != nil {
		return manifest, err
	}
	// Hard link publishes without overwriting a destination created meanwhile.
	if err = os.Link(partial, destination); err != nil {
		return manifest, err
	}
	manifest = OnlineBaseBackupManifest{OnlineDeltaManifest: OnlineDeltaManifest{RunID: runID, CaptureHash: captureHash, SHA256: hex.EncodeToString(h.Sum(nil)), Snapshot: snapshot}, Path: destination, Bytes: bytes, ExcludedTriggerNames: []string{onlineCaptureRowTrigger, onlineCaptureTruncateTrigger, "codego_online_capture_write_fence"}}
	return manifest, nil
}

func onlineDumpEnvironment(config *pgx.ConnConfig) ([]string, error) {
	original := config.ConnString()
	options := map[string]string{}
	if strings.HasPrefix(original, "postgres://") || strings.HasPrefix(original, "postgresql://") {
		parsed, err := url.Parse(original)
		if err != nil {
			return nil, errors.New("legacy online: invalid dump connection")
		}
		query := parsed.Query()
		for key := range query {
			options[key] = query.Get(key)
		}
	} else {
		var err error
		options, err = onlineConnectionOptions(original)
		if err != nil {
			return nil, err
		}
	}
	// Config endpoint fields can have changed after ParseConfig (test fixtures,
	// socket handoffs). ConnString alone retains the original database name.
	values := map[string]string{"PGHOST": config.Host, "PGPORT": strconv.Itoa(int(config.Port)), "PGUSER": config.User, "PGPASSWORD": config.Password, "PGDATABASE": config.Database}
	for key, env := range map[string]string{"sslmode": "PGSSLMODE", "sslrootcert": "PGSSLROOTCERT", "sslcert": "PGSSLCERT", "sslkey": "PGSSLKEY", "sslcrl": "PGSSLCRL", "sslcrldir": "PGSSLCRLDIR", "sslsni": "PGSSLSNI", "sslpassword": "PGSSLPASSWORD", "channel_binding": "PGCHANNELBINDING", "target_session_attrs": "PGTARGETSESSIONATTRS", "options": "PGOPTIONS", "application_name": "PGAPPNAME", "connect_timeout": "PGCONNECT_TIMEOUT", "gssencmode": "PGGSSENCMODE", "krbsrvname": "PGKRBSRVNAME", "requirepeer": "PGREQUIREPEER"} {
		if value, exists := options[key]; exists {
			values[env] = value
		} else if value, exists := os.LookupEnv(env); exists {
			values[env] = value
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result, nil
}

func onlineConnectionOptions(input string) (map[string]string, error) {
	result := map[string]string{}
	space := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
	for i := 0; i < len(input); {
		for i < len(input) && space(input[i]) {
			i++
		}
		if i == len(input) {
			break
		}
		start := i
		for i < len(input) && input[i] != '=' && !space(input[i]) {
			i++
		}
		key := input[start:i]
		for i < len(input) && space(input[i]) {
			i++
		}
		if key == "" || i == len(input) || input[i] != '=' {
			return nil, errors.New("legacy online: unsupported dump connection syntax")
		}
		i++
		for i < len(input) && space(input[i]) {
			i++
		}
		var value strings.Builder
		quoted := i < len(input) && input[i] == '\''
		if quoted {
			i++
		}
		closed := !quoted
		for i < len(input) {
			c := input[i]
			if c == '\\' {
				i++
				if i == len(input) {
					return nil, errors.New("legacy online: malformed connection escape")
				}
				value.WriteByte(input[i])
				i++
				continue
			}
			if quoted && c == '\'' {
				i++
				closed = true
				break
			}
			if !quoted && space(c) {
				break
			}
			value.WriteByte(c)
			i++
		}
		if !closed {
			return nil, errors.New("legacy online: unclosed connection value")
		}
		result[key] = value.String()
	}
	return result, nil
}
