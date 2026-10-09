package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

type onlineInput struct{ key, raw json.RawMessage }
type onlineMetrics map[string]*big.Int

var onlineMetricFields = []string{"amount", "original_amount", "remaining_amount", "consumed_amount", "actual_amount", "consumer_micro", "gross_micro", "commission_micro", "fee_micro", "net_micro", "reclaimed_micro"}

func (m onlineMetrics) add(name, value string, sign int64) error {
	n, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return fmt.Errorf("legacy: invalid online numeric metric")
	}
	if m[name] == nil {
		m[name] = new(big.Int)
	}
	m[name].Add(m[name], n.Mul(n, big.NewInt(sign)))
	return nil
}
func (m onlineMetrics) row(table string, raw []byte, sign int64) error {
	if err := m.add(table+".rows", "1", sign); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for _, field := range onlineMetricFields {
		value := fields[field]
		if len(value) == 0 || string(value) == "null" {
			continue
		}
		if err := m.add(table+"."+field, string(value), sign); err != nil {
			return err
		}
	}
	if table == "v3_channelmarket.settlements" && string(fields["status"]) == `"pending"` {
		return m.add("market.pending."+string(fields["owner_user_id"]), string(fields["net_micro"]), sign)
	}
	return nil
}

// Projection values have already passed PostgreSQL's typed exact check. Encode
// only the small numeric scalars for big.Int, never their unrelated metadata.
func (m onlineMetrics) fields(table string, fields map[string]any, sign int64) error {
	if err := m.add(table+".rows", "1", sign); err != nil {
		return err
	}
	for _, field := range onlineMetricFields {
		value, exists := fields[field]
		if !exists || value == nil {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if string(encoded) == "null" {
			continue
		}
		if err := m.add(table+"."+field, string(encoded), sign); err != nil {
			return err
		}
	}
	if table == "v3_channelmarket.settlements" {
		status, err := json.Marshal(fields["status"])
		if err != nil {
			return err
		}
		if string(status) == `"pending"` {
			owner, err := json.Marshal(fields["owner_user_id"])
			if err != nil {
				return err
			}
			net, err := json.Marshal(fields["net_micro"])
			if err != nil {
				return err
			}
			return m.add("market.pending."+string(owner), string(net), sign)
		}
	}
	return nil
}

// These are the complete numeric fields of the fixed online projection tables.
// Returning them directly avoids serializing/toasting large content or metadata
// a second time just to decrement a handful of counters.
func onlineDeleteMetrics(table string) string {
	var fields []string
	switch table {
	case "v3_billing.historical_entries", "v3_billing.usage_logs", "v3_audit.events", "v3_audit.request_audits", "v3_billing.funding_allocations":
		fields = []string{"amount"}
	case "v3_billing.funding_lots":
		fields = []string{"original_amount", "remaining_amount"}
	case "v3_channelmarket.settlements":
		fields = []string{"consumer_micro", "gross_micro", "commission_micro", "fee_micro", "net_micro", "reclaimed_micro", "owner_user_id", "status"}
	}
	if len(fields) == 0 {
		return "'{}'::jsonb"
	}
	parts := make([]string, 0, 2*len(fields))
	for _, field := range fields {
		parts = append(parts, "'"+field+"'", "h."+pgx.Identifier{field}.Sanitize())
	}
	return "jsonb_build_object(" + strings.Join(parts, ",") + ")"
}
func (m onlineMetrics) save(ctx context.Context, target pgx.Tx) error {
	for name, value := range m {
		if value.Sign() == 0 {
			continue
		}
		if _, err := target.Exec(ctx, `INSERT INTO v3_migration_online.totals(name,value) VALUES($1,$2::numeric) ON CONFLICT(name)DO UPDATE SET value=v3_migration_online.totals.value+EXCLUDED.value`, name, value.String()); err != nil {
			return err
		}
	}
	return nil
}

func onlineDeleteRows(ctx context.Context, target pgx.Tx, table string, keys []string, keyRows []map[string]any, metrics onlineMetrics) error {
	if len(keyRows) == 0 {
		return nil
	}
	data, err := json.Marshal(keyRows)
	if err != nil {
		return err
	}
	conditions := make([]string, len(keys))
	for i, key := range keys {
		q := pgx.Identifier{key}.Sanitize()
		conditions[i] = "h." + q + "=e." + q
	}
	stage := onlineStage(table)
	rows, err := target.Query(ctx, "DELETE FROM "+stage+" h USING jsonb_populate_recordset(NULL::"+stage+",$1::jsonb)e WHERE "+strings.Join(conditions, " AND ")+" RETURNING "+onlineDeleteMetrics(table), data)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		if err = metrics.row(table, raw, -1); err != nil {
			return err
		}
	}
	return rows.Err()
}

