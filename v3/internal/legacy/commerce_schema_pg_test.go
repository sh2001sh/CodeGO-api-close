//go:build pgintegration

package legacy

import (
	"context"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Check the entire retained source projection against actual embedded target
// migrations, so one missing column cannot hide later contract failures.
func TestCommerceRetainedProjectionFitsNativeSchema(t *testing.T) {
	source, target, _ := importTestDB(t)
	seedCommerceFixture(t, source)
	ctx := context.Background()
	tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tables, err := discoverSources(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	data, err := loadCommerce(ctx, tx, tables)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range commerceSourceNames {
		if len(data.rows[name]) == 0 {
			continue
		}
		row, err := data.project(name, data.rows[name][0])
		if err != nil {
			t.Fatal(err)
		}
		rows, err := target.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema='v3_commerce' AND table_name=$1`, row.table)
		if err != nil {
			t.Fatal(err)
		}
		columns := map[string]bool{}
		for rows.Next() {
			var column string
			if err = rows.Scan(&column); err != nil {
				t.Fatal(err)
			}
			columns[column] = true
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		var missing []string
		for field := range row.values {
			if !columns[field] {
				missing = append(missing, field)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("native %s is missing retained fields: %v", row.table, missing)
		}
	}
}
