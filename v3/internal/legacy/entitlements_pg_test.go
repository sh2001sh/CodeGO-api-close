//go:build pgintegration

package legacy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedEntitlementsFixture composes with both commerce and marketplace fixtures.
// It covers every entitlement table including the overloaded reset relation.
func seedEntitlementsFixture(t *testing.T, source *pgxpool.Pool) {
	t.Helper()
	d := entitlementFixture(t)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `CREATE SCHEMA entitlements_fixture`); err != nil {
		t.Fatal(err)
	}
	for _, contract := range entitlementContracts {
		defs, columns, holders := []string{}, []string{}, []string{}
		for _, spec := range strings.Fields(contract.spec) {
			parts := strings.Split(spec, ":")
			column, kind := parts[1], parts[2]
			sqlType := "bigint"
			switch kind {
			case "n":
				sqlType = "numeric"
			case "s":
				sqlType = "text"
			}
			columns = append(columns, column)
			defs = append(defs, pgx.Identifier{column}.Sanitize()+" "+sqlType)
			holders = append(holders, fmt.Sprintf("$%d::text::%s", len(columns), sqlType))
		}
		table := pgx.Identifier{"entitlements_fixture", contract.source}.Sanitize()
		if _, err := source.Exec(ctx, "CREATE TABLE "+table+" ("+strings.Join(defs, ",")+")"); err != nil {
			t.Fatal(err)
		}
		for _, row := range d.rows[contract.source] {
			values := make([]any, len(columns))
			for i, column := range columns {
				if len(row[column]) > 0 {
					text := string(row[column])
					if row[column][0] == '"' {
						text, _ = row.text(column)
					}
					values[i] = text
				}
			}
			if _, err := source.Exec(ctx, "INSERT INTO "+table+" VALUES("+strings.Join(holders, ",")+")", values...); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestEntitlementsIndependentReadonlyImportReplayAndCheck(t *testing.T) {
	source, target, crypto := importTestDB(t)
	var hasConfirmedRuntime bool
	if err := target.QueryRow(context.Background(), `SELECT to_regclass('v3_commerce.subscription_lucky_rewards') IS NOT NULL`).Scan(&hasConfirmedRuntime); err != nil {
		t.Fatal(err)
	}
	if !hasConfirmedRuntime {
		t.Skip("lucky/referral/reset runtime scope awaits user confirmation; native table proposal is withdrawn")
	}
	seedCommerceFixture(t, source)
	seedMarketplaceFixture(t, source)
	seedEntitlementsFixture(t, source)
	reader := readonlySource(t, source)
	ctx := context.Background()
	snapshot, err := reader.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snapshot.Rollback(ctx) }()
	sources, err := discoverSources(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	data, err := loadEntitlements(ctx, snapshot, sources)
	if err != nil {
		t.Fatal(err)
	}
	preview := Report{}
	data.validate(&preview)
	if len(preview.Issues) != 0 {
		t.Fatalf("preview invalid: %+v", preview)
	}
	var before int
	if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscription_lucky_rewards`).Scan(&before); err != nil || before != 0 {
		t.Fatalf("preview target count=%d err=%v", before, err)
	}
	users, err := loadUsers(ctx, snapshot, sources)
	if err != nil {
		t.Fatal(err)
	}
	commerce, err := loadCommerce(ctx, snapshot, sources)
	if err != nil {
		t.Fatal(err)
	}
	market, err := loadMarketplace(ctx, snapshot, sources)
	if err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(reader, target, crypto)
	for i := 0; i < 2; i++ {
		err = pgx.BeginFunc(ctx, target, func(targetTx pgx.Tx) error {
			if err := importer.importUsers(ctx, targetTx, users); err != nil {
				return err
			}
			if err := importer.importCommerce(ctx, targetTx, commerce); err != nil {
				return err
			}
			if err := importer.importMarketplace(ctx, targetTx, market); err != nil {
				return err
			}
			return importer.importEntitlements(ctx, targetTx, data)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var reward, balance int64
	var exactDecimal string
	if err = target.QueryRow(ctx, `SELECT final_reward_credits FROM v3_commerce.subscription_lucky_rewards WHERE id=104`).Scan(&reward); err != nil || reward != 246913578 {
		t.Fatalf("reward=%d %v", reward, err)
	}
	if err = target.QueryRow(ctx, `SELECT base_reward_1_usd::text FROM v3_commerce.subscription_lucky_draws WHERE id=103`).Scan(&exactDecimal); err != nil || exactDecimal != "0.123456789123456789" {
		t.Fatalf("USD precision=%s %v", exactDecimal, err)
	}
	if err = target.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=8 AND kind='wallet'`).Scan(&balance); err != nil || balance != 1500 {
		t.Fatalf("historical rewards recredited wallet: %d %v", balance, err)
	}
	check := func() Report {
		t.Helper()
		r := Report{}
		if err := pgx.BeginTxFunc(ctx, target, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(readTx pgx.Tx) error {
			return importer.checkEntitlements(ctx, readTx, data, &r)
		}); err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := check(); len(r.Issues) != 0 || r.Counts["check:entitlements"] != 11 {
		t.Fatalf("post-import check=%+v", r)
	}
	if _, err = target.Exec(ctx, `UPDATE v3_commerce.subscription_lucky_rewards SET final_reward_credits=final_reward_credits+1 WHERE id=104`); err != nil {
		t.Fatal(err)
	}
	if r := check(); len(r.Issues) != 1 {
		t.Fatalf("changed money was not detected: %+v", r)
	}
	if err = pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error { return importer.importEntitlements(ctx, tx, data) }); err == nil {
		t.Fatal("changed target history silently replaced")
	}
}
