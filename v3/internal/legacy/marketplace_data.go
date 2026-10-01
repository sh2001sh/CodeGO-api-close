package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// marketplaceSourceTables contains the actual GORM table names in v2, including
// data that must be rejected explicitly until its target semantics exist.
var marketplaceSourceTables = []string{
	"group_buy_orders", "group_buys", "group_buy_members", "blind_box_pools",
	"balance_blind_box_purchases", "balance_blind_box_items", "blind_box_purchases", "blind_box_items",
	"blind_box_open_records", "blind_box_props", "blind_box_pity_states", "balance_blind_box_pity_states",
	"blind_box_orders", "blind_box_grants", "blind_box_credits", "blind_box_prop_gifts",
	"blind_box_prop_discount_usages", "balance_blind_box_gifts", "balance_blind_box_gift_items",
	"blind_box_zero_hour_states",
}

type marketplaceSourceRow map[string]json.RawMessage
type marketplaceRecord struct {
	Table  string
	ID     int64
	Fields map[string]any
}
type marketplaceData struct {
	source  map[string][]marketplaceSourceRow
	records []marketplaceRecord
	issues  []Issue
}

func loadMarketplace(ctx context.Context, source pgx.Tx, sources map[string]string) (*marketplaceData, error) {
	d := &marketplaceData{source: make(map[string][]marketplaceSourceRow)}
	names := append(append([]string(nil), marketplaceSourceTables...), "subscription_plans", "user_subscriptions", "subscription_orders", "users", "options")
	for i, name := range names {
		table := sources[name]
		if i < len(marketplaceSourceTables) && sources["marketplace_"+name] != "" {
			table = sources["marketplace_"+name]
		}
		rows, err := loadRows(ctx, source, table)
		if err != nil {
			return nil, fmt.Errorf("legacy: read %s: %w", name, err)
		}
		for _, raw := range rows {
			var row marketplaceSourceRow
			if err := json.Unmarshal(raw, &row); err != nil {
				return nil, fmt.Errorf("legacy: decode %s: %w", name, err)
			}
			d.source[name] = append(d.source[name], row)
		}
	}
	d.normalizeGroups()
	d.normalizeBoxOrders()
	d.normalizeBlindBoxes()
	d.normalizeBoxOrderInventory()
	d.normalizeProps()
	d.normalizeBoxHistory()
	return d, nil
}

func (d *marketplaceData) validate(report *Report) {
	if report.Counts == nil {
		report.Counts = make(map[string]int64)
	}
	if report.Amounts == nil {
		report.Amounts = make(map[string]string)
	}
	totals := make(map[string]*big.Int)
	report.Issues = append(report.Issues, d.issues...)
	seen := make(map[string]bool)
	for _, row := range d.records {
		key := row.Table + ":" + strconv.FormatInt(row.ID, 10)
		if row.Table == "blind_box_pity" {
			key += ":" + fmt.Sprint(row.Fields["pool_id"])
		}
		synthetic := row.ID < 0 && (row.Table == "blind_box_purchases" || row.Table == "blind_box_items")
		if row.ID <= 0 && row.Table != "blind_box_pity" && !synthetic {
			report.Issues = append(report.Issues, Issue{row.Table, row.ID, "invalid_id", "positive source ID required"})
		}
		if seen[key] {
			report.Issues = append(report.Issues, Issue{row.Table, row.ID, "duplicate_marketplace_id", "multiple source rows map to one target identity"})
		}
		seen[key] = true
		report.Counts["marketplace."+row.Table]++
		for field, value := range row.Fields {
			if amount, ok := value.(int64); ok && (strings.HasSuffix(field, "_micro") || field == "amount_minor") {
				key := "marketplace." + row.Table + "." + field
				if field == "amount_minor" {
					key += ":" + fmt.Sprint(row.Fields["currency"])
				}
				if totals[key] == nil {
					totals[key] = new(big.Int)
				}
				totals[key].Add(totals[key], big.NewInt(amount))
			}
		}
	}
	for key, value := range totals {
		report.Amounts[key] = value.String()
	}
	retired := d.source["blind_box_credits"]
	if len(retired) > 0 {
		report.Counts["retired_features.blind_box_credits"] = int64(len(retired))
		for _, field := range []string{"original_amount", "remaining_amount"} {
			total := new(big.Int)
			for _, row := range retired {
				if value, ok := new(big.Int).SetString(row.text(field), 10); ok {
					total.Add(total, value)
				} else if row.text(field) != "" {
					report.Counts["retired_features.blind_box_credits."+field+"_unparseable"]++
				}
			}
			report.Amounts["retired_features.blind_box_credits."+field+"_v2_units"] = total.String()
		}
	}
}

func (d *marketplaceData) problem(entity string, id int64, code, detail string) {
	d.issues = append(d.issues, Issue{entity, id, code, detail})
}
func (d *marketplaceData) add(table string, id int64, fields map[string]any) {
	fields["id"] = id
	d.records = append(d.records, marketplaceRecord{table, id, fields})
}
func (r marketplaceSourceRow) id() int64 { v, _ := r.integer("id"); return v }
func (r marketplaceSourceRow) text(key string) string {
	raw := r[key]
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	return string(raw)
}
func (r marketplaceSourceRow) integer(key string) (int64, error) {
	v := r.text(key)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s is not an int64", key)
	}
	return n, nil
}
func (r marketplaceSourceRow) flag(key string) bool { return r.text(key) == "true" }
func (r marketplaceSourceRow) instant(key string) (*time.Time, error) {
	value := r.text(key)
	if value == "" || value == "0" {
		return nil, nil
	}
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		if n < 0 {
			return nil, fmt.Errorf("%s is negative", key)
		}
		t := time.Unix(n, 0).UTC()
		return &t, nil
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, fmt.Errorf("%s is not a timestamp", key)
	}
	return &t, nil
}
func marketplaceExact(value string, scale int64) (int64, error) {
	if value == "" {
		return 0, nil
	}
	n, ok := new(big.Rat).SetString(value)
	if !ok || n.Sign() < 0 {
		return 0, fmt.Errorf("invalid nonnegative decimal")
	}
	n.Mul(n, new(big.Rat).SetInt64(scale))
	if !n.IsInt() || !n.Num().IsInt64() {
		return 0, fmt.Errorf("decimal has excess precision or overflows int64")
	}
	return n.Num().Int64(), nil
}
func (d *marketplaceData) money(row marketplaceSourceRow, key string, scale int64) int64 {
	v, err := marketplaceExact(row.text(key), scale)
	if err != nil {
		d.problem(key, row.id(), "invalid_marketplace_amount", err.Error())
	}
	return v
}
func (d *marketplaceData) integer(row marketplaceSourceRow, key string) int64 {
	v, err := row.integer(key)
	if err != nil {
		d.problem(key, row.id(), "invalid_marketplace_integer", err.Error())
	}
	return v
}
func (d *marketplaceData) instant(row marketplaceSourceRow, key string, required bool) *time.Time {
	v, err := row.instant(key)
	if err != nil {
		d.problem(key, row.id(), "invalid_marketplace_time", err.Error())
	}
	if required && v == nil {
		d.problem(key, row.id(), "missing_marketplace_time", key+" is required")
	}
	return v
}
func marketplaceIndex(rows []marketplaceSourceRow) map[int64]marketplaceSourceRow {
	out := make(map[int64]marketplaceSourceRow)
	for _, r := range rows {
		out[r.id()] = r
	}
	return out
}
func marketplaceColumns(fields map[string]any) []string {
	out := make([]string, 0, len(fields))
	for k := range fields {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
