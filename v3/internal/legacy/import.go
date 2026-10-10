package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

type Importer struct {
	source             *pgxpool.Pool
	pool               *pgxpool.Pool
	crypto             catalog.Encrypter
	sourceCryptoSecret string
	archiveLedger      bool
	historyCutoff      time.Time
}

func NewImporter(source, target *pgxpool.Pool, crypto catalog.Encrypter) *Importer {
	return &Importer{source: source, pool: target, crypto: crypto}
}

// WithSourceCryptoSecret supplies the v2 CryptoSecret only for enc:v1 values.
// It is intentionally independent of the target encryption key.
func (m *Importer) WithSourceCryptoSecret(secret string) *Importer {
	m.sourceCryptoSecret = secret
	return m
}

// Import runs a read-only preview by default. Apply requires all v2 writers to
// be stopped. The source remains READ ONLY throughout; only the independent
// target transaction writes and locks. All target rows commit together.
func (m *Importer) Import(ctx context.Context, apply bool) (Report, error) {
	ctx = m.historyContext(ctx)
	r := Report{Issues: []Issue{}, UnmappedSources: []string{}, Counts: map[string]int64{}, Amounts: map[string]string{}, OpeningMicroCredits: "0"}
	if m.source == nil || m.pool == nil || m.crypto == nil {
		return r, errors.New("legacy: source, target and encrypter are required")
	}
	view := onlineViewFrom(ctx)
	if view != nil {
		inherited, err := onlineInheritedSourceFence(ctx, m.source)
		if err != nil {
			return r, err
		}
		if !inherited {
			fence, err := onlineHoldSourceFence(ctx, m.source)
			if err != nil {
				return r, err
			}
			defer closeOnlineConnection(fence)
		}
	}
	tx, err := m.source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return r, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	policyTx, policyErr := m.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if policyErr != nil {
		return r, policyErr
	}
	policyErr = validateHistoryCutoff(ctx, policyTx)
	_ = policyTx.Rollback(ctx)
	if policyErr != nil {
		return r, policyErr
	}
	var staged bool
	if err = m.pool.QueryRow(ctx, "SELECT to_regclass('v3_migration_online.run') IS NOT NULL").Scan(&staged); err != nil {
		return r, err
	}
	if staged && view == nil && apply {
		var phase string
		if err = m.pool.QueryRow(ctx, "SELECT phase FROM v3_migration_online.run WHERE singleton").Scan(&phase); err != nil {
			return r, err
		}
		if phase != "finalized" {
			return r, errors.New("legacy: staged target requires online finalization; use an independent empty target for offline fallback")
		}
	}
	if view != nil {
		capture, captureErr := ValidateOnlineCapture(ctx, tx, view.runID)
		if captureErr != nil {
			return r, captureErr
		}
		if !capture.Sealed {
			return r, errors.New("legacy: online source fence was released")
		}
		if err = onlineRequireQuiescent(ctx, tx); err != nil {
			return r, err
		}
	}
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		return r, err
	}
	data, r, err := inspectSource(ctx, tx, sources)
	if err != nil {
		return r, err
	}
	m.validateRestoredSecrets(data, &r)
	if !apply {
		return r, tx.Commit(ctx)
	}
	if len(r.Issues) != 0 {
		return r, errors.New("legacy: import blocked by dry-run issues")
	}
	var targetRelations []pgx.Identifier
	if view != nil {
		// Discover outside the SERIALIZABLE transaction. Its first statement
		// must lock these tables, before any SELECT fixes a stale catalog view.
		targetRelations, err = onlineDiscoverTargetRelations(ctx, m.pool)
		if err != nil {
			return r, err
		}
	}
	target, err := m.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return r, err
	}
	defer func() { _ = target.Rollback(ctx) }()
	if view != nil {
		if err = onlineLockTargetRelations(ctx, target, targetRelations); err != nil {
			return r, err
		}
	}
	if _, err = target.Exec(ctx, `SELECT pg_advisory_xact_lock(738301031)`); err != nil {
		return r, err
	}
	if err = bindHistoryCutoff(ctx, target); err != nil {
		return r, err
	}
	if view != nil {
		if _, _, err = onlineBindings(ctx, tx, target, view.runID); err != nil {
			return r, err
		}
		if err = onlineAdopt(ctx, target, onlineSpecs(sources), view); err != nil {
			return r, err
		}
	}
	if err = m.importUsers(ctx, target, data.users); err != nil {
		return r, err
	}
	if err = m.importKeys(ctx, target, data.keys); err != nil {
		return r, err
	}
	if err = m.importChannels(ctx, target, data.channels); err != nil {
		return r, err
	}
	if err = m.importOptions(ctx, target, data.options, data.prices); err != nil {
		return r, err
	}
	if err = m.importCatalogData(ctx, target, data.catalog); err != nil {
		return r, err
	}
	if err = m.importCommerce(ctx, target, data.commerce); err != nil {
		return r, err
	}
	if err = m.importMarketplace(ctx, target, data.marketplace); err != nil {
		return r, err
	}
	if err = m.importChannelMarket(ctx, target, data.channelMarket); err != nil {
		return r, err
	}
	if err = m.importHistory(ctx, target, data.history); err != nil {
		return r, err
	}
	if err = importLedgerArchive(ctx, target, data.history.archive); err != nil {
		return r, err
	}
	if err = m.importFunding(ctx, target, data.funding); err != nil {
		return r, err
	}
	if err = m.importEntitlements(ctx, target, data.entitlements); err != nil {
		return r, err
	}
	if err = m.importOIDCData(ctx, target, data.oidc); err != nil {
		return r, err
	}
	if err = m.importSecurityData(ctx, target, data.security); err != nil {
		return r, err
	}
	if err = m.importRestoredState(ctx, target, data.restored); err != nil {
		return r, err
	}
	if err = m.importTaskHistory(ctx, target, data.tasks); err != nil {
		return r, err
	}
	if err = m.initializeCommerceRuntime(ctx, target, &r); err != nil {
		return r, err
	}
	if err = resetSequences(ctx, target); err != nil {
		return r, err
	}
	if view != nil {
		if err = onlineRestoreRelationships(ctx, target, onlineSpecs(sources)); err != nil {
			return r, err
		}
		if err = m.checkImportData(ctx, target, data, &r); err != nil {
			return r, err
		}
		if _, err = target.Exec(ctx, "UPDATE v3_migration_online.run SET phase='finalized' WHERE singleton AND run_id=$1", view.runID); err != nil {
			return r, err
		}
	}
	if err = target.Commit(ctx); err != nil {
		return r, err
	}
	r.Applied = true
	return r, nil
}

