package legacy

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (m *Importer) VerifyOnline(ctx context.Context, opts OnlineOptions) (OnlineReport, error) {
	r := OnlineReport{RunID: opts.RunID, Tables: map[string]int64{}}
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
	target, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
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
	var complete bool
	if err = target.QueryRow(ctx, "SELECT NOT EXISTS(SELECT 1 FROM v3_migration_online.progress WHERE NOT complete)").Scan(&complete); err != nil {
		return r, err
	}
	if !complete {
		return r, errors.New("legacy: online baseline is incomplete")
	}
	if err = onlineRequireQuiescent(ctx, source); err != nil {
		return r, err
	}
	p, err := loadOnlineProjector(ctx, source, target, sources)
	if err != nil {
		return r, err
	}
	if err = onlineRefreshAccounts(ctx, target, p); err != nil {
		return r, err
	}
	staged := onlineStageTx{Tx: target, tables: onlineSortedTables(specs)}
	report := Report{Counts: map[string]int64{}, Amounts: map[string]string{}}
	history, err := loadHistory(ctx, source, sources)
	if err != nil {
		return r, err
	}
	if len(history.issues) > 0 {
		return r, errors.New("legacy: online source history validation failed")
	}
	// Login credentials and providers are small and remain final-offline work.
	history.passkeys = nil
	history.providers = nil
	history.bindings = nil
	for _, name := range []string{"passkeys_active", "passkeys_deleted", "custom_oauth_providers", "user_oauth_bindings"} {
		history.counts[name] = 0
	}
	if err = m.checkHistory(ctx, staged, history, &report); err != nil {
		return r, err
	}
	if len(report.Issues) > 0 {
		return r, errors.New("legacy: online historical projection differs from source")
	}
	funding := *p.funding
	funding.sources = map[string]string{}
	for name, table := range p.funding.sources {
		funding.sources[name] = table
	}
	for _, name := range []string{"funding_source_policies", "wallet_reward_holds", "request_economics"} {
		delete(funding.sources, name)
		delete(funding.sources, "billing_"+name)
	}
	funding.drains = nil
	funding.queues = nil
	if err = funding.validateContext(ctx, &report); err != nil {
		return r, err
	}
	if len(report.Issues) > 0 {
		return r, errors.New("legacy: online funding validation failed")
	}
	if err = m.checkFunding(ctx, staged, &funding, &report); err != nil {
		return r, err
	}
	if len(report.Issues) > 0 {
		return r, errors.New("legacy: online funding projection differs from source")
	}
	for _, spec := range specs {
		if !strings.HasPrefix(spec.name, "market.") {
			continue
		}
		name := strings.TrimPrefix(spec.name, "market.")
		var expected int64
		if err = p.market.streamBatches(ctx, name, func(records []cmRecord) error {
			values := make([]map[string]any, len(records))
			for i, record := range records {
				var err error
				values[i], err = cmCheckFields(record)
				if err != nil {
					return err
				}
			}
			matches, err := checkExactBulk(ctx, staged, "v3_channelmarket", name, records[0].keys, values)
			if err != nil {
				return err
			}
			if len(matches) != len(values) {
				return errors.New("legacy: online market check returned incorrect row count")
			}
			for _, match := range matches {
				if !match {
					return errors.New("legacy: online market projection differs from source")
				}
			}
			expected += int64(len(values))
			return nil
		}); err != nil {
			return r, err
		}
		var actual int64
		if err = target.QueryRow(ctx, "SELECT count(*) FROM "+onlineStage("v3_channelmarket."+name)).Scan(&actual); err != nil {
			return r, err
		}
		if actual != expected {
			return r, errors.New("legacy: online market has an unexpected row")
		}
		r.Tables[spec.name] = expected
	}
	if err = onlineCheckRetiredTotals(ctx, target, history, &report); err != nil {
		return r, err
	}
	// Recompute totals from actual staging, independently of the incremental
	// updater. This is expensive once, while V2 is still accepting traffic.
	if err = onlineRebuildTotals(ctx, target, specs); err != nil {
		return r, err
	}
	if _, err = target.Exec(ctx, "UPDATE v3_migration_online.run SET phase='verified',verified_at=clock_timestamp() WHERE singleton"); err != nil {
		return r, err
	}
	if err = target.Commit(ctx); err != nil {
		return r, err
	}
	r.Phase = "verified"
	r.Applied = true
	return r, nil
}

