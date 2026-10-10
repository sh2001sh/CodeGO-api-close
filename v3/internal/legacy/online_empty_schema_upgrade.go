package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/migrations"
)

type OnlineEmptySchemaUpgradeOptions struct {
	OnlineOptions
	ExpectedTargetShape, ExpectedCaptureHash, ExpectedMigrationSHA256 string
}

type OnlineSchemaUpgradeReport struct {
	RunID               string `json:"run_id"`
	Phase               string `json:"phase"`
	Applied             bool   `json:"applied"`
	Migration           string `json:"migration"`
	MigrationSHA256     string `json:"migration_sha256"`
	PreviousTargetShape string `json:"previous_target_shape"`
	TargetShape         string `json:"target_shape"`
	CaptureHash         string `json:"capture_hash"`
	LedgerHistoryMode   string `json:"ledger_history_mode"`
	HistoryCutoff       string `json:"history_cutoff,omitempty"`
	SourceWrites        int    `json:"source_writes"`
}

var onlineReviewedSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

func ValidateOnlineEmptySchemaUpgradeOptions(opts OnlineEmptySchemaUpgradeOptions) error {
	for _, value := range []string{opts.ExpectedTargetShape, opts.ExpectedCaptureHash, opts.ExpectedMigrationSHA256} {
		if !onlineReviewedSHA.MatchString(value) {
			return errors.New("legacy: empty schema upgrade requires reviewed target, capture and migration hashes")
		}
	}
	sql, err := migrations.Read(migrations.ExactPriceMigration)
	if err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(sql))
	if hex.EncodeToString(hash[:]) != opts.ExpectedMigrationSHA256 {
		return errors.New("legacy: empty schema upgrade migration approval differs from embedded bytes")
	}
	return nil
}

