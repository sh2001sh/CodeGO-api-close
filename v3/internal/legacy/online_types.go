package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Online staging owns no money. Account IDs are reserved without inserting
// accounts or opening entries; finalization posts the frozen openings once.
type OnlineOptions struct {
	RunID       string
	SourceAdmin *pgxpool.Pool
}

type OnlineReport struct {
	RunID             string           `json:"run_id"`
	Phase             string           `json:"phase"`
	Applied           bool             `json:"applied"`
	Copied            int64            `json:"copied_rows"`
	Acknowledged      int64            `json:"acknowledged_events"`
	Pending           int64            `json:"pending_events"`
	Tables            map[string]int64 `json:"tables"`
	LedgerHistoryMode string           `json:"ledger_history_mode"`
	HistoryCutoff     string           `json:"history_cutoff,omitempty"`
}

type onlineProjection struct {
	Schema, Table string
	Keys          []string
	Values        map[string]any
}

type onlineProjector struct {
	funding  *fundingData
	market   *channelMarketData
	mappings map[fundingAccountKey]int64
	history  *historyData
	users    map[int64]bool
	target   pgx.Tx
}

type onlineSpec struct {
	name, source       string
	keys               []string
	targets            []string
	requests, attempts string
}

func onlineSpecs(sources map[string]string) []onlineSpec {
	var specs []onlineSpec
	add := func(name, source string, keys []string, targets ...string) {
		if source != "" {
			specs = append(specs, onlineSpec{name: name, source: source, keys: keys, targets: targets, requests: sources["request_audits"], attempts: sources["request_attempt_audits"]})
		}
	}
	add("ledger_entries", sources["ledger_entries"], []string{"entry_id"}, "v3_billing.historical_entries")
	add("logs", sources["logs"], []string{"id"}, "v3_audit.events", "v3_billing.usage_logs")
	add("request_audits", sources["request_audits"], []string{"request_id"}, "v3_audit.request_audits")
	add("request_attempt_audits", sources["request_attempt_audits"], []string{"attempt_id"}, "v3_audit.request_attempt_audits", "v3_audit.orphan_request_attempt_history")
	for _, name := range []string{"funding_lots", "funding_allocations"} {
		key := "lot_id"
		if name == "funding_allocations" {
			key = "allocation_id"
		}
		add(name, fundingSource(sources, name), []string{key}, "v3_billing."+name)
	}
	for _, name := range channelMarketSourceTables {
		if !cmStreamedTable(name) {
			continue
		}
		keys := []string{"id"}
		if name == "pelican_artifacts" {
			keys = []string{"group_id", "model"}
		}
		add("market."+name, sources["marketplace_"+name], keys, "v3_channelmarket."+name)
	}
	return specs
}

func onlineStage(table string) string {
	return pgx.Identifier{"v3_migration_online", strings.ReplaceAll(table, ".", "__")}.Sanitize()
}

type onlineContextKey struct{}
type onlineView struct {
	runID         string
	counts        map[string]int64
	amounts       map[string]string
	marketPending map[int64]int64
	marketCounts  map[string]int64
}

func onlineViewFrom(ctx context.Context) *onlineView {
	v, _ := ctx.Value(onlineContextKey{}).(*onlineView)
	return v
}

// A transaction adapter reuses the offline typed check against staging. It
// never changes source SQL, and never aliases balances or the live ledger.
type onlineStageTx struct {
	pgx.Tx
	tables []string
}

func (t onlineStageTx) sql(query string) string {
	for _, name := range t.tables {
		parts := strings.Split(name, ".")
		query = strings.ReplaceAll(query, pgx.Identifier(parts).Sanitize(), onlineStage(name))
		query = strings.ReplaceAll(query, name, onlineStage(name))
	}
	return strings.ReplaceAll(query, "v3_billing.accounts", "v3_migration_online.account_ids")
}
func (t onlineStageTx) Query(ctx context.Context, q string, args ...any) (pgx.Rows, error) {
	return t.Tx.Query(ctx, t.sql(q), args...)
}
func (t onlineStageTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	return t.Tx.QueryRow(ctx, t.sql(q), args...)
}

func onlineProjectionFields(value any, renames map[string]string, omit []string, dates map[string]historyTime) (map[string]any, error) {
	p, err := historyJSONProjection(value, renames, omit)
	if err != nil {
		return nil, err
	}
	putHistoryDates(p, dates)
	return historyProjectionFields(p)
}

func onlineKey(raw json.RawMessage, keys []string) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	key := map[string]json.RawMessage{}
	for _, name := range keys {
		v := fields[name]
		if len(v) == 0 || string(v) == "null" {
			return nil, fmt.Errorf("legacy: online source key %s missing", name)
		}
		key[name] = v
	}
	b, err := json.Marshal(key)
	return b, err
}