func discoverSources(ctx context.Context, tx pgx.Tx) (map[string]string, error) {
	tables := map[string]string{}
	rows, err := tx.Query(ctx, `SELECT n.nspname, c.relname FROM pg_class c
        JOIN pg_namespace n ON n.oid=c.relnamespace
        WHERE c.relkind IN ('r','p') AND NOT c.relispartition
        AND n.nspname NOT LIKE 'v3_%' AND n.nspname NOT LIKE 'pg_%'
        AND n.nspname <> 'information_schema' ORDER BY n.nspname,c.relname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var schema, name string
		if err = rows.Scan(&schema, &name); err != nil {
			return nil, err
		}
		table := pgx.Identifier{schema, name}.Sanitize()
		tables[schema+"_"+name] = table
		if schema == "marketplace" {
			continue
		}
		if (name == "accounts" || name == "balance_snapshots") && schema != "billing" {
			continue
		}
		if previous := tables[name]; previous != "" && previous != table {
			return nil, fmt.Errorf("legacy: ambiguous source table %s", name)
		}
		tables[name] = table
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if tables["users"] == "" {
		return nil, errors.New("legacy: v2 users table not found")
	}
	if (tables["accounts"] == "") != (tables["balance_snapshots"] == "") {
		return nil, errors.New("legacy: incomplete v2 billing schema")
	}
	return tables, nil
}
func loadRows(ctx context.Context, tx pgx.Tx, table string) ([]json.RawMessage, error) {
	if table == "" {
		return nil, nil
	}
	rows, err := tx.Query(ctx, "SELECT to_jsonb(t) FROM "+table+" t")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []json.RawMessage
	for rows.Next() {
		var row json.RawMessage
		if err = rows.Scan(&row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func resetSequences(ctx context.Context, tx pgx.Tx) error {
	for _, table := range []string{"v3_identity.users", "v3_identity.api_keys", "v3_catalog.channels", "v3_billing.usage_logs"} {
		_, err := tx.Exec(ctx, `SELECT setval(pg_get_serial_sequence($1,'id'),
			GREATEST(COALESCE((SELECT max(id) FROM `+table+`),0),1),
			EXISTS(SELECT 1 FROM `+table+`))`, table)
		if err != nil {
			return err
		}
	}
	return nil
}

func list(s string) []string {
	var out []string
	for _, value := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' }) {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
