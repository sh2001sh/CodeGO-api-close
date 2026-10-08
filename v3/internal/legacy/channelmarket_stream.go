package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// These tables only depend on the small channel/group ownership maps. Keep
// their source transaction open rather than retaining millions of row maps.
func cmStreamedTable(table string) bool {
	switch table {
	case "settlements", "verification_runs", "gpt56_mapping_runs", "ranking_snapshots", "multiplier_trend_snapshots", "pelican_artifacts":
		return true
	}
	return false
}

func (d *channelMarketData) walkStreamed(ctx context.Context, table string, visit func(cmRow, cmRecord) error) error {
	if d.source == nil {
		return nil
	}
	return walkHistory(ctx, d.source, d.streamTables[table], func(raw json.RawMessage) error {
		var row cmRow
		if err := json.Unmarshal(raw, &row); err != nil {
			return fmt.Errorf("legacy: decode marketplace %s: %w", table, err)
		}
		var record cmRecord
		var err error
		if table == "settlements" {
			record, err = d.projectSettlement(row)
		} else {
			record, err = d.projectMarketHistory(table, row)
		}
		if err != nil {
			return fmt.Errorf("legacy: invalid marketplace %s projection: %w", table, err)
		}
		return visit(row, record)
	})
}

func (d *channelMarketData) addPendingSettlement(record cmRecord) error {
	if record.values["status"] != "pending" {
		return nil
	}
	owner := record.values["owner_user_id"].(int64)
	net := record.values["net_micro"].(int64)
	if d.pending[owner] > math.MaxInt64-net {
		return errors.New("pending owner income sum overflows")
	}
	d.pending[owner] += net
	return nil
}

func (d *channelMarketData) inspectStreamed(ctx context.Context) error {
	for _, table := range channelMarketSourceTables {
		if !cmStreamedTable(table) || d.streamTables[table] == "" {
			continue
		}
		if err := d.walkStreamed(ctx, table, func(row cmRow, record cmRecord) error {
			d.streamCounts[table]++
			for _, key := range record.keys {
				if record.values[key] == nil || record.values[key] == "" {
					return fmt.Errorf("legacy: empty marketplace %s source key %s", table, key)
				}
			}
			if table == "settlements" {
				return d.addPendingSettlement(record)
			}
			return nil
		}); err != nil {
			return err
		}
		// A SQL aggregate spills within PostgreSQL's work_mem limit. A Go
		// uniqueness map would retain a key for every historical settlement.
		keys := [][]string{{"id"}}
		if table == "settlements" {
			keys = append(keys, []string{"request_id"})
		} else if table == "pelican_artifacts" {
			keys = [][]string{{"group_id", "model"}}
		}
		for _, fields := range keys {
			quoted := make([]string, len(fields))
			for i, field := range fields {
				quoted[i] = pgx.Identifier{field}.Sanitize()
			}
			var duplicate bool
			if err := d.source.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+d.streamTables[table]+" GROUP BY "+strings.Join(quoted, ",")+" HAVING count(*)>1)").Scan(&duplicate); err != nil {
				return err
			}
			if duplicate {
				d.issues = append(d.issues, Issue{Entity: "marketplace_" + table, Code: "invalid_market_data", Detail: "duplicate source key " + strings.Join(fields, ",")})
			}
		}
	}
	return nil
}

func cmRecordShape(record cmRecord) string {
	columns := make([]string, 0, len(record.values))
	for column := range record.values {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	return strings.Join(columns, ",")
}

func (d *channelMarketData) streamBatches(ctx context.Context, table string, visit func([]cmRecord) error) error {
	const limit = 512
	batch := make([]cmRecord, 0, limit)
	shape := ""
	bytes := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := visit(batch); err != nil {
			return err
		}
		clear(batch)
		batch = batch[:0]
		bytes = 0
		return nil
	}
	err := d.walkStreamed(ctx, table, func(_ cmRow, record cmRecord) error {
		current := cmRecordShape(record)
		payload, err := json.Marshal(record.values)
		if err != nil {
			return err
		}
		if len(batch) == limit || (len(batch) > 0 && (shape != current || bytes+len(payload) > exactBulkBytes)) {
			if err := flush(); err != nil {
				return err
			}
		}
		shape = current
		bytes += len(payload)
		batch = append(batch, record)
		return nil
	})
	if err != nil {
		return err
	}
	return flush()
}
