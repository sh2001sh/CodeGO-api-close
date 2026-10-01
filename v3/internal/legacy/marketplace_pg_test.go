//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

// seedMarketplaceFixture composes with seedCommerceFixture without duplicating
// its shared users, plans, subscriptions, orders or configuration tables.
func seedMarketplaceFixture(t *testing.T, source *pgxpool.Pool) {
	t.Helper()
	d := marketplaceFullFixture(t)
	d.source["blind_box_props"][0]["used_discount_quota"] = json.RawMessage(`1500000`)
	for _, name := range []string{"users", "subscription_plans", "user_subscriptions", "subscription_orders", "options"} {
		delete(d.source, name)
	}
	d.source["group_buy_orders"][0]["plan_id"] = json.RawMessage(`5`)
	d.source["group_buy_members"][0]["order_id"] = json.RawMessage(`1`)
	if _, err := source.Exec(context.Background(), `INSERT INTO migration_source.options(key,value) VALUES('blind_box_setting.subscription_plan_title','Daily-cycle')`); err != nil {
		t.Fatal(err)
	}
	marketplaceSourceFixtureSchema(t, source, d, "legacy_boxes")
}

func marketplaceSourceFixture(t *testing.T, pool *pgxpool.Pool, d *marketplaceData) map[string]string {
	t.Helper()
	return marketplaceSourceFixtureSchema(t, pool, d, "marketplace")
}

func marketplaceSourceFixtureSchema(t *testing.T, pool *pgxpool.Pool, d *marketplaceData, schema string) map[string]string {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	sources := make(map[string]string)
	for name, rows := range d.source {
		types := make(map[string]string)
		for _, r := range rows {
			for key, raw := range r {
				if string(raw) == "null" {
					continue
				}
				value := r.text(key)
				kind := "text"
				if string(raw) == "true" || string(raw) == "false" {
					kind = "boolean"
				} else if len(raw) > 0 && raw[0] != '"' {
					kind = "bigint"
					if _, err := strconv.ParseInt(value, 10, 64); err != nil {
						kind = "numeric"
					}
				}
				previous := types[key]
				switch {
				case previous == "":
					types[key] = kind
				case previous == "text" || kind == "text":
					types[key] = "text"
				case previous == "numeric" || kind == "numeric":
					types[key] = "numeric"
				case previous != kind:
					types[key] = "text"
				}
			}
		}
		columns := make([]string, 0, len(types))
		for key := range types {
			columns = append(columns, key)
		}
		sort.Strings(columns)
		defs := make([]string, len(columns))
		for i, col := range columns {
			defs[i] = pgx.Identifier{col}.Sanitize() + " " + types[col]
		}
		table := pgx.Identifier{schema, name}.Sanitize()
		if _, err := pool.Exec(ctx, `CREATE TABLE `+table+` (`+strings.Join(defs, ",")+`)`); err != nil {
			t.Fatal(err)
		}
		sources["marketplace_"+name] = table
		if name == "users" || name == "subscription_plans" || name == "user_subscriptions" || name == "subscription_orders" || name == "options" {
			sources[name] = table
		}
		for _, r := range rows {
			values := make([]any, len(columns))
			holders := make([]string, len(columns))
			for i, col := range columns {
				if _, ok := r[col]; ok && string(r[col]) != "null" {
					values[i] = r.text(col)
				}
				holders[i] = fmt.Sprintf("$%d::text::%s", i+1, types[col])
			}
			names := make([]string, len(columns))
			for i, col := range columns {
				names[i] = pgx.Identifier{col}.Sanitize()
			}
			if _, err := pool.Exec(ctx, `INSERT INTO `+table+` (`+strings.Join(names, ",")+`) VALUES (`+strings.Join(holders, ",")+`)`, values...); err != nil {
				t.Fatal(err)
			}
		}
	}
	return sources
}

