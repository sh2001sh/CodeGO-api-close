//go:build pgintegration

package legacy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestFundingStreamedSQLValidation(t *testing.T) {
	for _, tc := range []struct {
		name, mutation, code string
	}{
		{"duplicate_id", `ALTER TABLE billing.funding_lots DROP CONSTRAINT funding_lots_pkey;
		 INSERT INTO billing.funding_lots SELECT * FROM billing.funding_lots WHERE lot_id='funding-box-lot'`, "duplicate_funding_id"},
		{"duplicate_idempotency", `INSERT INTO billing.funding_lots SELECT 'second-lot',account_id,source,idempotency_key,original_amount,remaining_amount,reference_type,reference_id,revenue_multiplier,created_at FROM billing.funding_lots WHERE lot_id='funding-box-lot'`, "duplicate_funding_idempotency"},
		{"duplicate_pair", `INSERT INTO billing.funding_allocations SELECT 'second-allocation',request_id,lot_id,account_id,source,amount,revenue_multiplier,created_at FROM billing.funding_allocations WHERE allocation_id='funding-box-allocation'`, "duplicate_request_lot"},
		{"missing_lot", `UPDATE billing.funding_allocations SET lot_id='missing' WHERE allocation_id='funding-box-allocation'`, "missing_funding_lot"},
		{"origin_mismatch", `UPDATE billing.funding_allocations SET source='other' WHERE allocation_id='funding-box-allocation'`, "funding_origin_mismatch"},
		{"rounded_rate_mismatch", `UPDATE billing.funding_allocations SET revenue_multiplier=0.6500005 WHERE allocation_id='funding-box-allocation'`, "funding_origin_mismatch"},
		{"same_rounded_rate", `UPDATE billing.funding_allocations SET revenue_multiplier=0.650000499999999999999 WHERE allocation_id='funding-box-allocation'`, ""},
		{"excess_consumption", `UPDATE billing.funding_allocations SET amount=41 WHERE allocation_id='funding-box-allocation'`, "funding_allocation_exceeds_consumed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, _, _ := importTestDB(t)
			seedFundingFixture(t, source)
			ctx := context.Background()
			if _, err := source.Exec(ctx, tc.mutation); err != nil {
				t.Fatal(err)
			}
			tx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			sources, err := discoverSources(ctx, tx)
			if err != nil {
				t.Fatal(err)
			}
			d, err := loadFunding(ctx, tx, sources)
			if err != nil || len(d.rows) != 0 {
				t.Fatalf("large tables must remain in source: rows=%v err=%v", d, err)
			}
			r := Report{}
			if err := d.validateContext(ctx, &r); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, issue := range r.Issues {
				found = found || issue.Code == tc.code
			}
			if tc.code == "" && len(r.Issues) != 0 || tc.code != "" && !found {
				t.Fatalf("wanted %q; issues=%+v", tc.code, r.Issues)
			}
		})
	}
}

type fundingCountTx struct {
	pgx.Tx
	accountLoads, perRowAccounts int
}

func (tx *fundingCountTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "SELECT id,owner_type,owner_id,kind FROM v3_billing.accounts") {
		tx.accountLoads++
	}
	return tx.Tx.Query(ctx, sql, args...)
}

func (tx *fundingCountTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "SELECT id FROM v3_billing.accounts WHERE") {
		tx.perRowAccounts++
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func TestFundingStreamedBatchesRetainExactAmounts(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedFundingFixture(t, source)
	ctx := context.Background()
	const count = exactBulkRows*2 + 7
	for _, sql := range []string{`INSERT INTO billing.funding_lots(lot_id,account_id,source,idempotency_key,original_amount,remaining_amount,reference_type,reference_id,revenue_multiplier,created_at)
	 SELECT 'bulk-lot-'||i,'wallet-7','other','bulk-credit-'||i,4611686018427387903,0,'','',0.0000005,'2026-09-29T09:00:00Z' FROM generate_series(1,$1::int) i`,
		`INSERT INTO billing.funding_allocations(allocation_id,request_id,lot_id,account_id,source,amount,revenue_multiplier,created_at)
	 SELECT 'bulk-allocation-'||i,'bulk-request-'||i,'bulk-lot-'||i,'wallet-7','other',4503599627370497,0.0000005,'2026-09-29T09:00:00Z' FROM generate_series(1,$1::int) i`} {
		if _, err := source.Exec(ctx, sql, count); err != nil {
			t.Fatal(err)
		}
	}
	importer := NewImporter(readonlySource(t, source), target, crypto)
	if r, err := importer.Import(ctx, true); err != nil || !r.Applied {
		t.Fatalf("bulk import: %+v %v", r, err)
	}
	var original, allocated, ppm int64
	if err := target.QueryRow(ctx, `SELECT l.original_amount,a.amount,l.revenue_multiplier_ppm FROM v3_billing.funding_lots l JOIN v3_billing.funding_allocations a USING(lot_id) WHERE l.lot_id='bulk-lot-1'`).Scan(&original, &allocated, &ppm); err != nil || original != 9223372036854775806 || allocated != 9007199254740994 || ppm != 1 {
		t.Fatalf("exact batch amounts %d %d ppm=%d err=%v", original, allocated, ppm, err)
	}
	sourceTx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sourceTx.Rollback(ctx) }()
	sources, err := discoverSources(ctx, sourceTx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := loadFunding(ctx, sourceTx, sources)
	if err != nil || len(d.rows) != 0 {
		t.Fatalf("streaming source must not retain funding rows: %+v %v", d, err)
	}
	for _, check := range []bool{false, true} {
		targetTx, err := target.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			t.Fatal(err)
		}
		counted := &fundingCountTx{Tx: targetTx}
		if check {
			r := Report{Counts: map[string]int64{}}
			err = importer.checkFunding(ctx, counted, d, &r)
			if len(r.Issues) != 0 || r.Counts["verified_billing_funding_allocations"] != count+1 {
				t.Fatalf("batch check: %+v", r)
			}
		} else {
			err = importer.importFunding(ctx, counted, d)
		}
		_ = targetTx.Rollback(ctx)
		if err != nil || counted.accountLoads != 1 || counted.perRowAccounts != 0 {
			t.Fatalf("check=%t native account queries loads=%d per-row=%d err=%v", check, counted.accountLoads, counted.perRowAccounts, err)
		}
	}
}

type fundingFailRow struct{ err error }

func (row fundingFailRow) Scan(...any) error { return row.err }

type fundingSQLFailTx struct {
	pgx.Tx
	err error
}

func (tx fundingSQLFailTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "SELECT EXISTS(") {
		return fundingFailRow{tx.err}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func TestFundingStreamingPropagatesSQLAndReadErrors(t *testing.T) {
	source, _, _ := importTestDB(t)
	seedFundingFixture(t, source)
	ctx := context.Background()
	tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("funding uniqueness SQL unavailable")
	d, err := loadFunding(ctx, fundingSQLFailTx{Tx: tx, err: want}, sources)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.validateContext(ctx, &Report{}); !errors.Is(err, want) {
		t.Fatalf("source SQL error swallowed: %v", err)
	}
	d.source = tx
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := d.validateContext(cancelled, &Report{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("source read cancellation swallowed: %v", err)
	}
}