// This is a single reviewed exception for a never-used target. It does not
// bless schema drift, renew source bindings, reset progress or acknowledge an
// event. Both the exact DDL and new catalog fingerprint commit atomically.
func (m *Importer) UpgradeEmptyOnlineSchema(ctx context.Context, opts OnlineEmptySchemaUpgradeOptions) (OnlineSchemaUpgradeReport, error) {
	ctx = m.historyContext(ctx)
	r := OnlineSchemaUpgradeReport{RunID: opts.RunID, Migration: migrations.ExactPriceMigration, MigrationSHA256: opts.ExpectedMigrationSHA256, LedgerHistoryMode: ledgerHistoryMode(ctx), HistoryCutoff: historyCutoffLabel(ctx)}
	if err := ValidateOnlineEmptySchemaUpgradeOptions(opts); err != nil {
		return r, err
	}
	conn, err := m.onlineConnection(ctx)
	if err != nil {
		return r, err
	}
	defer closeOnlineConnection(conn)
	target, err := conn.Begin(ctx)
	if err != nil {
		return r, err
	}
	defer func() { _ = target.Rollback(ctx) }()
	var locked bool
	if err = target.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, migrations.EmbeddedSchemaLock).Scan(&locked); err != nil {
		return r, err
	}
	if !locked {
		return r, errors.New("legacy: embedded schema migration is already running")
	}
	if err = onlineLockTargetSchema(ctx, target); err != nil {
		return r, err
	}
	if err = onlineAuthorize(ctx, target, opts.RunID); err != nil {
		return r, err
	}
	source, err := m.source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return r, err
	}
	defer func() { _ = source.Rollback(ctx) }()
	// Seal/setup use this same source lock. Normal captured source writes do
	// not, so they continue while the schema-only target transaction runs.
	if err = source.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, onlineLock).Scan(&locked); err != nil {
		return r, err
	}
	if !locked {
		return r, errors.New("legacy: source capture setup or seal is already running")
	}
	_, specs, err := onlineBindings(ctx, source, target, opts.RunID)
	if err != nil {
		return r, err
	}
	var untouched bool
	if err = target.QueryRow(ctx, `SELECT phase,target_shape,capture_hash,
	 dependencies='{}'::jsonb AND verified_at IS NULL FROM v3_migration_online.run WHERE singleton`).Scan(&r.Phase, &r.PreviousTargetShape, &r.CaptureHash, &untouched); err != nil {
		return r, err
	}
	if r.Phase != "copying" || !untouched || r.PreviousTargetShape != opts.ExpectedTargetShape || r.CaptureHash != opts.ExpectedCaptureHash {
		return r, errors.New("legacy: empty schema upgrade ownership, phase or reviewed bindings changed")
	}
	var unsealed, noAcknowledgments bool
	if err = source.QueryRow(ctx, `SELECT NOT sealed,NOT EXISTS(SELECT 1 FROM v3_migration_capture.events WHERE acked)
	 FROM v3_migration_capture.config WHERE singleton AND run_id=$1`, opts.RunID).Scan(&unsealed, &noAcknowledgments); err != nil {
		return r, err
	}
	if !unsealed || !noAcknowledgments {
		return r, errors.New("legacy: empty schema upgrade requires unsealed capture without acknowledgments")
	}
	var names []string
	for _, spec := range specs {
		names = append(names, spec.name)
	}
	if err = target.QueryRow(ctx, `SELECT count(*)=$1 AND coalesce(bool_and(name=ANY($2::text[]) AND cursor IS NULL AND copied=0 AND NOT complete),false)
	 FROM v3_migration_online.progress`, len(names), names).Scan(&untouched); err != nil {
		return r, err
	}
	if !untouched {
		return r, errors.New("legacy: empty schema upgrade requires all original unstarted progress rows")
	}
	if err = onlineRequireEmptySchemaUpgradeTarget(ctx, target); err != nil {
		return r, err
	}
	var tables []string
	for _, spec := range specs {
		tables = append(tables, spec.targets...)
	}
	tables = append(tables, "v3_billing.historical_accounts")
	if err = onlineValidateEmptySchemaUpgradeMetadata(ctx, target, tables); err != nil {
		return r, err
	}
	already, err := onlineExactPriceRevisionGate(ctx, target)
	if err != nil {
		return r, err
	}
	r.TargetShape = r.PreviousTargetShape
	if already {
		return r, nil
	}
	// Apply only the hash-approved historic revision. Later embedded migrations
	// require their own reviewed upgrade or an independent migration target.
	sql, err := migrations.Read(migrations.ExactPriceMigration)
	if err != nil {
		return r, err
	}
	if _, err = target.Exec(ctx, sql); err != nil {
		return r, err
	}
	if _, err = target.Exec(ctx, `INSERT INTO v3_platform.embedded_schema_revisions(name,checksum) VALUES($1,$2)`, migrations.ExactPriceMigration, opts.ExpectedMigrationSHA256); err != nil {
		return r, err
	}
	// ALTER TYPE and replaced checks acquire new object OIDs. Refresh only after
	// proving the original adoption mapping, inside the same DDL transaction.
	if err = onlineRememberObjectNames(ctx, target, tables); err != nil {
		return r, err
	}
	r.TargetShape, err = onlineTargetSchemaHash(ctx, target)
	if err != nil {
		return r, err
	}
	if r.TargetShape == r.PreviousTargetShape {
		return r, errors.New("legacy: empty schema upgrade did not change the actual schema")
	}
	if _, err = target.Exec(ctx, `UPDATE v3_migration_online.run SET target_shape=$1 WHERE singleton`, r.TargetShape); err != nil {
		return r, err
	}
	if err = target.Commit(ctx); err != nil {
		return r, err
	}
	r.Applied = true
	return r, nil
}

func onlineValidateEmptySchemaUpgradeMetadata(ctx context.Context, target pgx.Tx, tables []string) error {
	objects, err := onlineReadObjectNames(ctx, target, tables)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(objects)
	if err != nil {
		return err
	}
	var valid bool
	if err = target.QueryRow(ctx, `WITH actual AS (
	 SELECT t.name AS table_name,c.conname::text AS constraint_name,pg_get_constraintdef(c.oid) AS definition
	 FROM unnest($1::text[]) t(name) JOIN pg_constraint c ON c.conrelid=t.name::regclass AND c.contype='f'
	), changed AS (
	 (SELECT * FROM actual EXCEPT SELECT * FROM v3_migration_online.foreign_keys)
	 UNION ALL (SELECT * FROM v3_migration_online.foreign_keys EXCEPT SELECT * FROM actual)
	)
	SELECT object_names=$2::jsonb AND NOT EXISTS(SELECT 1 FROM changed)
	FROM v3_migration_online.run WHERE singleton`, tables, raw).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return errors.New("legacy: empty schema upgrade adoption metadata differs from original definitions")
	}
	return nil
}

