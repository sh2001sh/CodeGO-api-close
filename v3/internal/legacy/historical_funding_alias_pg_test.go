//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestHistoricalReclaimedZeroImportsAndChecksWithoutMoneyReplayPG(t *testing.T) {
	source, target, crypto := importTestDB(t)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `INSERT INTO migration_source.users VALUES(8,'consumer','bcrypt-placeholder',1,1,'default',0,0,'{}')`); err != nil {
		t.Fatal(err)
	}
	seedChannelMarketFixture(t, source)
	if _, err := source.Exec(ctx, `UPDATE marketplace.settlements SET status='reclaimed',reclaimed_amount=0;
	 UPDATE billing.balance_snapshots SET available_balance=0 WHERE account_id='fixture-market-pending-7'`); err != nil {
		t.Fatal(err)
	}
	m := NewImporter(readonlySource(t, source), target, crypto)
	for range 2 {
		if report, err := m.Import(ctx, true); err != nil || !report.Applied {
			t.Fatalf("import failed issues=%d err=%v", len(report.Issues), err)
		}
		if report, err := m.Check(ctx); err != nil || len(report.Issues) != 0 {
			t.Fatalf("check failed issues=%d err=%v", len(report.Issues), err)
		}
	}
	var reclaimed, net, wallet, entries, sourceRaw int64
	err := target.QueryRow(ctx, `SELECT reclaimed_micro,net_micro,
	 (SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet'),
	 (SELECT count(*) FROM v3_billing.ledger_entries) FROM v3_channelmarket.settlements WHERE id='settlement-201'`).Scan(&reclaimed, &net, &wallet, &entries)
	if err != nil || reclaimed != 190 || net != 190 || wallet != 1000 || entries != 2 {
		t.Fatalf("unexpected native amounts reclaimed=%d net=%d wallet=%d entries=%d err=%v", reclaimed, net, wallet, entries, err)
	}
	if err = source.QueryRow(ctx, `SELECT reclaimed_amount FROM marketplace.settlements WHERE id='settlement-201'`).Scan(&sourceRaw); err != nil || sourceRaw != 0 {
		t.Fatal("source raw reclaimed amount changed")
	}
	if _, err = target.Exec(ctx, `UPDATE v3_channelmarket.settlements SET reclaimed_micro=0 WHERE id='settlement-201'`); err != nil {
		t.Fatal(err)
	}
	if report, err := m.Check(ctx); err == nil || len(report.Issues) == 0 {
		t.Fatal("native reclaimed amount tampering escaped check")
	}
}

