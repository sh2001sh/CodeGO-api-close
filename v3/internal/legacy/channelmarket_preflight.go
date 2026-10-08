package legacy

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func (d *channelMarketData) validate(report *Report) {
	report.Issues = append(report.Issues, d.issues...)
	if report.Counts == nil {
		report.Counts = map[string]int64{}
	}
	if report.Amounts == nil {
		report.Amounts = map[string]string{}
	}
	for table, rows := range d.rows {
		report.Counts["marketplace_"+table] = int64(len(rows))
	}
	for table, count := range d.streamCounts {
		report.Counts["marketplace_"+table] = count
	}
	for owner, amount := range d.pending {
		report.Amounts["marketplace_pending:"+strconv.FormatInt(owner, 10)] = strconv.FormatInt(amount, 10)
	}
	if d.platformRevenue != nil {
		report.Amounts["marketplace_platform_revenue"] = strconv.FormatInt(*d.platformRevenue, 10)
	}
}
func (d *channelMarketData) issue(table string, row cmRow, err error) {
	id, _ := row.integer("id")
	d.issues = append(d.issues, Issue{Entity: "marketplace_" + table, ID: id, Code: "invalid_market_data", Detail: fmt.Sprintf("row %s: %s", row.text("id"), err)})
}
func (d *channelMarketData) user(id int64) error {
	if id <= 0 || !d.users[id] {
		return fmt.Errorf("unknown user %d", id)
	}
	return nil
}
func (d *channelMarketData) channel(id string) (*cmChannel, error) {
	c := d.channels[id]
	if c == nil {
		return nil, fmt.Errorf("unknown public channel %s", id)
	}
	return c, nil
}
func (d *channelMarketData) group(id string) (cmRow, error) {
	g := d.groups[id]
	if g == nil {
		return nil, fmt.Errorf("unknown group %s", id)
	}
	return g, nil
}
func (d *channelMarketData) record(table string, keys []string, b *cmBuilder, row cmRow) {
	if b.err != nil {
		d.issue(strings.TrimPrefix(table, "v3_channelmarket."), row, b.err)
		return
	}
	d.records = append(d.records, cmRecord{table: table, keys: keys, values: b.values})
}

func (d *channelMarketData) validateRecords() {
	keys := map[string]bool{}
	uniques := map[string]bool{}
	for _, r := range d.records {
		keyValues := make([]any, 0, len(r.keys))
		for _, key := range r.keys {
			value := r.values[key]
			if value == nil || value == "" {
				d.issues = append(d.issues, Issue{Entity: r.table, Code: "invalid_market_data", Detail: "empty required source key " + key})
			}
			keyValues = append(keyValues, value)
		}
		encoded, _ := json.Marshal(keyValues)
		key := r.table + ":" + string(encoded)
		if keys[key] {
			d.issues = append(d.issues, Issue{Entity: r.table, Code: "invalid_market_data", Detail: "duplicate native source key"})
		}
		keys[key] = true
		uniqueFields := []string{}
		switch r.table {
		case "v3_channelmarket.groups":
			uniqueFields = []string{"public_channel_id", "channel_id", "public_slug", "internal_group_name"}
		case "v3_channelmarket.group_invites":
			uniqueFields = []string{"token_hash"}
		case "v3_channelmarket.settlements":
			uniqueFields = []string{"request_id"}
		}
		for _, field := range uniqueFields {
			encoded, _ := json.Marshal(r.values[field])
			key := r.table + ":" + field + ":" + string(encoded)
			if uniques[key] {
				d.issues = append(d.issues, Issue{Entity: r.table, Code: "invalid_market_data", Detail: "duplicate " + field})
			}
			uniques[key] = true
		}
	}
}
