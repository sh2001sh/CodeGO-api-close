//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"
)

// A failure in the final discount initializer must roll back the earlier
// subscriptions, openings, and pending monthly/group intents as one import.
func TestCommerceRuntimeImportAtomicReplayAndCheck(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedCommerceFixture(t, source)
	seedFundingFixture(t, source)
	ctx := context.Background()
	_, err := source.Exec(ctx, `ALTER TABLE migration_source.subscription_plans
	 ADD COLUMN membership_tier text DEFAULT 'pro', ADD COLUMN plan_type text DEFAULT 'monthly', ADD COLUMN group_buy_enabled bool DEFAULT true;
	 INSERT INTO migration_source.subscription_orders VALUES
	 (3,7,5,6,'pending-group','alipay','epay','pending','pending',1700000000,0,'group_buy',0,0,0,0,0,12,true,0.5,'{}'),
	 (4,7,5,6,'pending-renew','alipay','epay','pending','pending',1700000000,0,'renew',9,0,0,0,0,12,true,0.5,'{}')`)
	if err != nil {
		t.Fatal(err)
	}
	reader := readonlySource(t, source)
	importer := NewImporter(reader, target, crypto)
	if report, err := importer.Import(ctx, false); err != nil || len(report.Issues) != 0 || report.Applied {
		t.Fatalf("preview=%+v err=%v", report, err)
	}
	_, err = target.Exec(ctx, `CREATE FUNCTION v3_commerce.reject_import_discount() RETURNS trigger LANGUAGE plpgsql AS $$
	 BEGIN RAISE EXCEPTION 'forced final metadata failure'; END $$;
	 CREATE TRIGGER reject_import_discount BEFORE INSERT ON v3_commerce.checkout_discounts
	 FOR EACH ROW EXECUTE FUNCTION v3_commerce.reject_import_discount()`)
	if err != nil {
		t.Fatal(err)
	}
	if report, err := importer.Import(ctx, true); err == nil || report.Applied || !strings.Contains(err.Error(), "forced final metadata failure") {
		t.Fatalf("forced failure=%+v err=%v", report, err)
	}
	for _, table := range []string{"v3_identity.users", "v3_billing.accounts", "v3_billing.ledger_entries", "v3_commerce.orders", "v3_commerce.subscriptions", "v3_commerce.monthly_purchase_benefits", "v3_commerce.group_checkouts"} {
		var count int
		if err = target.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("failed final metadata committed %s: count=%d err=%v", table, count, err)
		}
	}
	if _, err = target.Exec(ctx, `DROP TRIGGER reject_import_discount ON v3_commerce.checkout_discounts; DROP FUNCTION v3_commerce.reject_import_discount()`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		report, err := importer.Import(ctx, true)
		if err != nil || !report.Applied {
			t.Fatalf("apply %d=%+v err=%v", i, report, err)
		}
		want := int64(2)
		if i == 1 {
			want = 0
		}
		if report.Counts["initialized:monthly_purchase_benefits"] != want || report.Counts["initialized:checkout_discounts"] != want {
			t.Fatalf("metadata replay %d=%+v", i, report.Counts)
		}
		groupWant := want / 2
		if report.Counts["initialized:group_checkouts"] != groupWant {
			t.Fatalf("group replay %d=%+v", i, report.Counts)
		}
	}
	if report, err := importer.Check(ctx); err != nil || len(report.Issues) != 0 || report.Counts["check:commerce_runtime"] != 5 {
		t.Fatalf("complete frozen metadata check=%+v err=%v", report, err)
	}
	var targetSeconds, sourceSeconds, fullPrice int64
	if err = target.QueryRow(ctx, `SELECT target_seconds,source_seconds,full_price_minor FROM v3_commerce.monthly_purchase_benefits WHERE order_id=9`).Scan(&targetSeconds, &sourceSeconds, &fullPrice); err != nil || targetSeconds != 2700 || sourceSeconds != 2700 || fullPrice != 1212 {
		t.Fatalf("renew monthly snapshot=%d/%d/%d err=%v", targetSeconds, sourceSeconds, fullPrice, err)
	}
	var openingCount int
	if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='opening'`).Scan(&openingCount); err != nil || openingCount != 4 {
		t.Fatalf("replayed openings=%d err=%v", openingCount, err)
	}
	if _, err = target.Exec(ctx, `UPDATE v3_commerce.monthly_purchase_benefits SET target_seconds=0 WHERE order_id=7;
	 UPDATE v3_commerce.group_checkouts SET user_id=8 WHERE order_id=7;
	 UPDATE v3_commerce.checkout_discounts SET multiplier='0.25' WHERE order_id=9`); err != nil {
		t.Fatal(err)
	}
	if report, err := importer.Check(ctx); err == nil || len(report.Issues) != 3 {
		t.Fatalf("changed frozen metadata undetected=%+v err=%v", report, err)
	}
	if _, err = target.Exec(ctx, `DELETE FROM v3_commerce.monthly_purchase_benefits WHERE order_id=7;
	 DELETE FROM v3_commerce.group_checkouts WHERE order_id=7;
	 DELETE FROM v3_commerce.checkout_discounts WHERE order_id=9`); err != nil {
		t.Fatal(err)
	}
	if report, err := importer.Check(ctx); err == nil || len(report.Issues) != 3 {
		t.Fatalf("missing frozen metadata undetected=%+v err=%v", report, err)
	}
	if report, err := importer.Import(ctx, true); err != nil || report.Counts["initialized:monthly_purchase_benefits"] != 1 || report.Counts["initialized:group_checkouts"] != 1 || report.Counts["initialized:checkout_discounts"] != 1 {
		t.Fatalf("repair metadata=%+v err=%v", report, err)
	}
	if report, err := importer.Check(ctx); err != nil || len(report.Issues) != 0 {
		t.Fatalf("repaired metadata check=%+v err=%v", report, err)
	}
	var fuel string
	if err = target.QueryRow(ctx, `SELECT purchase_type FROM v3_commerce.orders WHERE id=5`).Scan(&fuel); err != nil || fuel != "fuel" {
		t.Fatalf("source fuel kind unavailable to native runtime: %q err=%v", fuel, err)
	}
}
