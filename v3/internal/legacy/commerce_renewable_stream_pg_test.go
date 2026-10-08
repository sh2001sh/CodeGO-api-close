//go:build pgintegration

package legacy

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type commerceFilteredTx struct {
	pgx.Tx
	table     string
	queries   int
	rows      int
	fail      error
	lastQuery string
}

func (tx *commerceFilteredTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, tx.table) {
		tx.queries++
		tx.lastQuery = sql
		if tx.fail != nil {
			return nil, tx.fail
		}
		rows, err := tx.Tx.Query(ctx, sql, args...)
		if err != nil {
			return nil, err
		}
		return &commerceFilteredRows{Rows: rows, tx: tx}, nil
	}
	return tx.Tx.Query(ctx, sql, args...)
}

type commerceFilteredRows struct {
	pgx.Rows
	tx *commerceFilteredTx
}

func (rows *commerceFilteredRows) Next() bool {
	if rows.Rows.Next() {
		rows.tx.rows++
		return true
	}
	return false
}

func TestCommerceRenewableStreamsOnlyDelayedCandidates(t *testing.T) {
	source, _, _ := importTestDB(t)
	ctx := context.Background()
	_, err := source.Exec(ctx, `CREATE TABLE billing.ledger_entries(account_id text,reason_code text,idempotency_key text,amount bigint,created_at timestamptz);
	 INSERT INTO billing.ledger_entries VALUES
	 ('sub9','subscription_bonus','group-buy:1:member:1:tier:101',101,'2023-11-14T22:13:20Z'),
	 ('sub9','subscription_bonus','group-buy:1:member:2:tier:37:member:2:',37,'2023-11-14T22:15:00.999999Z'),
	 ('sub9','subscription_bonus','group-buy:1:member:22:tier:99',99,'2023-11-14T22:13:20Z'),
	 ('sub9','subscription_bonus','group-buy:1:member:2:tier:38',38,'2023-11-14T22:15:01Z'),
	 ('sub9','subscription_bonus','group-buy:1:member:2:tier:10',10,'2023-11-14T22:13:19Z'),
	 ('foreign','subscription_bonus','group-buy:1:member:2:tier:99',99,'2023-11-14T22:13:20Z'),
	 ('sub10','subscription_bonus','group-buy:1:member:3:tier:99',99,'2023-11-14T22:13:30Z');
	 INSERT INTO billing.ledger_entries SELECT 'sub9','subscription_fuel','group-buy:1:member:2:tier:'||i,9223372036854775807,'2023-11-14T22:13:20Z' FROM generate_series(1,2000) i;`)
	if err != nil {
		t.Fatal(err)
	}
	subs := []commerceRow{
		commerceTestRow(t, `{"id":9,"user_id":7,"start_time":1700000000}`),
		commerceTestRow(t, `{"id":10,"user_id":7,"start_time":1700000020}`),
	}
	current := commerceTestRow(t, `{"id":1,"user_id":7,"user_subscription_id":9,"bonus_amount_usd":0.000201,"created_at":1700000000,"bonus_granted":true}`)
	members := []commerceRow{current,
		commerceTestRow(t, `{"id":2,"user_id":7,"user_subscription_id":9,"bonus_amount_usd":1,"created_at":1699999999,"bonus_granted":true}`),
		commerceTestRow(t, `{"id":3,"user_id":7,"user_subscription_id":10,"bonus_amount_usd":1,"created_at":1699999999,"bonus_granted":true}`),
	}
	accounts := []commerceRow{
		commerceTestRow(t, `{"account_id":"sub9","owner_id":9,"owner_type":"user_subscription","account_type":"subscription","quota_unit":"quota"}`),
		commerceTestRow(t, `{"account_id":"sub10","owner_id":10,"owner_type":"user_subscription","account_type":"subscription","quota_unit":"quota"}`),
	}
	cycle, err := commercePrepareCycleBonuses(subs, members, accounts, 1700000100, "500000")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	counted := &commerceFilteredTx{Tx: tx, table: "ledger_entries"}
	if err := cycle.walkDelayedGrants(ctx, counted, `billing.ledger_entries`, 1700000100); err != nil {
		t.Fatal(err)
	}
	if counted.queries != 1 || counted.rows != 4 || !strings.Contains(counted.lastQuery, "WHERE account_id=ANY") || cycle.bonuses[9].Int64() != 276 || cycle.bonuses[10] != nil {
		t.Fatalf("ledger filter/grant semantics: queries=%d rows=%d bonuses=%v SQL=%s", counted.queries, counted.rows, cycle.bonuses, counted.lastQuery)
	}
	want := errors.New("source ledger query failed")
	counted.fail = want
	if err := cycle.walkDelayedGrants(ctx, counted, `billing.ledger_entries`, 1700000100); !errors.Is(err, want) {
		t.Fatalf("ledger query failure swallowed: %v", err)
	}
	currentOnly, err := commercePrepareCycleBonuses(subs, []commerceRow{current}, accounts, 1700000100, "500000")
	if err != nil {
		t.Fatal(err)
	}
	priorQueries := counted.queries
	if err := currentOnly.walkDelayedGrants(ctx, counted, `billing.ledger_entries`, 1700000100); err != nil || counted.queries != priorQueries {
		t.Fatalf("no delayed markers must not query ledger: %v queries=%d/%d", err, counted.queries, priorQueries)
	}
}

