//go:build pgintegration

package legacy

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedRetiredHistoryFixture(t *testing.T, source *pgxpool.Pool) {
	t.Helper()
	_, err := source.Exec(context.Background(), `
	 INSERT INTO billing.accounts VALUES
	 ('retired-points','user',7,'gpt_wallet','points'),
	 ('retired-gpt','user',7,'wallet','quota');
	 INSERT INTO billing.ledger_entries(entry_id,account_id,entry_type,direction,amount,balance_after,idempotency_key) VALUES
	 ('retired-points-entry','retired-points','grant_credit','credit',9223372036854775807,9223372036854775807,'retired-points-key'),
	 ('retired-gpt-entry','retired-gpt','grant_credit','credit',9223372036854775807,9223372036854775807,'retired-gpt-key')`)
	if err != nil {
		t.Fatal(err)
	}
}

func assertRetiredHistoryReport(t *testing.T, r Report) {
	t.Helper()
	if len(r.Issues) != 0 || r.Counts["history.historical_accounts"] != 1 || r.Counts["history.ledger_entries"] != 2 ||
		r.Counts["retired_features.billing_history.accounts"] != 2 || r.Counts["retired_features.billing_history.ledger_entries"] != 2 ||
		r.Amounts["history.ledger_entries.micro_credits"] != "-60" ||
		r.Amounts["retired_features.billing_history.ledger_entries.amount_v2_units"] != "18446744073709551614" ||
		r.Amounts["retired_features.billing_history.ledger_entries.balance_after_v2_units"] != "18446744073709551614" {
		t.Fatalf("retired history must remain original-unit exclusion evidence: %+v", r)
	}
}

func TestHistoryRetiredBalancesExcludedBeforeMonetaryValidation(t *testing.T) {
	source, _, _ := importTestDB(t)
	historyFixture(t, source)
	seedRetiredHistoryFixture(t, source)
	ctx := context.Background()
	tx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := loadHistory(ctx, tx, sources)
	if err != nil {
		t.Fatal(err)
	}
	r := Report{}
	d.validate(&r)
	assertRetiredHistoryReport(t, r)
	var count int64
	if err := source.QueryRow(ctx, `SELECT count(*) FROM billing.ledger_entries WHERE entry_id LIKE 'retired-%' AND amount=9223372036854775807`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("retired source evidence changed: count=%d err=%v", count, err)
	}
}
