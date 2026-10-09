package legacy

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Each history table keeps at most one bounded batch while the source cursor
// remains streaming. Flushing does not commit the enclosing import transaction.
type historyBatch struct {
	rows         []map[string]any
	bytes        int
	flush        func([]map[string]any) error
	encoded      [][]byte
	flushEncoded func([]map[string]any, [][]byte) error
}

func (b *historyBatch) add(row map[string]any) error {
	encoded, err := exactBulkEncodeRow(row)
	if err != nil {
		return err
	}
	if len(b.rows) > 0 && b.bytes+len(encoded)+1 > exactBulkBytes {
		if err := b.finish(); err != nil {
			return err
		}
	}
	if len(b.rows) == 0 {
		b.bytes = 2
	} else {
		b.bytes++
	}
	b.rows = append(b.rows, row)
	if b.flushEncoded != nil {
		b.encoded = append(b.encoded, encoded)
	}
	b.bytes += len(encoded)
	// A source row larger than the cap is processed by itself, never grouped
	// with another large row. Buffer size does not grow with history volume.
	if len(b.rows) == exactBulkRows || b.bytes >= exactBulkBytes {
		return b.finish()
	}
	return nil
}

func (b *historyBatch) finish() error {
	if len(b.rows) == 0 {
		return nil
	}
	var err error
	if b.flushEncoded != nil {
		err = b.flushEncoded(b.rows, b.encoded)
	} else {
		err = b.flush(b.rows)
	}
	if err != nil {
		return err
	}
	clear(b.rows)
	b.rows = b.rows[:0]
	clear(b.encoded)
	b.encoded = b.encoded[:0]
	b.bytes = 0
	return nil
}

func historyImportBatch(ctx context.Context, target pgx.Tx, schema, table string, keys ...string) *historyBatch {
	return &historyBatch{flushEncoded: func(rows []map[string]any, encoded [][]byte) error {
		name, columns, data, err := exactBulkEncodedInput(schema, table, keys, rows, encoded)
		if err != nil {
			return err
		}
		return insertExactBulkData(ctx, target, name, table, keys, columns, data, len(rows))
	}}
}

func historyFields(columns []string, values []any) map[string]any {
	fields := make(map[string]any, len(columns))
	for i, column := range columns {
		fields[column] = values[i]
	}
	return fields
}

type historyAccountKey struct {
	owner string
	id    int64
	kind  string
}

// Account mappings are reused across every row of the large ledger/log tables.
func historyAccountTargets(ctx context.Context, target pgx.Tx) (map[historyAccountKey]int64, error) {
	rows, err := target.Query(ctx, `SELECT owner_type,owner_id,kind,id FROM v3_billing.accounts
	 WHERE (owner_type,kind) IN (('user','wallet'),('api_key','key_budget'),('subscription','subscription'),
	 ('user','marketplace_pending'),('user','marketplace_earned'),('platform','platform_revenue'))`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := map[historyAccountKey]int64{}
	for rows.Next() {
		var key historyAccountKey
		var id int64
		if err = rows.Scan(&key.owner, &key.id, &key.kind, &id); err != nil {
			return nil, err
		}
		accounts[key] = id
	}
	return accounts, rows.Err()
}