func TestCommerceRefundOriginsStreamOnlyPaidLotsAndRejectBadAttribution(t *testing.T) {
	source, _, _ := importTestDB(t)
	seedCommerceFixture(t, source)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `INSERT INTO billing.funding_lots SELECT 'unrelated-'||i,'foreign','other','unrelated-'||i,9223372036854775807,9223372036854775807,'','',0,'2023-11-14T22:13:20Z' FROM generate_series(1,2000) i`); err != nil {
		t.Fatal(err)
	}
	read := func(fail error) (*commerceData, int, error) {
		tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
		if err != nil {
			return nil, 0, err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		sources, err := discoverSources(ctx, tx)
		if err != nil {
			return nil, 0, err
		}
		counted := &commerceFilteredTx{Tx: tx, table: "funding_lots", fail: fail}
		d, err := loadCommerce(ctx, counted, sources)
		return d, counted.rows, err
	}
	d, rows, err := read(nil)
	if err != nil || rows != 1 || len(d.refundOrigins) != 1 || d.refundOrigins[0].Original != 2000000 || d.refundOrigins[0].Remaining != 800 {
		t.Fatalf("only paid origins should cross source stream: rows=%d data=%+v err=%v", rows, d, err)
	}
	want := errors.New("paid funding source unavailable")
	if _, _, err := read(want); !errors.Is(err, want) {
		t.Fatalf("paid origin source failure swallowed: %v", err)
	}
	for _, tc := range []struct{ mutation, detail string }{
		{`UPDATE billing.funding_lots SET account_id='foreign' WHERE lot_id='old-paid-lot'`, "owner differs"},
		{`UPDATE billing.funding_lots SET account_id='wallet-7',idempotency_key='topup:absent:unified' WHERE lot_id='old-paid-lot'`, "missing topup order"},
		{`UPDATE billing.funding_lots SET idempotency_key='topup:legacy-topup-1:unified',original_amount=4611686018427387904 WHERE lot_id='old-paid-lot'`, "overflow"},
	} {
		if _, err := source.Exec(ctx, tc.mutation); err != nil {
			t.Fatal(err)
		}
		if _, _, err := read(nil); err == nil || !strings.Contains(err.Error(), tc.detail) {
			t.Fatalf("paid origin restriction %q changed: %v", tc.detail, err)
		}
	}
}