// Retired data is excluded from the native staging tables, so rebuilding their
// money totals cannot verify these receipts. Compare independently streamed
// source counts and original-unit sums before accepting the excluded evidence.
func onlineCheckRetiredTotals(ctx context.Context, target pgx.Tx, history *historyData, funding *Report) error {
	expected := map[string]*big.Int{}
	known := map[string]bool{}
	for _, suffix := range []string{"", ".amount_v2_units", ".balance_after_v2_units", ".amount_unparseable", ".balance_after_unparseable"} {
		known["retired.history.retired:ledger_entries"+suffix] = true
	}
	for name, fields := range map[string][]string{"funding_lots": {"original_amount", "remaining_amount"}, "funding_allocations": {"amount"}} {
		prefix := "retired_features.billing_" + name
		known[prefix] = true
		for _, field := range fields {
			known[prefix+"."+field+"_v2_units"] = true
			known[prefix+"."+field+"_unparseable"] = true
		}
	}
	for name, count := range history.counts {
		key := "retired.history." + name
		if known[key] {
			expected[key] = big.NewInt(count)
		}
	}
	for name, amount := range history.amounts {
		key := "retired.history." + name
		if known[key] {
			expected[key] = amount
		}
	}
	for name, count := range funding.Counts {
		if known[name] {
			expected[name] = big.NewInt(count)
		}
	}
	for name, amount := range funding.Amounts {
		if known[name] {
			value, ok := new(big.Int).SetString(amount, 10)
			if !ok {
				return fmt.Errorf("legacy: invalid source retired metric %s", name)
			}
			expected[name] = value
		}
	}
	rows, err := target.Query(ctx, "SELECT name,value::text FROM v3_migration_online.totals WHERE name LIKE 'retired.%' OR name LIKE 'retired_features.%'")
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return err
		}
		if !known[name] {
			return fmt.Errorf("legacy: unexpected retired receipt metric %s", name)
		}
		want := expected[name]
		if want == nil {
			want = new(big.Int)
		}
		actual, ok := new(big.Rat).SetString(value)
		if !ok || actual.Cmp(new(big.Rat).SetInt(want)) != 0 {
			return fmt.Errorf("legacy: retired receipt metric differs from source: %s", name)
		}
		seen[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for name, value := range expected {
		if value.Sign() != 0 && !seen[name] {
			return fmt.Errorf("legacy: missing retired receipt metric %s", name)
		}
	}
	// The incremental updater can retain a zero after the last excluded row is
	// deleted. Absent and zero are equivalent only for the fixed metric names.
	return nil
}