func onlineInsertRows(ctx context.Context, target pgx.Tx, table string, keys []string, values []map[string]any, metrics onlineMetrics) error {
	start, bytes := 0, 2
	var encodedRows [][]byte
	for i, row := range values {
		encoded, err := exactBulkEncodeRow(row)
		if err != nil {
			return err
		}
		if len(encoded)+2 > onlineRowMaxBytes {
			return fmt.Errorf("legacy: online encoded projection exceeds 64 MiB limit")
		}
		if i > start && (i-start == exactBulkRows || bytes+len(encoded)+1 > exactBulkBytes) {
			if err = onlineInsertBatchRows(ctx, target, table, keys, values[start:i], encodedRows, metrics); err != nil {
				return err
			}
			start, bytes = i, 2
			clear(encodedRows)
			encodedRows = encodedRows[:0]
		}
		encodedRows = append(encodedRows, encoded)
		bytes += len(encoded) + 1
	}
	return onlineInsertBatchRows(ctx, target, table, keys, values[start:], encodedRows, metrics)
}

func onlineInsertBatchRows(ctx context.Context, target pgx.Tx, table string, keys []string, values []map[string]any, encodedRows [][]byte, metrics onlineMetrics) error {
	if len(values) == 0 {
		return nil
	}
	// Typed recordset insertion uses the same numeric/bytea rules as the offline
	// importer. Exact checks occur in this transaction before any event is acked.
	parts := strings.Split(table, ".")
	_, columns, data, err := exactBulkEncodedInput(parts[0], parts[1], keys, values, encodedRows)
	if err != nil {
		return err
	}
	quoted, selected := make([]string, len(columns)), make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = pgx.Identifier{col}.Sanitize()
		selected[i] = "e." + quoted[i]
	}
	stage := onlineStage(table)
	if _, err = target.Exec(ctx, "INSERT INTO "+stage+"("+strings.Join(quoted, ",")+")OVERRIDING SYSTEM VALUE SELECT "+strings.Join(selected, ",")+" FROM jsonb_populate_recordset(NULL::"+stage+",$1::jsonb)e", data); err != nil {
		return err
	}
	matches, err := checkExactBulkData(ctx, target, stage, keys, columns, data)
	if err != nil {
		return err
	}
	if len(matches) != len(values) {
		return fmt.Errorf("legacy: online exact check returned wrong row count")
	}
	for _, match := range matches {
		if !match {
			return fmt.Errorf("legacy: online staged row differs from typed source")
		}
	}
	for _, row := range values {
		if err = metrics.fields(table, row, 1); err != nil {
			return err
		}
	}
	return nil
}

func onlineKeyFields(raw json.RawMessage) (map[string]any, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	values := map[string]any{}
	for name, value := range fields {
		values[name] = value
	}
	return values, nil
}

func (p *onlineProjector) apply(ctx context.Context, spec onlineSpec, inputs []onlineInput) error {
	keys := make([]json.RawMessage, len(inputs))
	for i, input := range inputs {
		keys[i] = input.key
	}
	if err := p.delete(ctx, spec, keys); err != nil {
		return err
	}
	return p.insert(ctx, spec, inputs)
}