func TestHistoricalReclaimedInvalidAmountsRefuseAtomicImportPG(t *testing.T) {
	for _, mutation := range []string{
		`UPDATE marketplace.settlements SET status='reclaimed',reclaimed_amount=1`,
		`ALTER TABLE marketplace.settlements ALTER COLUMN reclaimed_amount TYPE numeric; UPDATE marketplace.settlements SET status='reclaimed',reclaimed_amount=0.5`,
		`UPDATE marketplace.settlements SET status='reclaimed',reclaimed_amount=0,owner_net_amount=4611686018427387904,settlement_gross_amount=4611686018427387909`,
	} {
		t.Run(mutation, func(t *testing.T) {
			source, target, crypto := importTestDB(t)
			ctx := context.Background()
			if _, err := source.Exec(ctx, `INSERT INTO migration_source.users VALUES(8,'consumer','bcrypt-placeholder',1,1,'default',0,0,'{}')`); err != nil {
				t.Fatal(err)
			}
			seedChannelMarketFixture(t, source)
			if _, err := source.Exec(ctx, mutation); err != nil {
				t.Fatal(err)
			}
			m := NewImporter(readonlySource(t, source), target, crypto)
			if report, err := m.Import(ctx, true); err == nil || report.Applied {
				t.Fatal("invalid reclaimed source accepted")
			}
			var users int64
			if err := target.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users`).Scan(&users); err != nil || users != 0 {
				t.Fatal("rejected source partially wrote target")
			}
		})
	}
}

func TestHistoricalClaudeEconomicsProjectionAndExactCheckPG(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedFundingFixture(t, source)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `UPDATE billing.request_economics SET billing_source='claude_wallet';
	 INSERT INTO billing.request_economics VALUES('large-exact',13,0,9007199254740993,'claude_wallet',0,0.123456,0.654321,'2026-08-01','2026-08-01')`); err != nil {
		t.Fatal(err)
	}
	var actualRows []json.RawMessage
	if fixture := os.Getenv("V3_MIGRATION_CLAUDE_ECONOMICS_FIXTURE"); fixture != "" {
		raw, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatal("cannot read private economics fixture")
		}
		if err = json.Unmarshal(raw, &actualRows); err != nil {
			t.Fatal("invalid private economics fixture")
		}
		for _, row := range actualRows {
			if _, err = source.Exec(ctx, `INSERT INTO billing.request_economics SELECT * FROM jsonb_populate_record(NULL::billing.request_economics,$1::jsonb)`, row); err != nil {
				t.Fatal("could not load private economics row")
			}
		}
		t.Logf("private actual source economics rows verified=%d", len(actualRows))
	}
	m := NewImporter(readonlySource(t, source), target, crypto)
	for range 2 {
		if report, err := m.Import(ctx, true); err != nil || !report.Applied {
			t.Fatalf("import failed issues=%d err=%v", len(report.Issues), err)
		}
		if report, err := m.Check(ctx); err != nil || len(report.Issues) != 0 {
			t.Fatalf("economics check failed issues=%d err=%v", len(report.Issues), err)
		}
	}
	var amount, cost, revenue, wallet, entries, rawCount int64
	var label string
	err := target.QueryRow(ctx, `SELECT actual_amount,billing_source,procurement_cost_multiplier_ppm,revenue_multiplier_ppm,
	 (SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet'),
	 (SELECT count(*) FROM v3_billing.ledger_entries) FROM v3_billing.request_economics WHERE request_id='large-exact'`).Scan(&amount, &label, &cost, &revenue, &wallet, &entries)
	if err != nil || amount != 18014398509481986 || label != "wallet" || cost != 123456 || revenue != 654321 || wallet != 1000 || entries != 1 {
		t.Fatalf("economic money/precision changed amount=%d label=%s cost=%d revenue=%d wallet=%d entries=%d err=%v", amount, label, cost, revenue, wallet, entries, err)
	}
	if err = source.QueryRow(ctx, `SELECT count(*) FROM billing.request_economics WHERE billing_source='claude_wallet'`).Scan(&rawCount); err != nil || rawCount != int64(2+len(actualRows)) {
		t.Fatal("source raw economics rewritten")
	}
	if _, err = target.Exec(ctx, `UPDATE v3_billing.request_economics SET billing_source='subscription' WHERE request_id='large-exact'`); err != nil {
		t.Fatal(err)
	}
	if report, err := m.Check(ctx); err == nil || len(report.Issues) == 0 {
		t.Fatal("changed normalized source escaped exact check")
	}
	if _, err = target.Exec(ctx, `UPDATE v3_billing.request_economics SET billing_source='wallet',actual_amount=actual_amount-1 WHERE request_id='large-exact'`); err != nil {
		t.Fatal(err)
	}
	if report, err := m.Check(ctx); err == nil || len(report.Issues) == 0 {
		t.Fatal("one-micro monetary drift escaped exact check")
	}
}

func TestHistoricalClaudeEconomicsUnknownAndOverflowBlockAtomicImportPG(t *testing.T) {
	for _, mutation := range []string{
		`UPDATE billing.request_economics SET billing_source='unknown'`,
		`UPDATE billing.request_economics SET billing_source='claude_wallet',actual_amount=4611686018427387904`,
		`ALTER TABLE billing.request_economics ALTER COLUMN actual_amount TYPE numeric; UPDATE billing.request_economics SET billing_source='claude_wallet',actual_amount=0.5`,
	} {
		t.Run(mutation, func(t *testing.T) {
			source, target, crypto := importTestDB(t)
			seedFundingFixture(t, source)
			ctx := context.Background()
			if _, err := source.Exec(ctx, mutation); err != nil {
				t.Fatal(err)
			}
			m := NewImporter(readonlySource(t, source), target, crypto)
			if report, err := m.Import(ctx, true); err == nil || report.Applied {
				t.Fatal("unknown/overflow economics accepted")
			}
			var users int64
			if err := target.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users`).Scan(&users); err != nil || users != 0 {
				t.Fatal("economics rejection wrote partial target")
			}
			tx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if _, err = tx.Exec(ctx, `UPDATE billing.request_economics SET billing_source='wallet'`); err == nil {
				t.Fatal("source read-only contract bypassed")
			}
		})
	}
}
