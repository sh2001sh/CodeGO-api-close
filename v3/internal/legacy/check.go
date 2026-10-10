package legacy

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Check compares the frozen v2 source with all migrated native target records.
// Run before enabling target writers: subsequent activity legitimately changes
// current balances and business states. Both snapshots are strictly read-only.
func (m *Importer) Check(ctx context.Context) (Report, error) {
	ctx = m.historyContext(ctx)
	r := Report{Issues: []Issue{}, Counts: map[string]int64{}, Amounts: map[string]string{}}
	if m.source == nil || m.pool == nil || m.crypto == nil {
		return r, errors.New("legacy: source, target and encrypter are required")
	}
	source, err := m.source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return r, err
	}
	defer func() { _ = source.Rollback(ctx) }()
	policyTx, policyErr := m.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if policyErr != nil {
		return r, policyErr
	}
	policyErr = validateHistoryCutoff(ctx, policyTx)
	_ = policyTx.Rollback(ctx)
	if policyErr != nil {
		return r, policyErr
	}
	sources, err := discoverSources(ctx, source)
	if err != nil {
		return r, err
	}
	data, r, err := inspectSource(ctx, source, sources)
	if err != nil {
		return r, err
	}
	m.validateRestoredSecrets(data, &r)
	if len(r.Issues) > 0 {
		return r, errors.New("legacy: source validation failed")
	}
	target, err := m.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return r, err
	}
	defer func() { _ = target.Rollback(ctx) }()
	if err = m.checkImportData(ctx, target, data, &r); err != nil {
		return r, err
	}
	if err = target.Commit(ctx); err != nil {
		return r, err
	}
	return r, source.Commit(ctx)
}

// checkImportData reconciles the inspected source against the caller's target
// transaction. Finalization can validate its uncommitted openings atomically.
func (m *Importer) checkImportData(ctx context.Context, target pgx.Tx, data *importData, r *Report) error {
	checks := []func() error{
		func() error { return m.checkUsers(ctx, target, data.users, r) },
		func() error { return m.checkKeys(ctx, target, data.keys, data.channelMarket, r) },
		func() error { return m.checkChannels(ctx, target, data.channels, r) },
		func() error { return m.checkOptions(ctx, target, data.options, data.prices, r) },
		func() error { return m.checkCatalogData(ctx, target, data.catalog, r) },
		func() error { return m.checkCommerce(ctx, target, data.commerce, r) },
		func() error { return m.checkMarketplace(ctx, target, data.marketplace, r) },
		func() error { return m.checkCommerceRuntime(ctx, target, data.commerce, r) },
		func() error { return m.checkChannelMarket(ctx, target, data.channelMarket, r) },
		func() error { return m.checkHistory(ctx, target, data.history, r) },
		func() error { return checkLedgerArchive(ctx, target, data.history.archive, r) },
		func() error { return m.checkFunding(ctx, target, data.funding, r) },
		func() error { return m.checkEntitlements(ctx, target, data.entitlements, r) },
		func() error { return m.checkOIDCData(ctx, target, data.oidc, r) },
		func() error { return m.checkSecurityData(ctx, target, data.security, r) },
		func() error { return m.checkRestoredState(ctx, target, data.restored, r) },
		func() error { return m.checkTaskHistory(ctx, target, data.tasks, r) },
	}
	for _, check := range checks {
		if err := check(); err != nil {
			return err
		}
	}
	var differences int64
	if err := target.QueryRow(ctx, `SELECT count(*) FROM v3_billing.accounts a
		LEFT JOIN (SELECT account_id,sum(amount) total FROM v3_billing.ledger_entries GROUP BY account_id)e ON e.account_id=a.id
		WHERE a.balance<>COALESCE(e.total,0)`).Scan(&differences); err != nil {
		return err
	}
	r.Counts["ledger_balance_differences"] = differences
	if differences > 0 {
		checkIssue(r, "ledger", 0, "live account balances differ from ledger sums")
	}
	if len(r.Issues) > 0 {
		return errors.New("legacy: source-target reconciliation failed")
	}
	return nil
}

func checkAccount(ctx context.Context, target pgx.Tx, owner string, id int64, kind string, amount int64, report *Report) error {
	match, err := checkProjection(ctx, target, "v3_billing.accounts", map[string]any{"owner_type": owner, "owner_id": id, "kind": kind, "balance": amount})
	if err != nil {
		return err
	}
	if !match {
		checkIssue(report, owner, id, "account opening or balance differs from source")
	}
	return nil
}
