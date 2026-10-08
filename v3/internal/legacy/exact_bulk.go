package legacy

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

const exactBulkRows = 512
const exactBulkBytes = 4 << 20

// insertExactBulk never overwrites a conflicting target row. Both the insert
// and the exact comparison belong to the caller's transaction; a conflict must
// make that transaction roll back, including previously imported batches.
func insertExactBulk(ctx context.Context, target pgx.Tx, schema, table string, keys []string, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	name, columns, data, err := exactBulkInput(schema, table, keys, rows)
	if err != nil {
		return err
	}
	quoted, selected := make([]string, len(columns)), make([]string, len(columns))
	for i, column := range columns {
		quoted[i] = pgx.Identifier{column}.Sanitize()
		selected[i] = "e." + quoted[i]
	}
	qkeys := make([]string, len(keys))
	for i, key := range keys {
		qkeys[i] = pgx.Identifier{key}.Sanitize()
	}
	query := "INSERT INTO " + name + " (" + strings.Join(quoted, ",") + ") OVERRIDING SYSTEM VALUE SELECT " + strings.Join(selected, ",") + " FROM jsonb_populate_recordset(NULL::" + name + ",$1::jsonb) e ON CONFLICT(" + strings.Join(qkeys, ",") + ") DO NOTHING"
	if _, err = target.Exec(ctx, query, data); err != nil {
		return err
	}
	matches, err := checkExactBulkData(ctx, target, name, keys, columns, data)
	if err != nil {
		return err
	}
	for _, match := range matches {
		if !match {
			return fmt.Errorf("legacy: historical %s row conflicts with target", table)
		}
	}
	return nil
}

// checkExactBulk is SELECT-only, including on a read-only target transaction.
// Its result has exactly one boolean per source row, in source order.
func checkExactBulk(ctx context.Context, target pgx.Tx, schema, table string, keys []string, rows []map[string]any) ([]bool, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	name, columns, data, err := exactBulkInput(schema, table, keys, rows)
	if err != nil {
		return nil, err
	}
	return checkExactBulkData(ctx, target, name, keys, columns, data)
}

func exactBulkInput(schema, table string, keys []string, rows []map[string]any) (string, []string, []byte, error) {
	if !strings.HasPrefix(schema, "v3_") || table == "" || len(keys) == 0 || len(rows) == 0 || len(rows) > exactBulkRows || len(rows[0]) == 0 {
		return "", nil, nil, fmt.Errorf("legacy: invalid exact bulk projection")
	}
	columns := make([]string, 0, len(rows[0]))
	for column := range rows[0] {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	encoded := make([]map[string]any, len(rows))
	for i, row := range rows {
		if len(row) != len(columns) {
			return "", nil, nil, fmt.Errorf("legacy: inconsistent exact bulk columns")
		}
		for _, key := range keys {
			if value, exists := row[key]; !exists || value == nil {
				return "", nil, nil, fmt.Errorf("legacy: missing exact bulk key %s", key)
			}
		}
		encoded[i] = make(map[string]any, len(row))
		for _, column := range columns {
			value, exists := row[column]
			if !exists {
				return "", nil, nil, fmt.Errorf("legacy: inconsistent exact bulk columns")
			}
			// RawMessage retains nested JSON and numeric precision. bytea needs
			// PostgreSQL hex input rather than encoding/json's base64 string.
			if binary, ok := value.([]byte); ok && binary != nil {
				value = "\\x" + hex.EncodeToString(binary)
			}
			encoded[i][column] = value
		}
	}
	data, err := json.Marshal(encoded)
	return pgx.Identifier{schema, table}.Sanitize(), columns, data, err
}

func checkExactBulkData(ctx context.Context, target pgx.Tx, name string, keys, columns []string, data []byte) ([]bool, error) {
	result, err := target.Query(ctx, exactBulkCheckQuery(name, keys, columns), data)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	matches := make([]bool, 0, exactBulkRows)
	for result.Next() {
		var match bool
		if err = result.Scan(&match); err != nil {
			return nil, err
		}
		matches = append(matches, match)
	}
	return matches, result.Err()
}

func exactBulkCheckQuery(name string, keys, columns []string) string {
	conditions := make([]string, 0, len(keys)+len(columns))
	// Equality on the immutable key supplies index conditions. Using only
	// IS NOT DISTINCT FROM here would scan the historical table per input row.
	for _, key := range keys {
		quoted := pgx.Identifier{key}.Sanitize()
		conditions = append(conditions, "h."+quoted+"=e."+quoted)
	}
	for _, column := range columns {
		quoted := pgx.Identifier{column}.Sanitize()
		conditions = append(conditions, "h."+quoted+" IS NOT DISTINCT FROM e."+quoted)
	}
	return "SELECT EXISTS(SELECT 1 FROM " + name + " h WHERE " + strings.Join(conditions, " AND ") + ") FROM jsonb_array_elements($1::jsonb) WITH ORDINALITY j(value,n) CROSS JOIN LATERAL jsonb_populate_record(NULL::" + name + ",j.value) e ORDER BY j.n"
}