func TestMarketplaceIndependentPGImportReplayAndCheck(t *testing.T) {
	source, target, crypto := importTestDB(t)
	ctx := context.Background()
	d := marketplaceFullFixture(t)
	d.source["blind_box_props"][0]["used_discount_quota"] = json.RawMessage(`1500000`)
	d.source["balance_blind_box_purchases"] = append(d.source["balance_blind_box_purchases"], marketplaceFixtureRow(t, `{"id":59,"user_id":8,"request_id":"frozen-purchase-59","quantity":1,"unit_price_usd":2.5,"total_quota":1250000,"purchase_date":"2027-01-15","status":"completed","created_at":1800000000}`))
	d.source["balance_blind_box_items"] = append(d.source["balance_blind_box_items"], marketplaceFixtureRow(t, `{"id":54,"purchase_id":59,"purchase_user_id":8,"owner_user_id":8,"pool_version":"legacy-frozen","reward_type":"claude_quota","reward_usd":5,"credit_amount":2500000,"reward_title":"frozen reward","reward_tier":"five","reward_wallet_type":"claude","guarantee_type":"none","status":"available","open_record_id":0,"created_at":1800000000,"updated_at":1800000001,"opened_at":0}`))
	sources := marketplaceSourceFixture(t, source, d)
	reader, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Rollback(ctx) }()
	data, err := loadMarketplace(ctx, reader, sources)
	if err != nil {
		t.Fatal(err)
	}
	var report Report
	data.validate(&report)
	if len(report.Issues) > 0 {
		t.Fatalf("dry-run issues=%+v", report.Issues)
	}
	var count int
	if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_marketplace.blind_box_items`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("preview wrote %d items %v", count, err)
	}
	_, err = target.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES(7,'alice'),(8,'ghost');
	INSERT INTO v3_commerce.plans(id,name,price_minor,credits,period_seconds) VALUES(3,'plan',100,1000,86400);
	INSERT INTO v3_billing.accounts(id,owner_type,owner_id,kind) OVERRIDING SYSTEM VALUE VALUES(90,'subscription',9,'subscription');
	INSERT INTO v3_commerce.subscriptions(id,user_id,plan_id,account_id,starts_at,expires_at) VALUES(9,7,3,90,to_timestamp(1800000000),to_timestamp(1800086400));`)
	if err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(source, target, crypto)
	for i := 0; i < 2; i++ {
		err = pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error { return importer.importMarketplace(ctx, tx, data) })
		if err != nil {
			t.Fatalf("apply %d: %v", i, err)
		}
	}
	var sourceOpen *int64
	if err = target.QueryRow(ctx, `SELECT open_record_id FROM v3_marketplace.blind_box_props WHERE id=45`).Scan(&sourceOpen); err != nil || sourceOpen != nil {
		t.Fatalf("subscription-issued monthly card got invented open record=%v err=%v", sourceOpen, err)
	}
	var profiled bool
	if err = target.QueryRow(ctx, `SELECT cards @> '[{"id":44,"used_discount_micro":3000000,"max_discount_micro":2000000}]'::jsonb FROM v3_marketplace.account_profiles WHERE user_id=7`).Scan(&profiled); err != nil || !profiled {
		t.Fatalf("informational legacy cap wrongly disabled live card: %v %v", profiled, err)
	}
	if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_marketplace.blind_box_items`).Scan(&count); err != nil || count != 5 {
		t.Fatalf("duplicate item issuance %d %v", count, err)
	}
	var wallet, entries int64
	if err = target.QueryRow(ctx, `SELECT (SELECT coalesce(sum(balance),0) FROM v3_billing.accounts),(SELECT count(*) FROM v3_billing.ledger_entries)`).Scan(&wallet, &entries); err != nil || wallet != 0 || entries != 0 {
		t.Fatalf("historical reward granted again balance=%d entries=%d err=%v", wallet, entries, err)
	}
	check := func() Report {
		var result Report
		err = pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error { return importer.checkMarketplace(ctx, tx, data, &result) })
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if result := check(); len(result.Issues) != 0 || result.Counts["check:marketplace"] == 0 {
		t.Fatalf("check=%+v", result)
	}
	if _, err = target.Exec(ctx, `UPDATE v3_marketplace.blind_box_props SET used_discount_micro=used_discount_micro+1 WHERE id=44`); err != nil {
		t.Fatal(err)
	}
	if result := check(); len(result.Issues) != 1 || result.Issues[0].Code != "target_mismatch" {
		t.Fatalf("check missed changed money=%+v", result)
	}
	if err = pgx.BeginFunc(ctx, target, func(tx pgx.Tx) error { return importer.importMarketplace(ctx, tx, data) }); err == nil {
		t.Fatal("replay overwrote changed target card balance")
	}
	if _, err = target.Exec(ctx, `UPDATE v3_marketplace.blind_box_props SET used_discount_micro=used_discount_micro-1 WHERE id=44; DELETE FROM v3_marketplace.blind_box_pity WHERE user_id=7 AND pool_id=2`); err != nil {
		t.Fatal(err)
	}
	if result := check(); len(result.Issues) != 1 {
		t.Fatalf("check missed dropped pity row=%+v", result)
	}
	var original int64
	if err = source.QueryRow(ctx, `SELECT used_discount_quota FROM marketplace.blind_box_props WHERE id=44`).Scan(&original); err != nil || original != 1500000 {
		t.Fatalf("source changed %d %v", original, err)
	}
	// Prove imported facts are consumed by the real post-cutover API. Historical
	// opens never post entries; opening current stock grants its frozen value or
	// current first-draw reward once, without charging the purchase again.
	if _, err = target.Exec(ctx, `UPDATE v3_marketplace.blind_box_pools SET enabled=true WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	accounts := ledger.NewAccounts(target)
	service := marketplace.New(target, ledger.NewPoster(target), accounts, nil, nil, marketplace.Config{Now: func() time.Time { return time.Unix(1800000003, 0) }, Draw: func(int64) (int64, error) { return 0, nil }})
	for i := 0; i < 2; i++ {
		records, openErr := service.OpenBoxes(ctx, 8, "post-cutover-open", 2)
		if openErr != nil || len(records) != 2 {
			t.Fatalf("native open %d records=%+v err=%v", i, records, openErr)
		}
		if records[0].Reward.Amount != 2500000 || records[1].Reward.Amount != 5000000 {
			t.Fatalf("native rewards changed: %+v", records)
		}
	}
	if err = target.QueryRow(ctx, `SELECT (SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=8 AND kind='wallet'),(SELECT count(*) FROM v3_billing.ledger_entries)`).Scan(&wallet, &entries); err != nil || wallet != 7500000 || entries != 2 {
		t.Fatalf("native purchase recharged or reward repeated balance=%d entries=%d err=%v", wallet, entries, err)
	}
}
