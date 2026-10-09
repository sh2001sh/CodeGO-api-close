package legacy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The archive is evidence, never a source of new balances or ledger postings.
// Credentials do not enter either the migration report or target metadata.
type LedgerHistoryArchive struct {
	SourceClusterID   string    `json:"source_cluster_id"`
	SourceDatabaseOID int64     `json:"source_database_oid"`
	SourceDatabase    string    `json:"source_database"`
	EntryCount        int64     `json:"entry_count"`
	ArchivedAt        time.Time `json:"archived_at"`
}

type ledgerArchiveContextKey struct{}

func (m *Importer) WithLedgerHistoryArchive(enabled bool) *Importer {
	m.archiveLedger = enabled
	return m
}

func (m *Importer) historyContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, ledgerArchiveContextKey{}, m.archiveLedger)
}

func ledgerHistoryArchived(ctx context.Context) bool {
	archived, _ := ctx.Value(ledgerArchiveContextKey{}).(bool)
	return archived
}

func ledgerHistoryMode(ctx context.Context) string {
	if ledgerHistoryArchived(ctx) {
		return "archive"
	}
	return "copy"
}

func onlineValidateLedgerHistoryMode(ctx context.Context, target pgx.Tx) error {
	var stored string
	if err := target.QueryRow(ctx, "SELECT ledger_history_mode FROM v3_migration_online.run WHERE singleton").Scan(&stored); err != nil {
		return err
	}
	if stored != ledgerHistoryMode(ctx) {
		return errors.New("legacy: ledger history mode changed during online migration; start an independent run")
	}
	return nil
}

func inspectLedgerArchive(ctx context.Context, source pgx.Tx, table string) (*LedgerHistoryArchive, error) {
	if table == "" {
		return nil, errors.New("legacy: ledger archive requires the original ledger table")
	}
	a := &LedgerHistoryArchive{}
	if err := source.QueryRow(ctx, `SELECT s.system_identifier::text,d.oid::bigint,d.datname,
	 transaction_timestamp() FROM pg_control_system() s JOIN pg_database d ON d.datname=current_database()`).Scan(
		&a.SourceClusterID, &a.SourceDatabaseOID, &a.SourceDatabase, &a.ArchivedAt); err != nil {
		return nil, fmt.Errorf("legacy: cannot establish ledger archive identity: %w", err)
	}
	// Count preserved source evidence without decoding, converting or copying
	// every historical row. Retired rows also remain intact in this archive.
	if err := source.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&a.EntryCount); err != nil {
		return nil, err
	}
	return a, nil
}

func checkLedgerArchive(ctx context.Context, target pgx.Tx, expected *LedgerHistoryArchive, report *Report) error {
	var stored LedgerHistoryArchive
	err := target.QueryRow(ctx, `SELECT source_cluster_id,source_database_oid,source_database,entry_count,archived_at
	 FROM v3_billing.ledger_history_archive WHERE singleton`).Scan(&stored.SourceClusterID,
		&stored.SourceDatabaseOID, &stored.SourceDatabase, &stored.EntryCount, &stored.ArchivedAt)
	if expected == nil {
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		return errors.New("legacy: target uses ledger archive mode; copy mode cannot check or overwrite it")
	}
	if err != nil {
		return fmt.Errorf("legacy: required ledger archive metadata is missing: %w", err)
	}
	if stored.SourceClusterID != expected.SourceClusterID || stored.SourceDatabaseOID != expected.SourceDatabaseOID ||
		stored.SourceDatabase != expected.SourceDatabase || stored.EntryCount != expected.EntryCount || stored.ArchivedAt.IsZero() {
		return errors.New("legacy: ledger archive identity or preserved source count differs")
	}
	var copied bool
	if err = target.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM v3_billing.historical_entries)").Scan(&copied); err != nil {
		return err
	}
	if copied {
		return errors.New("legacy: archived ledger must not coexist with a copied historical ledger")
	}
	report.LedgerHistoryArchive = &stored
	return nil
}

func importLedgerArchive(ctx context.Context, target pgx.Tx, expected *LedgerHistoryArchive) error {
	if expected == nil {
		return checkLedgerArchive(ctx, target, nil, &Report{})
	}
	_, err := target.Exec(ctx, `INSERT INTO v3_billing.ledger_history_archive
	 (source_cluster_id,source_database_oid,source_database,entry_count,archived_at)
	 VALUES($1,$2,$3,$4,$5) ON CONFLICT(singleton) DO NOTHING`, expected.SourceClusterID,
		expected.SourceDatabaseOID, expected.SourceDatabase, expected.EntryCount, expected.ArchivedAt)
	if err != nil {
		return err
	}
	return checkLedgerArchive(ctx, target, expected, &Report{})
}