func onlineExactPriceRevisionGate(ctx context.Context, target pgx.Tx) (bool, error) {
	names, err := migrations.Files()
	if err != nil {
		return false, err
	}
	approved := 0
	for i, name := range names {
		if name == migrations.ExactPriceMigration {
			approved = i + 1
			break
		}
	}
	if approved == 0 {
		return false, errors.New("legacy: empty schema upgrade only supports the reviewed exact price revision")
	}
	names = names[:approved]
	rows, err := target.Query(ctx, `SELECT name,checksum FROM v3_platform.embedded_schema_revisions`)
	if err != nil {
		return false, err
	}
	installed := map[string]string{}
	for rows.Next() {
		var name, checksum string
		if err = rows.Scan(&name, &checksum); err != nil {
			rows.Close()
			return false, err
		}
		installed[name] = checksum
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return false, err
	}
	_, already := installed[migrations.ExactPriceMigration]
	expected := len(names) - 1
	if already {
		expected++
	}
	if len(installed) != expected {
		return false, errors.New("legacy: empty schema upgrade requires the complete unchanged embedded predecessor")
	}
	for _, name := range names {
		if name == migrations.ExactPriceMigration && !already {
			continue
		}
		sql, err := migrations.Read(name)
		if err != nil {
			return false, err
		}
		hash := sha256.Sum256([]byte(sql))
		if installed[name] != hex.EncodeToString(hash[:]) {
			return false, fmt.Errorf("legacy: embedded predecessor migration %s differs from approved bytes", name)
		}
	}
	return already, nil
}

func onlineRequireEmptySchemaUpgradeTarget(ctx context.Context, target pgx.Tx) error {
	names, err := onlineDiscoverTargetRelations(ctx, target)
	if err != nil {
		return err
	}
	// Only immutable schema seeds and reviewed migration metadata may exist.
	// All staging, reservations, balances, histories and customer rows are empty.
	seedOrMetadata := map[string]bool{
		"v3_platform.embedded_schema_revisions": true, "v3_platform.settings": true,
		"v3_platform.cache_invalidation_outbox": true,
		"v3_billing.funding_source_policies":    true, "v3_commerce.referral_consumption_policy": true,
		"v3_migration_online.run": true, "v3_migration_online.progress": true, "v3_migration_online.foreign_keys": true,
	}
	for _, name := range names {
		if seedOrMetadata[name[0]+"."+name[1]] {
			continue
		}
		var populated bool
		if err = target.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+name.Sanitize()+")").Scan(&populated); err != nil {
			return err
		}
		if populated {
			return fmt.Errorf("legacy: empty schema upgrade found populated target relation %s", name.Sanitize())
		}
	}
	var seeds bool
	if err = target.QueryRow(ctx, `SELECT
	 (SELECT count(*)=1 AND bool_and(key='InvoiceSellerAddress' AND value='"UNIT 1618A, 16/F, PIONEER CENTRE, 750 NATHAN ROAD, MONG KOK, HONG KONG"'::jsonb) FROM v3_platform.settings)
	 AND (SELECT count(*)<=1 AND coalesce(bool_and(id=1 AND entity='settings' AND entity_id='InvoiceSellerAddress'
	 AND created_at=(SELECT updated_at FROM v3_platform.settings WHERE key='InvoiceSellerAddress')
	 AND leased_until IS NULL AND attempts=0),true) FROM v3_platform.cache_invalidation_outbox)
	 AND (SELECT count(*)=4 AND bool_and(source IN('referral_reward','subscription_conversion','blind_box_batch_base','blind_box_batch_reward') AND revenue_multiplier_ppm=0) FROM v3_billing.funding_source_policies)
	 AND (SELECT count(*)=1 AND bool_and(id AND NOT enabled AND revision=1 AND effective_at IS NOT NULL AND updated_at=effective_at
	 AND ancillary_cost_ppm IS NULL AND reward_ppm=10000 AND profit_share_ppm=200000 AND window_days=30 AND delay_days=7
	 AND max_reward_credits=0 AND total_budget_credits=0 AND reserved_credits=0 AND spent_credits=0) FROM v3_commerce.referral_consumption_policy)`).Scan(&seeds); err != nil {
		return err
	}
	if !seeds {
		return errors.New("legacy: empty schema upgrade target seeds differ from the unused schema")
	}
	return nil
}
