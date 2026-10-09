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
	return insertExactBulkData(ctx, target, name, table, keys, columns, data, len(rows))
}

func insertExactBulkData(ctx context.Context, target pgx.Tx, name, table string, keys, columns []string, data []byte, count int) error {
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
	if _, err := target.Exec(ctx, query, data); err != nil {
		return err
	}
	matches, err := checkExactBulkData(ctx, target, name, keys, columns, data)
	if err != nil {
		return err
	}
	if len(matches) != count {
		return fmt.Errorf("legacy: historical %s exact check returned wrong row count", table)
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
	name, columns, err := exactBulkShape(schema, table, keys, rows)
	if err != nil {
		return "", nil, nil, err
	}
	// Ordinary projections contain no bytea. Preserve their maps rather than
	// allocating and copying every field of every historical record.
	normalized := rows
	copied := false
	for i, row := range rows {
		for _, value := range row {
			if binary, ok := value.([]byte); ok && binary != nil {
				if !copied {
					normalized = append([]map[string]any(nil), rows...)
					copied = true
				}
				normalized[i] = exactBulkByteaRow(row)
				break
			}
		}
	}
	data, err := json.Marshal(normalized)
	return name, columns, data, err
}

func exactBulkShape(schema, table string, keys []string, rows []map[string]any) (string, []string, error) {
	if !strings.HasPrefix(schema, "v3_") || table == "" || len(keys) == 0 || len(rows) == 0 || len(rows) > exactBulkRows || len(rows[0]) == 0 {
		return "", nil, fmt.Errorf("legacy: invalid exact bulk projection")
	}
	columns := make([]string, 0, len(rows[0]))
	for column := range rows[0] {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	for _, row := range rows {
		if len(row) != len(columns) {
			return "", nil, fmt.Errorf("legacy: inconsistent exact bulk columns")
		}
		for _, key := range keys {
			if value, exists := row[key]; !exists || value == nil {
				return "", nil, fmt.Errorf("legacy: missing exact bulk key %s", key)
			}
		}
		for _, column := range columns {
			_, exists := row[column]
			if !exists {
				return "", nil, fmt.Errorf("legacy: inconsistent exact bulk columns")
			}
		}
	}
	return pgx.Identifier{schema, table}.Sanitize(), columns, nil
}

func exactBulkByteaRow(row map[string]any) map[string]any {
	normalized := make(map[string]any, len(row))
	for column, value := range row {
		// RawMessage is a distinct named type and retains exact nested JSON.
		if binary, ok := value.([]byte); ok && binary != nil {
			value = "\\x" + hex.EncodeToString(binary)
		}
		normalized[column] = value
	}
	return normalized
}

func exactBulkEncodeRow(row map[string]any) ([]byte, error) {
	for _, value := range row {
		if binary, ok := value.([]byte); ok && binary != nil {
			return json.Marshal(exactBulkByteaRow(row))
		}
	}
	return json.Marshal(row)
}

// Encoded rows are produced once by exactBulkEncodeRow. Joining them does not
// decode nested metadata or marshal their projections a second time.
func exactBulkEncodedInput(schema, table string, keys []string, rows []map[string]any, encoded [][]byte) (string, []string, []byte, error) {
	name, columns, err := exactBulkShape(schema, table, keys, rows)
	if err != nil {
		return "", nil, nil, err
	}
	if len(encoded) != len(rows) {
		return "", nil, nil, fmt.Errorf("legacy: inconsistent encoded exact bulk rows")
	}
	bytes := 2
	for _, row := range encoded {
		if len(row) == 0 {
			return "", nil, nil, fmt.Errorf("legacy: missing encoded exact bulk row")
		}
		bytes += len(row) + 1
	}
	data := make([]byte, 0, bytes)
	data = append(data, '[')
	for i, row := range encoded {
		if i > 0 {
			data = append(data, ',')
		}
		data = append(data, row...)
	}
	data = append(data, ']')
	return name, columns, data, nil
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
