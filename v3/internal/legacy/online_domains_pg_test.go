//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestOnlineDomainAdaptersStageExactProvenanceWithoutMoney(t *testing.T) {
	source, target, _ := importTestDB(t)
	seedFundingFixture(t, source)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `INSERT INTO migration_source.users(id,username,role,status,"group",quota,claude_quota,setting) VALUES(8,'consumer',1,1,'default',0,0,'{}')`); err != nil {
		t.Fatal(err)
	}
	marketSources := seedChannelMarketFixture(t, source)
	snapshot, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snapshot.Rollback(ctx) }()
	sources, err := discoverSources(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for name, table := range marketSources {
		sources[name] = table
	}
	funding, err := loadFunding(ctx, snapshot, sources)
	if err != nil {
		t.Fatal(err)
	}
	market, err := loadChannelMarketBase(ctx, snapshot, sources, false)
	if err != nil || len(market.issues) != 0 || len(market.streamCounts) != 0 || len(market.pending) != 0 || market.platformRevenue != nil {
		t.Fatalf("online market must not inspect live historical/account totals: %+v err=%v", market, err)
	}
	p := &onlineProjector{funding: funding, market: market, mappings: map[fundingAccountKey]int64{{owner: "user", kind: "wallet", id: 7}: 901}}
	tx, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `CREATE SCHEMA v3_online_domain_test`); err != nil {
		t.Fatal(err)
	}
	stage := func(projections []onlineProjection) {
		t.Helper()
		for _, projection := range projections {
			_, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+pgx.Identifier{"v3_online_domain_test", projection.Table}.Sanitize()+` (LIKE `+pgx.Identifier{projection.Schema, projection.Table}.Sanitize()+` INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES)`)
			if err != nil {
				t.Fatal(err)
			}
			if err := insertExactBulk(ctx, tx, "v3_online_domain_test", projection.Table, projection.Keys, []map[string]any{projection.Values}); err != nil {
				t.Fatalf("stage %s.%s: %v", projection.Schema, projection.Table, err)
			}
		}
	}
	counts := map[string]int{}
	for _, name := range fundingSourceNames {
		err := walkHistory(ctx, snapshot, fundingSource(sources, name), func(raw json.RawMessage) error {
			projections, err := p.projectOnlineFunding(name, raw)
			if err != nil {
				return err
			}
			stage(projections)
			counts[name] += len(projections)
			return nil
		})
		if err != nil || counts[name] != 1 {
			t.Fatalf("funding %s count=%d err=%v", name, counts[name], err)
		}
	}
	for _, name := range channelMarketSourceTables {
		if !cmStreamedTable(name) {
			continue
		}
		err := walkHistory(ctx, snapshot, sources["marketplace_"+name], func(raw json.RawMessage) error {
			projections, err := p.projectOnlineMarket(name, raw)
			if err != nil {
				return err
			}
			stage(projections)
			counts[name] += len(projections)
			return nil
		})
		if err != nil || counts[name] != 1 {
			t.Fatalf("market %s count=%d err=%v", name, counts[name], err)
		}
	}
	var balance, original, remaining, account, gross, net, rate int64
	if err := tx.QueryRow(ctx, `SELECT original_amount,remaining_amount,account_id,revenue_multiplier_ppm FROM v3_online_domain_test.funding_lots WHERE lot_id='funding-box-lot'`).Scan(&original, &remaining, &account, &rate); err != nil || original != 200 || remaining != 120 || account != 901 || rate != 650000 {
		t.Fatalf("funding exact conversion original=%d remaining=%d account=%d rate=%d err=%v", original, remaining, account, rate, err)
	}
	if err := tx.QueryRow(ctx, `SELECT gross_micro,net_micro FROM v3_online_domain_test.settlements WHERE id='settlement-201'`).Scan(&gross, &net); err != nil || gross != 200 || net != 190 {
		t.Fatalf("settlement exact conversion gross=%d net=%d err=%v", gross, net, err)
	}
	if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_billing.accounts)+(SELECT count(*) FROM v3_billing.ledger_entries)+(SELECT count(*) FROM v3_identity.users)`).Scan(&balance); err != nil || balance != 0 {
		t.Fatalf("staging wrote financial/identity state count=%d err=%v", balance, err)
	}
	// Values beyond float64's exact integer range survive a genuine PG JSON row.
	var raw []byte
	if err := snapshot.QueryRow(ctx, `SELECT to_jsonb(t) FROM (SELECT 'large'::text lot_id,'wallet-7'::text account_id,'other'::text source,'large-credit'::text idempotency_key,4611686018427387903::bigint original_amount,0::bigint remaining_amount,0.0000005::numeric revenue_multiplier,'2026-09-29T09:00:00Z'::timestamptz created_at) t`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	projections, err := p.projectOnlineFunding("funding_lots", raw)
	if err != nil || projections[0].Values["original_amount"] != int64(9223372036854775806) || projections[0].Values["revenue_multiplier_ppm"] != int64(1) {
		t.Fatalf("large exact conversion changed: %+v %v", projections, err)
	}
	stage(projections)
}

type onlineDomainNoScanTx struct {
	pgx.Tx
	tables []string
}

func (tx onlineDomainNoScanTx) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	if strings.Contains(query, "to_jsonb") {
		for _, table := range tx.tables {
			if strings.Contains(query, table) {
				return nil, errors.New("test: verified historical table was scanned again")
			}
		}
	}
	return tx.Tx.Query(ctx, query, args...)
}

func TestOnlineFundingFinalHooksRequireCompleteReceiptAndKeepSQLGuards(t *testing.T) {
	source, _, _ := importTestDB(t)
	seedFundingFixture(t, source)
	ctx := context.Background()
	tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := loadFunding(ctx, tx, sources)
	if err != nil {
		t.Fatal(err)
	}
	// Ordinary imports still inspect every row when there is no private view.
	ordinary := Report{}
	if err := d.validateContext(ctx, &ordinary); err != nil || len(ordinary.Issues) != 0 || ordinary.Counts["billing_funding_lots"] != 1 || ordinary.Amounts["billing_funding_lots_remaining_amount_micro_credits"] != "120" {
		t.Fatalf("nil view changed offline validation: %+v %v", ordinary, err)
	}
	base := map[string]string{
		"v3_billing.funding_lots.rows":                                    "1",
		"v3_billing.funding_lots.original_amount":                         "200",
		"v3_billing.funding_lots.remaining_amount":                        "120",
		"v3_billing.funding_allocations.rows":                             "1",
		"v3_billing.funding_allocations.amount":                           "80",
		"retired_features.billing_funding_lots":                           "1",
		"retired_features.billing_funding_lots.remaining_amount_v2_units": "9223372036854775807",
		"retired_features.billing_funding_allocations":                    "1",
	}
	d.source = onlineDomainNoScanTx{Tx: tx, tables: []string{fundingSource(sources, "funding_lots"), fundingSource(sources, "funding_allocations")}}
	finalCtx := context.WithValue(ctx, onlineContextKey{}, &onlineView{amounts: base})
	report := Report{}
	if err := d.validateContext(finalCtx, &report); err != nil || len(report.Issues) != 0 || report.Counts["billing_funding_allocations"] != 1 || report.Amounts["billing_funding_lots_original_amount_micro_credits"] != "200" || report.Counts["retired_features.billing_funding_lots"] != 1 || report.Amounts["retired_features.billing_funding_lots.remaining_amount_v2_units"] != "9223372036854775807" {
		t.Fatalf("verified summaries changed funding evidence: %+v %v", report, err)
	}
	if err := d.validateContext(ctx, &Report{}); err == nil {
		t.Fatal("nil view bypassed the original historical scan")
	}
	for _, name := range []string{"funding_lots", "funding_allocations"} {
		if err := d.batches(finalCtx, name, nil, func(string, []map[string]any) error {
			t.Fatal("verified funding data was imported twice")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ name, key, value string }{
		{"missing_count", "v3_billing.funding_lots.rows", ""},
		{"negative_count", "v3_billing.funding_lots.rows", "-1"},
		{"invalid_count", "v3_billing.funding_lots.rows", "1.5"},
		{"overflow_count", "v3_billing.funding_lots.rows", "9223372036854775808"},
		{"missing_amount", "v3_billing.funding_lots.remaining_amount", ""},
		{"negative_amount", "v3_billing.funding_lots.original_amount", "-1"},
		{"invalid_amount", "v3_billing.funding_allocations.amount", "NaN"},
		{"invalid_retired_count", "retired_features.billing_funding_lots", "-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{}
			for key, value := range base {
				values[key] = value
			}
			if tc.value == "" {
				delete(values, tc.key)
			} else {
				values[tc.key] = tc.value
			}
			invalidCtx := context.WithValue(ctx, onlineContextKey{}, &onlineView{amounts: values})
			if err := d.validateContext(invalidCtx, &Report{}); err == nil {
				t.Fatalf("invalid receipt was accepted: %s", tc.key)
			}
		})
	}
	// sum(bigint) can exceed signed bigint: receipt aggregates stay decimal.
	base["v3_billing.funding_lots.original_amount"] = "18446744073709551612"
	large := Report{}
	if err := d.validateContext(finalCtx, &large); err != nil || large.Amounts["billing_funding_lots_original_amount_micro_credits"] != "18446744073709551612" {
		t.Fatalf("receipt aggregate was truncated: %+v %v", large, err)
	}
	if _, err := source.Exec(ctx, `UPDATE billing.funding_allocations SET amount=41 WHERE allocation_id='funding-box-allocation'`); err != nil {
		t.Fatal(err)
	}
	base["v3_billing.funding_allocations.amount"] = "82"
	violated := Report{}
	if err := d.validateContext(finalCtx, &violated); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, issue := range violated.Issues {
		found = found || issue.Code == "funding_allocation_exceeds_consumed"
	}
	if !found {
		t.Fatalf("verified view bypassed source funding association SQL: %+v", violated)
	}
}

func TestOnlineMarketFinalHooksRequireCountsAndKeepBalanceGuards(t *testing.T) {
	source, _, _ := importTestDB(t)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `INSERT INTO migration_source.users(id,username,role,status,"group",quota,claude_quota,setting) VALUES(8,'consumer',1,1,'default',0,0,'{}')`); err != nil {
		t.Fatal(err)
	}
	sources := seedChannelMarketFixture(t, source)
	sources["users"], sources["channels"], sources["accounts"], sources["balance_snapshots"] = "migration_source.users", "migration_source.channels", "billing.accounts", "billing.balance_snapshots"
	tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ordinary, err := loadChannelMarket(ctx, tx, sources)
	if err != nil || len(ordinary.issues) != 0 || ordinary.streamCounts["settlements"] != 1 || ordinary.pending[7] != 190 {
		t.Fatalf("nil view changed offline market: %+v %v", ordinary, err)
	}
	counts := map[string]int64{}
	tables := []string{}
	for _, name := range channelMarketSourceTables {
		if cmStreamedTable(name) {
			counts[name] = 1
			tables = append(tables, sources["marketplace_"+name])
		}
	}
	guarded := onlineDomainNoScanTx{Tx: tx, tables: tables}
	view := &onlineView{marketCounts: counts, marketPending: map[int64]int64{7: 190}}
	finalCtx := context.WithValue(ctx, onlineContextKey{}, view)
	data, err := loadChannelMarket(finalCtx, guarded, sources)
	if err != nil || len(data.issues) != 0 || data.streamCounts["settlements"] != 1 || data.pending[7] != 190 || data.platformRevenue == nil || *data.platformRevenue != 10 {
		t.Fatalf("verified market receipt or live account guard changed: %+v %v", data, err)
	}
	if _, err := loadChannelMarket(ctx, guarded, sources); err == nil {
		t.Fatal("nil view bypassed original market scanning")
	}
	if err := data.streamBatches(finalCtx, "settlements", func([]cmRecord) error {
		t.Fatal("verified market history was imported twice")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, missing := range []bool{true, false} {
		invalidCounts := map[string]int64{}
		for name, count := range counts {
			invalidCounts[name] = count
		}
		if missing {
			delete(invalidCounts, "settlements")
		} else {
			invalidCounts["settlements"] = -1
		}
		invalidCtx := context.WithValue(ctx, onlineContextKey{}, &onlineView{marketCounts: invalidCounts, marketPending: view.marketPending})
		if _, err := loadChannelMarket(invalidCtx, guarded, sources); err == nil {
			t.Fatalf("invalid market receipt accepted missing=%t", missing)
		}
	}
	if _, err := source.Exec(ctx, `UPDATE billing.balance_snapshots SET reserved_balance=1 WHERE account_id='fixture-market-pending-7'`); err != nil {
		t.Fatal(err)
	}
	data, err = loadChannelMarket(finalCtx, guarded, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.issues) == 0 {
		t.Fatal("verified view bypassed reserved-balance guard")
	}
}

func TestOnlineDomainAdaptersRejectInvalidReferencesAndAmounts(t *testing.T) {
	source, _, _ := importTestDB(t)
	seedFundingFixture(t, source)
	ctx := context.Background()
	tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	funding, err := loadFunding(ctx, tx, sources)
	if err != nil {
		t.Fatal(err)
	}
	p := &onlineProjector{funding: funding, mappings: map[fundingAccountKey]int64{{owner: "user", kind: "wallet", id: 7}: 901}}
	var lotRaw []byte
	if err := tx.QueryRow(ctx, `SELECT to_jsonb(t) FROM billing.funding_lots t WHERE lot_id='funding-box-lot'`).Scan(&lotRaw); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, field string
		value       json.RawMessage
	}{
		{"missing_account", "account_id", json.RawMessage(`"absent"`)},
		{"overflow", "original_amount", json.RawMessage(`4611686018427387904`)},
		{"negative", "remaining_amount", json.RawMessage(`-1`)},
		{"remaining_exceeds_original", "remaining_amount", json.RawMessage(`101`)},
		{"rate_overflow", "revenue_multiplier", json.RawMessage(`9223372036854.775808`)},
		{"rate_nondecimal", "revenue_multiplier", json.RawMessage(`"1/3"`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var row commerceRow
			if err := json.Unmarshal(lotRaw, &row); err != nil {
				t.Fatal(err)
			}
			row[tc.field] = tc.value
			raw, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			if projections, err := p.projectOnlineFunding("funding_lots", raw); err == nil || len(projections) != 0 {
				t.Fatalf("invalid row was staged: %+v %v", projections, err)
			}
		})
	}
	p.mappings = nil
	if _, err := p.projectOnlineFunding("funding_lots", lotRaw); err == nil {
		t.Fatal("nonempty lot accepted missing reserved native account")
	}
	// Retired accounts are filtered before missing fields and overflow checks.
	for _, name := range []string{"funding_lots", "funding_allocations"} {
		if projections, err := p.projectOnlineFunding(name, json.RawMessage(`{"account_id":"retired-gpt-7","original_amount":9223372036854775807,"amount":9223372036854775807,"revenue_multiplier":-99}`)); err != nil || len(projections) != 0 {
			t.Fatalf("retired %s was validated as money: %+v %v", name, projections, err)
		}
	}
	for _, raw := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(`[]`), json.RawMessage(`{`)} {
		if _, err := p.projectOnlineFunding("funding_lots", raw); err == nil {
			t.Fatalf("non-object funding row accepted: %s", raw)
		}
	}
}

func TestOnlineMarketDependenciesAndClosedReferences(t *testing.T) {
	source, _, _ := marketMigrationDBs(t)
	ctx := context.Background()
	sources := seedChannelMarketFixture(t, source)
	if _, err := source.Exec(ctx, `CREATE TABLE public.cm_users(id bigint);INSERT INTO public.cm_users VALUES(7),(8);CREATE TABLE public.cm_channels(id bigint,"group" text);INSERT INTO public.cm_channels VALUES(13,'default');UPDATE marketplace.verification_runs SET status='unknown'`); err != nil {
		t.Fatal(err)
	}
	sources["users"], sources["channels"] = "public.cm_users", "public.cm_channels"
	tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	market, err := loadChannelMarketBase(ctx, tx, sources, false)
	if err != nil || len(market.issues) != 0 {
		t.Fatalf("online loader inspected invalid history: %+v %v", market, err)
	}
	if _, err := loadChannelMarket(ctx, tx, sources); err == nil {
		t.Fatal("offline loader must continue inspecting invalid historical rows")
	}
	p := &onlineProjector{market: market}
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT to_jsonb(t) FROM marketplace.settlements t`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"group_id", "owner_user_id", "consumer_user_id"} {
		var row cmRow
		if err := json.Unmarshal(raw, &row); err != nil {
			t.Fatal(err)
		}
		value := `999`
		if field == "group_id" {
			value = `"absent"`
		}
		row[field] = json.RawMessage(value)
		changed, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		if projections, err := p.projectOnlineMarket("settlements", changed); err == nil || len(projections) != 0 {
			t.Fatalf("broken %s reference accepted: %+v %v", field, projections, err)
		}
	}
	if _, err := p.projectOnlineMarket("channels", raw); err == nil {
		t.Fatal("non-streaming table accepted")
	}
	funding := fundingUnitFixture(t)
	p.funding = funding
	before := p.onlineDependencyValues()
	account := funding.accounts["wallet"]
	account.Version++
	account.Status = "closed"
	funding.accounts["wallet"] = account
	user := funding.users[7]
	user.DisplayName = "changed display"
	funding.users[7] = user
	if !reflect.DeepEqual(before, p.onlineDependencyValues()) {
		t.Fatal("mutable non-projection fields invalidate dependencies")
	}
	account.OwnerID = 8
	funding.accounts["wallet"] = account
	if reflect.DeepEqual(before["funding.account:wallet"], p.onlineDependencyValues()["funding.account:wallet"]) {
		t.Fatal("account owner remapping was omitted")
	}
	user.CreatedAt++
	funding.users[7] = user
	if reflect.DeepEqual(before["funding.user:7"], p.onlineDependencyValues()["funding.user:7"]) {
		t.Fatal("user age dependency was omitted")
	}
	market.channels["legacy-public-201"].catalogID++
	if reflect.DeepEqual(before["market.channel:legacy-public-201"], p.onlineDependencyValues()["market.channel:legacy-public-201"]) {
		t.Fatal("public-to-catalog remapping was omitted")
	}
	market.groups["legacy-group-201"]["channel_id"] = json.RawMessage(`"changed"`)
	if reflect.DeepEqual(before["market.group:legacy-group-201"], p.onlineDependencyValues()["market.group:legacy-group-201"]) {
		t.Fatal("group-to-channel remapping was omitted")
	}
	delete(market.users, 8)
	if _, exists := p.onlineDependencyValues()["market.user:8"]; exists {
		t.Fatal("deleted market user remained a dependency")
	}
}