// Delete and insert are separate phases during replay so unique values can move
// between rows in different byte-bounded batches. Both share the caller's target
// transaction; a later bad projection rolls back every earlier deletion.
func (p *onlineProjector) delete(ctx context.Context, spec onlineSpec, sourceKeys []json.RawMessage) error {
	metrics := onlineMetrics{}
	keys := make([]map[string]any, len(sourceKeys))
	for i, key := range sourceKeys {
		var err error
		keys[i], err = onlineKeyFields(key)
		if err != nil {
			return err
		}
	}
	var logGroups []map[string]any
	if spec.name == "logs" {
		data, err := json.Marshal(keys)
		if err != nil {
			return err
		}
		stage := onlineStage("v3_audit.events")
		// Content/metadata may be large. Old grouping and usage partition keys need
		// only these fields, never a second complete historical-row decode.
		rows, err := p.target.Query(ctx, "SELECT jsonb_build_object('created_at',h.created_at,'id',h.id,'user_id',h.user_id,'request_id',h.request_id) FROM "+stage+" h JOIN jsonb_populate_recordset(NULL::"+stage+",$1::jsonb)e ON h.id=e.id", data)
		if err != nil {
			return err
		}
		var usageKeys []map[string]any
		for rows.Next() {
			var raw []byte
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return err
			}
			fields, err := onlineKeyFields(raw)
			if err != nil {
				rows.Close()
				return err
			}
			usageKeys = append(usageKeys, map[string]any{"created_at": fields["created_at"], "id": fields["id"]})
			logGroups = append(logGroups, fields)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		if err = onlineDeleteRows(ctx, p.target, "v3_billing.usage_logs", []string{"created_at", "id"}, usageKeys, metrics); err != nil {
			return err
		}
	}
	before, err := onlineDuplicateCount(ctx, p.target, logGroups)
	if err != nil {
		return err
	}
	for _, table := range spec.targets {
		if table == "v3_billing.usage_logs" {
			continue
		}
		if err = onlineDeleteRows(ctx, p.target, table, spec.keys, keys, metrics); err != nil {
			return err
		}
	}
	if spec.name == "logs" {
		if err = onlineNormalizeLogs(ctx, p.target, logGroups, false); err != nil {
			return err
		}
		after, err := onlineDuplicateCount(ctx, p.target, logGroups)
		if err != nil {
			return err
		}
		if err = metrics.add("history.log_duplicates", fmt.Sprint(after-before), 1); err != nil {
			return err
		}
	}
	return metrics.save(ctx, p.target)
}

func (p *onlineProjector) insert(ctx context.Context, spec onlineSpec, inputs []onlineInput) error {
	metrics := onlineMetrics{}
	var logGroups []map[string]any
	projected := map[string][]map[string]any{}
	targetKeys := map[string][]string{}
	for _, input := range inputs {
		if err := p.replaceRetired(ctx, spec, input, metrics); err != nil {
			return err
		}
		if len(input.raw) == 0 {
			continue
		}
		if spec.name == "logs" {
			l, err := decodeHistoryLog(input.raw)
			if err != nil {
				return err
			}
			logGroups = append(logGroups, map[string]any{"created_at": historyDate(l.CreatedAt), "user_id": l.UserID, "request_id": l.RequestID})
		}
		out, err := p.project(ctx, spec, input.raw)
		if err != nil {
			return err
		}
		for _, row := range out {
			table := row.Schema + "." + row.Table
			projected[table] = append(projected[table], row.Values)
			targetKeys[table] = row.Keys
		}
	}
	before, err := onlineDuplicateCount(ctx, p.target, logGroups)
	if err != nil {
		return err
	}
	for _, table := range spec.targets {
		if err = onlineInsertRows(ctx, p.target, table, targetKeys[table], projected[table], metrics); err != nil {
			return err
		}
	}
	if spec.name == "logs" {
		if err = onlineNormalizeLogs(ctx, p.target, logGroups, false); err != nil {
			return err
		}
		after, err := onlineDuplicateCount(ctx, p.target, logGroups)
		if err != nil {
			return err
		}
		if err = metrics.add("history.log_duplicates", fmt.Sprint(after-before), 1); err != nil {
			return err
		}
	}
	return metrics.save(ctx, p.target)
}

