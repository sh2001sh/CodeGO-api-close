package legacy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const SourceDrainProtocol = "codego-v2-canonical-drain-v1"

var ErrSourceDrainBlocked = errors.New("source drain has unresolved blockers")
var drainSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)
var drainSnapshot = regexp.MustCompile(`^[0-9A-Fa-f]{1,32}-[0-9A-Fa-f]{1,32}-[0-9]{1,20}$`)

type SourceDrainOptions struct {
	ExportedSnapshot     string
	FrozenManifestSHA256 string
}

type SourceDrainIdentity struct {
	Database         string    `json:"database"`
	DatabaseOID      int64     `json:"database_oid"`
	SystemIdentifier string    `json:"system_identifier"`
	ServerAddr       string    `json:"server_addr"`
	ServerPort       int       `json:"server_port"`
	ServerVersionNum int       `json:"server_version_num"`
	TimeZone         string    `json:"time_zone"`
	ReadOnly         bool      `json:"read_only"`
	Snapshot         string    `json:"snapshot"`
	ExportedSnapshot string    `json:"exported_snapshot"`
	StartedAt        time.Time `json:"started_at"`
	CompletedAt      time.Time `json:"completed_at"`
	IdentitySHA256   string    `json:"identity_sha256"`
}

type SourceDrainCheck struct {
	Present   bool `json:"present"`
	Completed bool `json:"completed"`
}

type SourceDrainBackgroundRow struct {
	ID          string `json:"id"`
	RequestHash string `json:"request_hash"`
	RowSHA256   string `json:"row_sha256"`
}

type SourceDrainBackground struct {
	Count  int64                      `json:"count"`
	Hashes []string                   `json:"hashes"`
	SHA256 string                     `json:"sha256"`
	Rows   []SourceDrainBackgroundRow `json:"rows"`
}

type SourceDrainProjection struct {
	RequestID           string `json:"request_id"`
	RequestHash         string `json:"request_hash"`
	RowSHA256           string `json:"row_sha256"`
	BackgroundID        string `json:"background_id"`
	BackgroundRowSHA256 string `json:"background_row_sha256"`
	OwnerKeyMatch       bool   `json:"owner_key_match"`
}

type SourceDrainReport struct {
	Protocol                  string                      `json:"protocol"`
	Completed                 bool                        `json:"completed"`
	BinarySHA256              string                      `json:"binary_sha256"`
	FrozenManifestSHA256      string                      `json:"frozen_manifest_sha256"`
	Source                    SourceDrainIdentity         `json:"source"`
	Checks                    map[string]SourceDrainCheck `json:"checks"`
	Report                    Report                      `json:"report"`
	PendingBackground         SourceDrainBackground       `json:"pending_background"`
	UndrainedAudits           []SourceDrainProjection     `json:"undrained_audits"`
	UndrainedExecutions       []SourceDrainProjection     `json:"undrained_executions"`
	ProjectionAssertionSQL    string                      `json:"projection_assertion_sql"`
	ProjectionAssertionSHA256 string                      `json:"projection_assertion_sha256"`
	ErrorCode                 string                      `json:"error_code,omitempty"`
}

// NewSourceDrainReport also fingerprints the actual running executable inode
// on Linux. Replacing its pathname cannot substitute another binary's hash.
func NewSourceDrainReport(options SourceDrainOptions) (SourceDrainReport, error) {
	r := SourceDrainReport{Protocol: SourceDrainProtocol, FrozenManifestSHA256: options.FrozenManifestSHA256,
		Checks: map[string]SourceDrainCheck{}, Report: Report{Issues: []Issue{}, Counts: map[string]int64{}, Amounts: map[string]string{}, UnmappedSources: []string{}},
		PendingBackground: SourceDrainBackground{Hashes: []string{}, Rows: []SourceDrainBackgroundRow{}},
		UndrainedAudits:   []SourceDrainProjection{}, UndrainedExecutions: []SourceDrainProjection{}}
	r.Source.ExportedSnapshot = options.ExportedSnapshot
	if options.FrozenManifestSHA256 != "" && !drainSHA.MatchString(options.FrozenManifestSHA256) {
		r.ErrorCode = "invalid_frozen_manifest_sha256"
		return r, errors.New(r.ErrorCode)
	}
	if options.ExportedSnapshot != "" && !drainSnapshot.MatchString(options.ExportedSnapshot) {
		r.ErrorCode = "invalid_exported_snapshot"
		return r, errors.New(r.ErrorCode)
	}
	path, err := os.Executable()
	if err != nil {
		r.ErrorCode = "executable_hash_failed"
		return r, err
	}
	if runtime.GOOS == "linux" {
		path = "/proc/self/exe"
	}
	file, err := os.Open(path)
	if err != nil {
		r.ErrorCode = "executable_hash_failed"
		return r, err
	}
	defer func() { _ = file.Close() }()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		r.ErrorCode = "executable_hash_failed"
		return r, err
	}
	r.BinarySHA256 = hex.EncodeToString(digest.Sum(nil))
	return r, nil
}

