package ledger

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

var ErrHistoryArchive = errors.New("ledger: historical archive unavailable or invalid")

type historyArchiveIdentity struct {
	cluster  string
	database string
	oid      int64
}

func loadHistoryArchive(ctx context.Context, pool *pgxpool.Pool) (*historyArchiveIdentity, error) {
	var identity historyArchiveIdentity
	err := pool.QueryRow(ctx, `SELECT source_cluster_id,source_database_oid,source_database
	 FROM v3_billing.ledger_history_archive WHERE singleton`).Scan(&identity.cluster, &identity.oid, &identity.database)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: read archive metadata: %w", ErrHistoryArchive, err)
	}
	if identity.cluster == "" || identity.database == "" || identity.oid <= 0 {
		return nil, fmt.Errorf("%w: incomplete source identity", ErrHistoryArchive)
	}
	return &identity, nil
}

// ValidateHistoryArchive makes an archive-mode deployment fail startup when its
// source is missing, writable, or different from the frozen migration source.
// Full-copy deployments require no archive connection.
func ValidateHistoryArchive(ctx context.Context, pool, archive *pgxpool.Pool) error {
	identity, err := loadHistoryArchive(ctx, pool)
	if err != nil || identity == nil {
		return err
	}
	tx, err := beginHistoryArchive(ctx, archive, *identity)
	if err != nil {
		return err
	}
	return tx.Rollback(ctx)
}

