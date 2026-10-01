//go:build pgintegration

package legacy

import (
	"context"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestCommerceRenewableImportedGroupBonusesResetWithoutRestoringFuel(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedCommerceFixture(t, source)
	seedMarketplaceFixture(t, source)
	seedFundingFixture(t, source)
	historyFixture(t, source)
	ctx := context.Background()
	_, err := source.Exec(ctx, `INSERT INTO migration_source.options(key,value) VALUES('QuotaPerUnit','250000');
	 UPDATE migration_source.subscription_plans SET total_amount=100000;
	 UPDATE migration_source.user_subscriptions SET amount_total=1000000,amount_used=900000,period_amount=400000,period_used=350000 WHERE id=9;
	 UPDATE billing.balance_snapshots SET available_balance=100000 WHERE account_id='subscription-9';
	 UPDATE legacy_boxes.group_buy_members SET bonus_amount_usd=0.400002,created_at=1700000000 WHERE id=100;
	 INSERT INTO migration_source.subscription_orders(id,user_id,plan_id,money,trade_no,payment_method,payment_provider,status,fulfillment_status,create_time,complete_time,purchase_type,target_subscription_id,group_buy_id,fuel_quota,fuel_unit_price,fuel_expires_at,original_money,first_purchase_discount_applied,first_purchase_discount_multiplier,provider_payload) VALUES
	 (3,7,5,12.12,'prior-group-11','alipay','epay','success','completed',1699999900,1699999901,'group_buy',9,11,0,0,0,12.12,false,0,'{}'),
	 (4,7,5,12.12,'prior-group-12','alipay','epay','success','completed',1699999900,1699999901,'group_buy',9,12,0,0,0,12.12,false,0,'{}');
	 INSERT INTO legacy_boxes.group_buy_orders(id,initiator_id,plan_id,target_count,current_count,status,expires_at,settled_at,created_at,updated_at) VALUES
	 (11,7,5,5,1,'pending',1702592000,0,1699999900,1700000001),
	 (12,7,5,5,1,'pending',1702592000,0,1699999900,1699999950);
	 INSERT INTO legacy_boxes.group_buy_members(id,group_buy_id,user_id,order_id,user_subscription_id,bonus_granted,bonus_amount_usd,created_at) VALUES
	 (102,11,7,3,9,true,0.30,1699999900),
	 (104,12,7,4,9,true,0.50,1699999900);
	 INSERT INTO billing.ledger_entries(entry_id,account_id,reference_type,reference_id,entry_type,direction,amount,balance_after,idempotency_key,reason_code,reason_detail,operator_type,operator_id,metadata,created_at) VALUES
	 ('delayed-group','subscription-9','group_buy','11','grant','credit',150000,NULL,'group-buy:11:member:102:tier:150000','subscription_bonus','','system','','{}','2023-11-14T22:13:20Z'),
	 ('old-group','subscription-9','group_buy','12','grant','credit',250000,NULL,'group-buy:12:member:104:tier:250000','subscription_bonus','','system','','{}','2023-11-14T22:13:19Z'),
	 ('fuel-credit','subscription-9','fuel','old-fuel','grant','credit',500000,NULL,'fuel:old-fuel','subscription_fuel','','system','','{}','2023-11-14T22:13:20Z')`)
	if err != nil {
		t.Fatal(err)
	}
	reader := readonlySource(t, source)
	importer := NewImporter(reader, target, crypto)
	for i := 0; i < 2; i++ {
		if report, err := importer.Import(ctx, true); err != nil || !report.Applied {
			t.Fatalf("paid bonus import%d=%+v err=%v", i, report, err)
		}
	}
	if report, err := importer.Check(ctx); err != nil || len(report.Issues) != 0 {
		t.Fatalf("paid bonus reconciliation=%+v err=%v", report, err)
	}
	var renewable, balance, used, account int64
	err = target.QueryRow(ctx, `SELECT s.renewable_credits,a.balance,s.used_credits,s.account_id FROM v3_commerce.subscriptions s JOIN v3_billing.accounts a ON a.id=s.account_id WHERE s.id=9`).Scan(&renewable, &balance, &used, &account)
	if err != nil || renewable != 700002 || balance != 100000 || used != 1800000 {
		t.Fatalf("derived renewable=%d opening=%d used=%d err=%v", renewable, balance, used, err)
	}
	if _, err = target.Exec(ctx, `UPDATE v3_commerce.subscriptions SET renewable_credits=renewable_credits+1 WHERE id=9`); err != nil {
		t.Fatal(err)
	}
	if report, err := importer.Check(ctx); err == nil || len(report.Issues) != 1 {
		t.Fatalf("modified derived allowance undetected=%+v err=%v", report, err)
	}
	if _, err = target.Exec(ctx, `UPDATE v3_commerce.subscriptions SET renewable_credits=renewable_credits-1 WHERE id=9`); err != nil {
		t.Fatal(err)
	}
	service := commerce.New(target, ledger.NewPoster(target), nil, commerce.Config{Now: func() time.Time { return time.Unix(1700000300, 0).UTC() }})
	for i := 0; i < 2; i++ {
		if err = service.ResetSubscription(ctx, 9, 7, "retained-group-renewal"); err != nil {
			t.Fatalf("native reset%d=%v", i, err)
		}
	}
	var current, periodUsed, bucketCount int64
	err = target.QueryRow(ctx, `SELECT s.renewable_credits,a.balance,s.used_credits,s.account_id,s.period_used,
	 (SELECT count(*) FROM v3_commerce.subscription_buckets WHERE subscription_id=9)
	 FROM v3_commerce.subscriptions s JOIN v3_billing.accounts a ON a.id=s.account_id WHERE s.id=9`).Scan(&renewable, &balance, &used, &current, &periodUsed, &bucketCount)
	if err != nil || renewable != 700002 || balance != 800000 || used != 1099998 || current == account || periodUsed != 0 || bucketCount != 2 {
		t.Fatalf("native reset renewable=%d balance=%d residual fuel=%d account=%d/%d cycle=%d buckets=%d err=%v", renewable, balance, used, account, current, periodUsed, bucketCount, err)
	}
	if err = source.QueryRow(ctx, `SELECT amount_used FROM migration_source.user_subscriptions WHERE id=9`).Scan(&used); err != nil || used != 900000 {
		t.Fatalf("readonly source usage changed=%d err=%v", used, err)
	}
}