// DrainSource inspects the canonical source without target, assets, encryption
// keys or skip policies. Completed means the entire audit ran, not that its
// blockers can be ignored: all reported issues return ErrSourceDrainBlocked.
func DrainSource(ctx context.Context, source *pgxpool.Pool, options SourceDrainOptions) (SourceDrainReport, error) {
	r, err := NewSourceDrainReport(options)
	if err != nil {
		return r, err
	}
	if source == nil {
		r.ErrorCode = "source_database_required"
		return r, errors.New(r.ErrorCode)
	}
	tx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		r.ErrorCode = "source_snapshot_failed"
		return r, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if options.ExportedSnapshot != "" {
		// Grammar validation above permits no quote, whitespace or SQL token.
		if _, err := tx.Exec(ctx, "SET TRANSACTION SNAPSHOT '"+options.ExportedSnapshot+"'"); err != nil {
			r.ErrorCode = "source_snapshot_failed"
			return r, err
		}
	}
	r.ErrorCode = "source_drain_incomplete"
	if err := r.readIdentity(ctx, tx); err != nil {
		return r, err
	}
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		return r, err
	}
	if err := validateKnownSources(ctx, tx, sources, &r.Report); err != nil {
		return r, err
	}
	r.Checks["known_source_coverage"] = SourceDrainCheck{Present: true, Completed: true}
	keys, err := loadRows(ctx, tx, sources["tokens"])
	if err != nil {
		return r, err
	}
	if err := validateSourceState(ctx, tx, sources, keys, &r.Report); err != nil {
		return r, err
	}
	for _, alias := range []string{"billing_reservations", "billing_settlements", "billing_outbox_events", "gateway_responses_background_jobs", "tasks", "workflow_task_workflows", "workflow_task_terminal_results", "gateway_request_executions", "gateway_request_audits"} {
		_, checked := r.Report.Counts["in_flight:"+alias]
		r.Checks[alias] = SourceDrainCheck{Present: checked, Completed: true}
	}
	r.Checks["accounts"] = SourceDrainCheck{Present: sources["accounts"] != "", Completed: true}
	if sources["accounts"] != "" {
		var checked int64
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+sources["accounts"]+" a LEFT JOIN "+sources["balance_snapshots"]+" s ON s.account_id=a.account_id").Scan(&checked); err != nil {
			return r, err
		}
		r.Report.Counts["source_accounts_checked"] = checked
	}
	if err := r.collectPendingBackground(ctx, tx, sources); err != nil {
		return r, err
	}
	if err := r.collectProjectionEvidence(ctx, tx, sources); err != nil {
		return r, err
	}
	if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&r.Source.CompletedAt); err != nil {
		return r, err
	}
	if err := tx.Commit(ctx); err != nil {
		return r, err
	}
	r.Completed = true
	r.ErrorCode = ""
	if len(r.Report.Issues) != 0 {
		return r, ErrSourceDrainBlocked
	}
	return r, nil
}

func (r *SourceDrainReport) readIdentity(ctx context.Context, tx pgx.Tx) error {
	var readonly string
	err := tx.QueryRow(ctx, `SELECT current_database(),(SELECT oid::bigint FROM pg_database WHERE datname=current_database()),
	 (pg_control_system()).system_identifier::text,COALESCE(inet_server_addr()::text,''),COALESCE(inet_server_port(),0),
	 current_setting('server_version_num')::int,current_setting('TimeZone'),current_setting('transaction_read_only'),
	 pg_current_snapshot()::text,transaction_timestamp()`).Scan(&r.Source.Database, &r.Source.DatabaseOID, &r.Source.SystemIdentifier,
		&r.Source.ServerAddr, &r.Source.ServerPort, &r.Source.ServerVersionNum, &r.Source.TimeZone, &readonly, &r.Source.Snapshot, &r.Source.StartedAt)
	if err != nil {
		return err
	}
	r.Source.ReadOnly = readonly == "on"
	if !r.Source.ReadOnly {
		return errors.New("source transaction must be read only")
	}
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(map[string]any{"database": r.Source.Database, "database_oid": r.Source.DatabaseOID,
		"system_identifier": r.Source.SystemIdentifier, "server_addr": r.Source.ServerAddr, "server_port": r.Source.ServerPort,
		"server_version_num": r.Source.ServerVersionNum, "time_zone": r.Source.TimeZone}); err != nil {
		return err
	}
	r.Source.IdentitySHA256 = drainDigest(bytes.TrimSuffix(data.Bytes(), []byte("\n")))
	return nil
}

func drainDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (r *SourceDrainReport) collectPendingBackground(ctx context.Context, tx pgx.Tx, sources map[string]string) error {
	table := backgroundSourceTable(sources, "responses_background_jobs")
	if table != "" {
		rows, err := tx.Query(ctx, "SELECT id,left(md5(id),12),encode(sha256(convert_to(to_jsonb(t)::text,'UTF8')),'hex') FROM "+table+" t WHERE COALESCE(status,'') NOT IN ('completed','failed','cancelled') ORDER BY id")
		if err != nil {
			return err
		}
		for rows.Next() {
			var row SourceDrainBackgroundRow
			if err := rows.Scan(&row.ID, &row.RequestHash, &row.RowSHA256); err != nil {
				rows.Close()
				return err
			}
			r.PendingBackground.Rows = append(r.PendingBackground.Rows, row)
			r.PendingBackground.Hashes = append(r.PendingBackground.Hashes, row.RequestHash)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	r.PendingBackground.Count = int64(len(r.PendingBackground.Rows))
	sort.Strings(r.PendingBackground.Hashes)
	data, err := json.Marshal(r.PendingBackground.Hashes)
	if err != nil {
		return err
	}
	r.PendingBackground.SHA256 = drainDigest(data)
	return nil
}

func (r *SourceDrainReport) collectProjectionEvidence(ctx context.Context, tx pgx.Tx, sources map[string]string) error {
	var assertions strings.Builder
	background := backgroundSourceTable(sources, "responses_background_jobs")
	if background != "" {
		assertions.WriteString("IF EXISTS(SELECT 1 FROM " + background + " WHERE COALESCE(status,'') NOT IN ('completed','failed','cancelled')) THEN RAISE EXCEPTION 'canonical_post_drain_failed'; END IF;\n")
	}
	for _, name := range []string{"request_executions", "request_audits"} {
		table := projectionSource(sources, "gateway_", name)
		if table == "" {
			continue
		}
		condition := projectionDrainCondition{}
		var err error
		if name == "request_executions" {
			condition.predicate, err = executionDrainSQL(ctx, tx, sources, "projection")
		} else {
			condition, err = auditDrainSQL(ctx, tx, sources, "projection")
		}
		if err != nil {
			return err
		}
		prefix := ""
		if condition.ctes != "" {
			prefix = "WITH " + condition.ctes + " "
		}
		where := " FROM " + table + " projection WHERE NOT COALESCE((" + condition.predicate + "),false)"
		assertions.WriteString("IF EXISTS(" + prefix + "SELECT 1" + where + " LIMIT 1) THEN RAISE EXCEPTION 'canonical_post_drain_failed'; END IF;\n")
		if r.Report.Counts["in_flight:gateway_"+name] == 0 {
			continue
		}
		columns := "projection.request_id,left(md5(projection.request_id),12),encode(sha256(convert_to(to_jsonb(projection)::text,'UTF8')),'hex'),''::text,''::text,false"
		join := ""
		if background != "" {
			columns = "projection.request_id,left(md5(projection.request_id),12),encode(sha256(convert_to(to_jsonb(projection)::text,'UTF8')),'hex'),COALESCE(background.id,''),CASE WHEN background.id IS NULL THEN '' ELSE encode(sha256(convert_to(to_jsonb(background)::text,'UTF8')),'hex') END,COALESCE(projection.user_id=background.user_id AND projection.token_id=background.token_id,false)"
			join = " LEFT JOIN " + background + " background ON background.id=projection.request_id"
		}
		query := prefix + "SELECT " + columns + " FROM " + table + " projection" + join + " WHERE NOT COALESCE((" + condition.predicate + "),false) ORDER BY projection.request_id"
		rows, err := tx.Query(ctx, query)
		if err != nil {
			return err
		}
		var evidence []SourceDrainProjection
		for rows.Next() {
			var row SourceDrainProjection
			if err := rows.Scan(&row.RequestID, &row.RequestHash, &row.RowSHA256, &row.BackgroundID, &row.BackgroundRowSHA256, &row.OwnerKeyMatch); err != nil {
				rows.Close()
				return err
			}
			evidence = append(evidence, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if int64(len(evidence)) != r.Report.Counts["in_flight:gateway_"+name] {
			return fmt.Errorf("source drain evidence cardinality differs for %s", name)
		}
		if name == "request_executions" {
			r.UndrainedExecutions = evidence
		} else {
			r.UndrainedAudits = evidence
		}
	}
	body := "BEGIN\n" + assertions.String() + "END;"
	// Identifiers are pgx-quoted, and the dollar delimiter is absent from the
	// generated body even if an unusual source identifier contains '$'.
	delimiter := "$codego_drain_assert$"
	for strings.Contains(body, delimiter) {
		delimiter = strings.TrimSuffix(delimiter, "$") + "_$"
	}
	r.ProjectionAssertionSQL = "DO " + delimiter + "\n" + body + "\n" + delimiter + ";\n"
	r.ProjectionAssertionSHA256 = drainDigest([]byte(r.ProjectionAssertionSQL))
	return nil
}