func onlineRebuildTotals(ctx context.Context, target pgx.Tx, specs []onlineSpec) error {
	if _, err := target.Exec(ctx, "DELETE FROM v3_migration_online.totals WHERE name NOT LIKE 'retired.%' AND name NOT LIKE 'retired_features.%'"); err != nil {
		return err
	}
	for _, table := range onlineSortedTables(specs) {
		if table == "v3_billing.historical_accounts" {
			continue
		}
		rows, err := target.Query(ctx, `SELECT attname FROM pg_attribute WHERE attrelid=$1::regclass AND attnum>0 AND NOT attisdropped AND attname=ANY($2::text[])`, onlineStage(table), []string{"amount", "original_amount", "remaining_amount", "consumed_amount", "actual_amount", "consumer_micro", "gross_micro", "commission_micro", "fee_micro", "net_micro", "reclaimed_micro"})
		if err != nil {
			return err
		}
		var fields []string
		for rows.Next() {
			var name string
			if err = rows.Scan(&name); err != nil {
				rows.Close()
				return err
			}
			fields = append(fields, name)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		// Count and all monetary fields share one aggregate scan. PostgreSQL
		// numeric sums preserve values beyond int64 without a Go/float roundtrip.
		aggregates := []string{"'rows'", "count(*)"}
		for _, field := range fields {
			aggregates = append(aggregates, "'"+field+"'", "COALESCE(sum("+pgx.Identifier{field}.Sanitize()+"),0)")
		}
		query := "INSERT INTO v3_migration_online.totals(name,value) SELECT $1||'.'||metric.key,metric.value::numeric FROM (SELECT jsonb_build_object(" + strings.Join(aggregates, ",") + ") AS amounts FROM " + onlineStage(table) + ") summary CROSS JOIN LATERAL jsonb_each_text(summary.amounts) metric"
		if _, err = target.Exec(ctx, query, table); err != nil {
			return err
		}
	}
	var logs bool
	if err := target.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", onlineStage("v3_audit.events")).Scan(&logs); err != nil {
		return err
	}
	if logs {
		if _, err := target.Exec(ctx, "INSERT INTO v3_migration_online.totals SELECT 'history.log_duplicates',COALESCE(sum(n),0) FROM(SELECT count(*) n FROM "+onlineStage("v3_audit.events")+" WHERE event_type=2 AND request_id<>'' GROUP BY created_at,user_id,request_id HAVING count(*)>1)s"); err != nil {
			return err
		}
	}
	var settlements bool
	if err := target.QueryRow(ctx, "SELECT to_regclass($1)IS NOT NULL", onlineStage("v3_channelmarket.settlements")).Scan(&settlements); err != nil {
		return err
	}
	if settlements {
		if _, err := target.Exec(ctx, "INSERT INTO v3_migration_online.totals SELECT 'market.pending.'||owner_user_id,sum(net_micro) FROM "+onlineStage("v3_channelmarket.settlements")+" WHERE status='pending' GROUP BY owner_user_id"); err != nil {
			return err
		}
	}
	return nil
}

func onlineLoadView(ctx context.Context, target pgx.Tx, runID string) (*onlineView, error) {
	v := &onlineView{runID: runID, counts: map[string]int64{}, amounts: map[string]string{}, marketCounts: map[string]int64{}, marketPending: map[int64]int64{}}
	var phase string
	if err := target.QueryRow(ctx, "SELECT phase FROM v3_migration_online.run WHERE singleton AND run_id=$1", runID).Scan(&phase); err != nil {
		return nil, err
	}
	if phase != "verified" {
		return nil, errors.New("legacy: finalization requires a verified online baseline")
	}
	rows, err := target.Query(ctx, "SELECT name,value::text FROM v3_migration_online.totals")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name, value string
		if err = rows.Scan(&name, &value); err != nil {
			return nil, err
		}
		v.amounts[name] = value
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	counts := map[string]string{"ledger_entries": "v3_billing.historical_entries.rows", "logs": "v3_audit.events.rows", "usage_logs": "v3_billing.usage_logs.rows", "request_audits": "v3_audit.request_audits.rows", "request_attempt_audits_linked": "v3_audit.request_attempt_audits.rows", "orphan_request_attempt_history": "v3_audit.orphan_request_attempt_history.rows", "usage_request_ids_disambiguated": "history.log_duplicates"}
	for name, key := range counts {
		var n int64
		if text := v.amounts[key]; text != "" {
			if _, err = fmt.Sscan(text, &n); err != nil {
				return nil, err
			}
		}
		v.counts[name] = n
	}
	v.counts["request_attempt_audits"] = v.counts["request_attempt_audits_linked"] + v.counts["orphan_request_attempt_history"]
	for name, text := range v.amounts {
		if strings.HasPrefix(name, "v3_channelmarket.") && strings.HasSuffix(name, ".rows") {
			var n int64
			if _, err = fmt.Sscan(text, &n); err != nil {
				return nil, err
			}
			v.marketCounts[strings.TrimSuffix(strings.TrimPrefix(name, "v3_channelmarket."), ".rows")] = n
		}
		if strings.HasPrefix(name, "market.pending.") {
			var owner, amount int64
			if _, err = fmt.Sscan(strings.TrimPrefix(name, "market.pending."), &owner); err != nil {
				return nil, err
			}
			if _, err = fmt.Sscan(text, &amount); err != nil {
				return nil, err
			}
			if amount < 0 {
				return nil, errors.New("legacy: negative staged pending income")
			}
			if amount > 0 {
				v.marketPending[owner] = amount
			}
		}
	}
	return v, nil
}
