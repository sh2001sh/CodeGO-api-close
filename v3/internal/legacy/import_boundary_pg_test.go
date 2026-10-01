//go:build pgintegration

package legacy

import (
	"context"
	"math"
	"testing"
)

func TestSeparateSourceImportBigintBoundary(t *testing.T) {
	source, target, crypto := importTestDB(t)
	ctx := context.Background()
	units := int64(math.MaxInt64 / 2)
	for _, sql := range []string{`UPDATE migration_source.users SET claude_quota=$1`, `UPDATE billing.balance_snapshots SET available_balance=$1`} {
		if _, err := source.Exec(ctx, sql, units); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewImporter(source, target, crypto).Import(ctx, true); err != nil {
		t.Fatal(err)
	}
	var actual int64
	if err := target.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet'`).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != math.MaxInt64-1 {
		t.Fatalf("boundary wallet rounded or overflowed: %d", actual)
	}
}

func TestSeparateSourceImportRejectsOverflowWithoutWrites(t *testing.T) {
	source, target, crypto := importTestDB(t)
	ctx := context.Background()
	units := int64(math.MaxInt64/2 + 1)
	for _, sql := range []string{`UPDATE migration_source.users SET claude_quota=$1`, `UPDATE billing.balance_snapshots SET available_balance=$1`} {
		if _, err := source.Exec(ctx, sql, units); err != nil {
			t.Fatal(err)
		}
	}
	report, err := NewImporter(source, target, crypto).Import(ctx, true)
	if err == nil || report.Applied {
		t.Fatal("overflowing wallet imported")
	}
	var count int
	if queryErr := target.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users`).Scan(&count); queryErr != nil {
		t.Fatal(queryErr)
	}
	if count != 0 {
		t.Fatal("overflow rejection left partial rows")
	}
}