func (p *onlineProjector) replaceRetired(ctx context.Context, spec onlineSpec, input onlineInput, metrics onlineMetrics) error {
	var before []byte
	err := p.target.QueryRow(ctx, "DELETE FROM v3_migration_online.retired_rows WHERE name=$1 AND row_key=$2 RETURNING metrics", spec.name, input.key).Scan(&before)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	apply := func(raw []byte, sign int64) error {
		var values map[string]string
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		for key, value := range values {
			if err := metrics.add(key, value, sign); err != nil {
				return err
			}
		}
		return nil
	}
	if err == nil {
		if err = apply(before, -1); err != nil {
			return err
		}
	}
	if len(input.raw) == 0 {
		return nil
	}
	values := map[string]string{}
	if spec.name == "ledger_entries" && p.history.retiredHistoryEntry(input.raw) {
		d := &historyData{counts: map[string]int64{}, amounts: map[string]*big.Int{}}
		d.reportRetiredHistoryEntry(input.raw)
		for key, count := range d.counts {
			values["retired.history."+key] = fmt.Sprint(count)
		}
		for key, amount := range d.amounts {
			values["retired.history."+key] = amount.String()
		}
	} else if spec.name == "funding_lots" || spec.name == "funding_allocations" {
		var row commerceRow
		if err = json.Unmarshal(input.raw, &row); err != nil {
			return err
		}
		if p.funding.retiredFundingRow(spec.name, row) {
			r := Report{Counts: map[string]int64{}, Amounts: map[string]string{}}
			reportRetiredFunding(&r, spec.name, row)
			for key, count := range r.Counts {
				values[key] = fmt.Sprint(count)
			}
			for key, amount := range r.Amounts {
				values[key] = amount
			}
		}
	}
	if len(values) == 0 {
		return nil
	}
	b, err := json.Marshal(values)
	if err != nil {
		return err
	}
	if _, err = p.target.Exec(ctx, "INSERT INTO v3_migration_online.retired_rows(name,row_key,metrics)VALUES($1,$2,$3)", spec.name, input.key, b); err != nil {
		return err
	}
	return apply(b, 1)
}

func onlineDuplicateCount(ctx context.Context, target pgx.Tx, groups []map[string]any) (int64, error) {
	if len(groups) == 0 {
		return 0, nil
	}
	b, err := json.Marshal(groups)
	if err != nil {
		return 0, err
	}
	var count int64
	events := onlineStage("v3_audit.events")
	query := `SELECT COALESCE(sum(n),0)::bigint FROM(SELECT count(*) n FROM ` + events + ` e JOIN(SELECT DISTINCT created_at,user_id,request_id FROM jsonb_populate_recordset(NULL::` + events + `,$1::jsonb))g USING(created_at,user_id,request_id) WHERE e.event_type=2 AND e.request_id<>'' GROUP BY e.created_at,e.user_id,e.request_id HAVING count(*)>1)s`
	err = target.QueryRow(ctx, query, b).Scan(&count)
	return count, err
}

func onlineNormalizeLogs(ctx context.Context, target pgx.Tx, groups []map[string]any, all bool) error {
	if !all && len(groups) == 0 {
		return nil
	}
	events, usage := onlineStage("v3_audit.events"), onlineStage("v3_billing.usage_logs")
	filter := ""
	var args []any
	if !all {
		b, err := json.Marshal(groups)
		if err != nil {
			return err
		}
		args = append(args, b)
		filter = " JOIN(SELECT DISTINCT created_at,user_id,request_id FROM jsonb_populate_recordset(NULL::" + events + ",$1::jsonb))g USING(created_at,user_id,request_id)"
	}
	query := `WITH repeated AS(SELECT e.created_at,e.user_id,e.request_id,count(*) n FROM ` + events + ` e` + filter + ` WHERE e.event_type=2 AND e.request_id<>'' GROUP BY e.created_at,e.user_id,e.request_id)
	 UPDATE ` + usage + ` u SET request_id=CASE WHEN r.n>1 THEN e.request_id||':v2-log:'||e.id ELSE e.request_id END
	 FROM ` + events + ` e JOIN repeated r USING(created_at,user_id,request_id)
	 WHERE u.created_at=e.created_at AND u.id=e.id AND e.event_type=2 AND u.request_id IS DISTINCT FROM CASE WHEN r.n>1 THEN e.request_id||':v2-log:'||e.id ELSE e.request_id END`
	_, err := target.Exec(ctx, query, args...)
	return err
}

func onlineSortedTables(specs []onlineSpec) []string {
	seen := map[string]bool{"v3_billing.historical_accounts": true}
	for _, spec := range specs {
		for _, table := range spec.targets {
			seen[table] = true
		}
	}
	var out []string
	for table := range seen {
		out = append(out, table)
	}
	sort.Strings(out)
	return out
}