func beginHistoryArchive(ctx context.Context, pool *pgxpool.Pool, expected historyArchiveIdentity) (pgx.Tx, error) {
	if pool == nil {
		return nil, fmt.Errorf("%w: archive DSN is required by migration metadata", ErrHistoryArchive)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("%w: connect to archive: %w", ErrHistoryArchive, err)
	}
	fail := func(err error) (pgx.Tx, error) {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	var actual historyArchiveIdentity
	var elevated, readable, writable bool
	err = tx.QueryRow(ctx, `SELECT c.system_identifier::text,d.oid::bigint,current_database(),
	 r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb,
	 NOT ledger.relrowsecurity AND has_schema_privilege(current_user,'billing','USAGE')
	 AND has_column_privilege(current_user,'billing.ledger_entries','entry_id','SELECT')
	 AND has_column_privilege(current_user,'billing.ledger_entries','account_id','SELECT')
	 AND has_column_privilege(current_user,'billing.ledger_entries','amount','SELECT')
	 AND has_column_privilege(current_user,'billing.ledger_entries','balance_after','SELECT')
	 AND has_column_privilege(current_user,'billing.ledger_entries','entry_type','SELECT')
	 AND has_column_privilege(current_user,'billing.ledger_entries','direction','SELECT')
	 AND has_column_privilege(current_user,'billing.ledger_entries','reason_code','SELECT')
	 AND has_column_privilege(current_user,'billing.ledger_entries','created_at','SELECT'),
	 has_table_privilege(current_user,'billing.ledger_entries','INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')
	 OR has_any_column_privilege(current_user,'billing.ledger_entries','INSERT,UPDATE,REFERENCES')
	 FROM pg_catalog.pg_control_system() c
	 JOIN pg_catalog.pg_database d ON d.datname=current_database()
	 JOIN pg_catalog.pg_roles r ON r.rolname=current_user
	 JOIN pg_catalog.pg_class ledger ON ledger.oid='billing.ledger_entries'::regclass`).Scan(&actual.cluster, &actual.oid, &actual.database, &elevated, &readable, &writable)
	if err != nil {
		return fail(fmt.Errorf("%w: inspect archive identity and permissions: %w", ErrHistoryArchive, err))
	}
	if actual != expected {
		return fail(fmt.Errorf("%w: source cluster or database identity mismatch", ErrHistoryArchive))
	}
	if elevated || writable || !readable {
		return fail(fmt.Errorf("%w: archive requires a dedicated nonprivileged SELECT-only role", ErrHistoryArchive))
	}
	return tx, nil
}

// ReadHistoryWithArchive retains the native reader when no archive migration
// metadata exists. Archive account visibility is always resolved in V3 first;
// the source cannot grant access through a stale V2 owner or caller-supplied ID.
func ReadHistoryWithArchive(ctx context.Context, pool, archive *pgxpool.Pool, userID int64, source, before string, limit int) (HistoryPage, error) {
	page := HistoryPage{Items: []HistoricalEntry{}}
	cursor, err := parseHistoryQuery(userID, source, before, limit)
	if err != nil {
		return page, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	identity, err := loadHistoryArchive(ctx, pool)
	if err != nil {
		return page, err
	}
	if identity == nil {
		return ReadHistory(ctx, pool, userID, source, before, limit)
	}
	rows, err := pool.Query(ctx, `SELECT h.source_account_id FROM v3_billing.historical_accounts h
	 LEFT JOIN v3_billing.accounts a ON a.id=h.account_id
	 WHERE ((h.owner_type='user' AND h.owner_id=$1) OR (a.owner_type='user' AND a.owner_id=$1)
	 OR (a.owner_type='api_key' AND EXISTS(SELECT 1 FROM v3_identity.api_keys k WHERE k.id=a.owner_id AND k.user_id=$1))
	 OR (a.owner_type='subscription' AND EXISTS(SELECT 1 FROM v3_commerce.subscriptions s WHERE s.account_id=a.id AND s.user_id=$1)))
	 AND ($2='' OR h.source_account_id=$2)`, userID, source)
	if err != nil {
		return page, err
	}
	accounts := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return page, err
		}
		accounts = append(accounts, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return page, err
	}
	// Validate even an empty result: configuration/source failures must never
	// masquerade as a successful empty historical ledger.
	tx, err := beginHistoryArchive(ctx, archive, *identity)
	if err != nil {
		return page, err
	}
	defer tx.Rollback(ctx)
	if len(accounts) == 0 {
		return page, nil
	}
	// A bounded index scan per authorized account avoids sorting the entire
	// source ledger before applying a global limit across multiple accounts.
	rows, err = tx.Query(ctx, `SELECT e.entry_id,e.account_id,e.amount,e.balance_after,e.entry_type,e.direction,e.reason_code,e.created_at
	 FROM unnest($1::text[]) account(source_id)
	 CROSS JOIN LATERAL (SELECT entry_id,account_id,amount,balance_after,entry_type,direction,reason_code,created_at
	 FROM billing.ledger_entries WHERE account_id=account.source_id
	 AND ($2='' OR (created_at,entry_id)<($3,$2))
	 ORDER BY created_at DESC,entry_id DESC LIMIT $4) e
	 ORDER BY e.created_at DESC,e.entry_id DESC LIMIT $4`, accounts, cursor.ID, cursor.Time, limit+1)
	if err != nil {
		return page, fmt.Errorf("%w: query source ledger: %w", ErrHistoryArchive, err)
	}
	defer rows.Close()
	for rows.Next() {
		var entry HistoricalEntry
		var amount int64
		var balance *int64
		if err = rows.Scan(&entry.ID, &entry.SourceAccount, &amount, &balance, &entry.Kind, &entry.Direction, &entry.Reason, &entry.CreatedAt); err != nil {
			return HistoryPage{Items: []HistoricalEntry{}}, fmt.Errorf("%w: decode source ledger: %w", ErrHistoryArchive, err)
		}
		if err = convertArchiveEntry(&entry, amount, balance); err != nil {
			return HistoryPage{Items: []HistoricalEntry{}}, err
		}
		entry.CreatedAt = entry.CreatedAt.UTC()
		if entry.CreatedAt.IsZero() {
			return HistoryPage{Items: []HistoricalEntry{}}, fmt.Errorf("%w: source zero timestamp cannot provide a valid history cursor", ErrHistoryArchive)
		}
		page.Items = append(page.Items, entry)
	}
	if err = rows.Err(); err != nil {
		return HistoryPage{Items: []HistoricalEntry{}}, fmt.Errorf("%w: read source ledger: %w", ErrHistoryArchive, err)
	}
	return paginateHistory(page, limit)
}

func archiveMicro(units int64) (credits.Micro, error) {
	// V2 uses 500,000 quota units per credit; V3 uses 1,000,000 micro-credits.
	if units > math.MaxInt64/2 || units < math.MinInt64/2 {
		return 0, fmt.Errorf("%w: %w", ErrHistoryArchive, credits.ErrOverflow)
	}
	return credits.Micro(units * 2), nil
}

func convertArchiveEntry(entry *HistoricalEntry, amount int64, balance *int64) error {
	if amount < 0 {
		return fmt.Errorf("%w: source amount must be nonnegative", ErrHistoryArchive)
	}
	converted, err := archiveMicro(amount)
	if err != nil {
		return err
	}
	switch entry.Direction {
	case "debit":
		entry.Amount = -converted
	case "credit":
		entry.Amount = converted
	default:
		return fmt.Errorf("%w: source direction must be debit or credit", ErrHistoryArchive)
	}
	if balance != nil {
		convertedBalance, err := archiveMicro(*balance)
		if err != nil {
			return err
		}
		entry.BalanceAfter = &convertedBalance
	}
	return nil
}