func TestCommerceReservationEvidenceAggregatesOnlyRelevantRequests(t *testing.T) {
	source, _, _ := importTestDB(t)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `CREATE TABLE billing.reservations(request_id text,status text);
	 INSERT INTO billing.reservations VALUES('mixed','settled'),('mixed','open'),('null-status',NULL),('other','open');
	 INSERT INTO billing.reservations SELECT 'done','settled' FROM generate_series(1,2000);
	 INSERT INTO billing.reservations SELECT 'unrelated-'||i,'open' FROM generate_series(1,2000) i;`); err != nil {
		t.Fatal(err)
	}
	d := commerceTestData(t)
	for i, request := range []string{"done", "mixed", "null-status", "missing", "done"} {
		d.rows["subscription_pre_consume_records"] = append(d.rows["subscription_pre_consume_records"], commerceTestRow(t,
			`{"id":`+big.NewInt(int64(i+1)).String()+`,"user_id":7,"user_subscription_id":9,"request_id":"`+request+`","status":"consumed","pre_consumed":10}`))
	}
	completed := commerceTestRow(t, `{"id":6,"user_id":7,"user_subscription_id":9,"request_id":"other","status":"completed","pre_consumed":10}`)
	d.rows["subscription_pre_consume_records"] = append(d.rows["subscription_pre_consume_records"], completed)
	tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	counted := &commerceFilteredTx{Tx: tx, table: "reservations"}
	if err := d.loadReservationStates(ctx, counted, `billing.reservations`); err != nil {
		t.Fatal(err)
	}
	if counted.queries != 1 || counted.rows != 3 || len(d.reservationStates) != 3 || len(d.consumedSubscriptionRequests()) != 4 {
		t.Fatalf("reservation source retained unrelated or duplicate states: rows=%d queries=%d map=%v", counted.rows, counted.queries, d.reservationStates)
	}
	for i, row := range d.rows["subscription_pre_consume_records"] {
		_, err := d.project("subscription_pre_consume_records", row)
		if (err != nil) != (i == 1 || i == 2 || i == 3) {
			t.Fatalf("settlement evidence changed for row %d: %v", i, err)
		}
	}
	d.completedRequests = map[string]int64{"missing": 7}
	if _, err := d.project("subscription_pre_consume_records", d.rows["subscription_pre_consume_records"][3]); err != nil {
		t.Fatalf("absent reservation must still allow own durable consume log: %v", err)
	}
	want := errors.New("reservation aggregation failed")
	counted.fail = want
	if err := d.loadReservationStates(ctx, counted, `billing.reservations`); !errors.Is(err, want) {
		t.Fatalf("reservation source failure swallowed: %v", err)
	}
}

func TestCommerceCompletionUsesIndexedTypedLogsAndRejectsConflicts(t *testing.T) {
	source, _, _ := importTestDB(t)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `CREATE TABLE migration_source.logs(request_id text,user_id bigint,type int);
	 CREATE INDEX renewal_logs_request_type_idx ON migration_source.logs(request_id,type);
	 INSERT INTO migration_source.logs SELECT 'unrelated-'||i,7,2 FROM generate_series(1,10000) i;
	 INSERT INTO migration_source.logs VALUES('done',7,2),('done',7,2),('conflict',7,2),('conflict',8,2),('conflict',7,2),('wrong-type',7,1),('large-owner',9007199254740993,2);
	 ANALYZE migration_source.logs;`); err != nil {
		t.Fatal(err)
	}
	d := commerceTestData(t)
	for _, request := range []string{"done", "done", "conflict", "wrong-type", "large-owner"} {
		d.rows["subscription_pre_consume_records"] = append(d.rows["subscription_pre_consume_records"], commerceTestRow(t,
			`{"request_id":"`+request+`","status":"consumed"}`))
	}
	tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	counted := &commerceFilteredTx{Tx: tx, table: "logs"}
	if err := d.loadCompletedRequests(ctx, counted, map[string]string{"logs": "migration_source.logs"}); err != nil {
		t.Fatal(err)
	}
	if counted.queries != 1 || counted.rows != 6 || strings.Contains(counted.lastQuery, "to_jsonb") || d.completedRequests["done"] != 7 || d.completedRequests["conflict"] != -1 || d.completedRequests["wrong-type"] != 0 || d.completedRequests["large-owner"] != 9007199254740993 {
		t.Fatalf("typed completion query lost owner/conflict evidence: rows=%d map=%v SQL=%s", counted.rows, d.completedRequests, counted.lastQuery)
	}
	var plan []byte
	if err := tx.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+counted.lastQuery, d.consumedSubscriptionRequests()).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plan), "renewal_logs_request_type_idx") {
		t.Fatalf("completion predicates prevented existing request index use: %s", plan)
	}
	want := errors.New("consume log source unavailable")
	counted.fail = want
	if err := d.loadCompletedRequests(ctx, counted, map[string]string{"logs": "migration_source.logs"}); !errors.Is(err, want) {
		t.Fatalf("consume log SQL error swallowed: %v", err)
	}
}
