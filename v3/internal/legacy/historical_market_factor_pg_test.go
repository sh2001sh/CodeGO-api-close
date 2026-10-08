//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"testing"
)

func TestHistoricalMarketSubscriptionFactorImportAndExactCheckPG(t *testing.T) {
	source, target, crypto := importTestDB(t)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `INSERT INTO migration_source.users VALUES(8,'consumer','bcrypt-placeholder',1,1,'default',0,0,'{}')`); err != nil {
		t.Fatal(err)
	}
	seedChannelMarketFixture(t, source)
	if _, err := source.Exec(ctx, `UPDATE marketplace.settlements SET multiplier=0.17,subscription_multiplier=1.7000000000000002`); err != nil {
		t.Fatal(err)
	}
	if fixture := os.Getenv("V3_MIGRATION_MARKET_FACTOR_FIXTURE"); fixture != "" {
		raw, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatal("cannot read private multiplier fixture")
		}
		var actual [][2]string
		if err = json.Unmarshal(raw, &actual); err != nil {
			t.Fatal("invalid private multiplier fixture")
		}
		for _, pair := range actual {
			wallet, _ := json.Marshal(pair[0])
			subscription, _ := json.Marshal(pair[1])
			row := cmRow{"multiplier": wallet, "subscription_multiplier": subscription}
			b := cmBuild()
			b.settlementFactors(row)
			if b.err != nil {
				t.Fatal("private factor cannot project exactly")
			}
		}
		t.Logf("private actual source factor rows verified=%d", len(actual))
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
	var walletFactor, subFactor, gross, net, wallet int64
	err := target.QueryRow(ctx, `SELECT multiplier_ppm,subscription_multiplier_ppm,gross_micro,net_micro,
	 (SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet')
	 FROM v3_channelmarket.settlements WHERE id='settlement-201'`).Scan(&walletFactor, &subFactor, &gross, &net, &wallet)
	if err != nil || walletFactor != 170000 || subFactor != 1700000 || gross != 200 || net != 190 || wallet != 1000 {
		t.Fatalf("native factor or money changed wallet_factor=%d sub_factor=%d gross=%d net=%d wallet=%d err=%v", walletFactor, subFactor, gross, net, wallet, err)
	}
	var original string
	if err = source.QueryRow(ctx, `SELECT subscription_multiplier::text FROM marketplace.settlements WHERE id='settlement-201'`).Scan(&original); err != nil || original != "1.7000000000000002" {
		t.Fatal("source multiplier rewritten")
	}
	if _, err = target.Exec(ctx, `UPDATE v3_channelmarket.settlements SET subscription_multiplier_ppm=subscription_multiplier_ppm-1 WHERE id='settlement-201'`); err != nil {
		t.Fatal(err)
	}
	if report, err := m.Check(ctx); err == nil || len(report.Issues) == 0 {
		t.Fatal("normalized factor tampering escaped check")
	}
}

func TestHistoricalMarketFractionalWalletFactorImportAndExactCheckPG(t *testing.T) {
	source, target, crypto := importTestDB(t)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `INSERT INTO migration_source.users VALUES(8,'consumer','bcrypt-placeholder',1,1,'default',0,0,'{}')`); err != nil {
		t.Fatal(err)
	}
	seedChannelMarketFixture(t, source)
	if _, err := source.Exec(ctx, `UPDATE marketplace.settlements SET multiplier=0.13114514191981,subscription_multiplier=1.3115`); err != nil {
		t.Fatal(err)
	}
	if fixture := os.Getenv("V3_MIGRATION_MARKET_MAIN_FACTOR_FIXTURE"); fixture != "" {
		raw, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatal("cannot read private exact multiplier fixture")
		}
		var actual []cmRow
		if err = json.Unmarshal(raw, &actual); err != nil {
			t.Fatal("invalid private exact multiplier fixture")
		}
		for _, row := range actual {
			b := cmBuild()
			b.settlementFactors(row)
			original, valid := new(big.Rat).SetString(row.text("multiplier"))
			projected, projectedValid := new(big.Rat).SetString(string(b.values["multiplier_ppm"].(json.Number)))
			if b.err != nil || !valid || !projectedValid || original.Mul(original, big.NewRat(1000000, 1)).Cmp(projected) != 0 {
				t.Fatal("private decimal factor projection lost precision")
			}
		}
		t.Logf("private actual source exact multiplier rows verified=%d", len(actual))
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
	var exact, original string
	var subFactor, gross, net, wallet int64
	err := target.QueryRow(ctx, `SELECT multiplier_ppm::text,subscription_multiplier_ppm,gross_micro,net_micro,
	 (SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet')
	 FROM v3_channelmarket.settlements WHERE id='settlement-201'`).Scan(&exact, &subFactor, &gross, &net, &wallet)
	if err != nil || exact != "131145.14191981" || subFactor != 1311500 || gross != 200 || net != 190 || wallet != 1000 {
		t.Fatalf("exact factor or financial totals changed exact=%s sub=%d gross=%d net=%d wallet=%d err=%v", exact, subFactor, gross, net, wallet, err)
	}
	if err = source.QueryRow(ctx, `SELECT multiplier::text FROM marketplace.settlements WHERE id='settlement-201'`).Scan(&original); err != nil || original != "0.13114514191981" {
		t.Fatal("source exact multiplier rewritten")
	}
	for _, bad := range []string{"'NaN'::numeric", "'Infinity'::numeric", "'-Infinity'::numeric", "-0.1", "9223372036854775807.1"} {
		if _, err = target.Exec(ctx, `UPDATE v3_channelmarket.settlements SET multiplier_ppm=`+bad+` WHERE id='settlement-201'`); err == nil {
			t.Fatal("native exact factor accepted invalid range")
		}
	}
	if _, err = target.Exec(ctx, `UPDATE v3_channelmarket.settlements SET multiplier_ppm=multiplier_ppm+0.00000001 WHERE id='settlement-201'`); err != nil {
		t.Fatal(err)
	}
	if report, err := m.Check(ctx); err == nil || len(report.Issues) == 0 {
		t.Fatal("fractional PPM metadata tampering escaped exact check")
	}
}

func TestHistoricalMarketSubscriptionArbitraryResidueRefusesAtomicImportPG(t *testing.T) {
	for _, values := range []string{
		`multiplier=0.17,subscription_multiplier=1.7000000000000003`,
		`multiplier=0.17,subscription_multiplier=0.8999999999999999`,
		`multiplier=9223372036854.7758071,subscription_multiplier=0`,
	} {
		t.Run(values, func(t *testing.T) {
			source, target, crypto := importTestDB(t)
			ctx := context.Background()
			if _, err := source.Exec(ctx, `INSERT INTO migration_source.users VALUES(8,'consumer','bcrypt-placeholder',1,1,'default',0,0,'{}')`); err != nil {
				t.Fatal(err)
			}
			seedChannelMarketFixture(t, source)
			if _, err := source.Exec(ctx, `UPDATE marketplace.settlements SET `+values); err != nil {
				t.Fatal(err)
			}
			m := NewImporter(readonlySource(t, source), target, crypto)
			if report, err := m.Import(ctx, true); err == nil || report.Applied {
				t.Fatal("unproven precision loss accepted")
			}
			var users int64
			if err := target.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users`).Scan(&users); err != nil || users != 0 {
				t.Fatal("rejected factor wrote partial target")
			}
		})
	}
}
