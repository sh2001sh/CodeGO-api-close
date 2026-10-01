//go:build pgintegration

package legacy

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedCommerceFixture can be reused by the parent's full independent-DB test.
// The data exercises original IDs that collide across the two order tables,
// a nearly-exhausted daily cycle, three redemption types, partial refund
// history, transfer credentials, issued invoice metadata, and timestamps.
func seedCommerceFixture(t *testing.T, source *pgxpool.Pool) {
	t.Helper()
	_, err := source.Exec(context.Background(), `
		INSERT INTO migration_source.users VALUES(8,'bob','bcrypt-placeholder',1,1,'default',0,750,'{}');
		CREATE TABLE migration_source.subscription_plans(id bigint PRIMARY KEY,title text,subtitle text,price_amount numeric,currency text,
			total_amount bigint,period_amount bigint,duration_unit text,duration_value bigint,quota_reset_period text,enabled bool,
			model_limits text,created_at bigint,updated_at bigint);
		INSERT INTO migration_source.subscription_plans VALUES(5,'Daily-cycle','Historical plan',12.123456,'USD',1000,400,'month',1,'daily',true,
			'{"chat-model":250}',1690000000,1700000000);
		CREATE TABLE migration_source.user_subscriptions(id bigint PRIMARY KEY,user_id bigint,plan_id bigint,amount_total bigint,amount_used bigint,
			period_amount bigint,period_used bigint,status text,start_time bigint,end_time bigint,last_reset_time bigint,next_reset_time bigint,
			model_limits text,model_usage text,source text,membership_tier text,created_at bigint,updated_at bigint);
		INSERT INTO migration_source.user_subscriptions VALUES
			(9,7,5,1000,200,400,350,'active',1700000000,1702592000,1700000000,1700086400,'{"chat-model":250}','{"chat-model":100}','order','silver',1700000000,1700000100),
			(10,8,5,1000,999,0,0,'active',1700000000,1702592000,1700000000,1700086400,'{}','{}','admin','none',1700000000,1700000100),
			(11,7,5,1000,1000,400,400,'expired',1690000000,1692592000,1690000000,0,'{}','{}','order','none',1690000000,1692592000);
		INSERT INTO billing.accounts VALUES('subscription-9','user_subscription',9,'subscription','quota');
		INSERT INTO billing.balance_snapshots VALUES('subscription-9',800,0);
		CREATE TABLE migration_source.top_ups(id bigint PRIMARY KEY,user_id bigint,amount bigint,money numeric,trade_no text,payment_method text,
			payment_provider text,status text,create_time bigint,complete_time bigint,refund_status text,refund_no text,refund_amount numeric,
			refund_quota bigint,refund_updated_at bigint,external_payment_id text);
		INSERT INTO migration_source.top_ups VALUES
			(1,7,2,2.35,'legacy-topup-1','alipay','epay','success',1700000000,1700000100,'success','partial-refund',1.25,100,1700000200,''),
			(2,8,3,3.00,'legacy-topup-2','stripe','stripe','pending',1700000000,0,'','',0,0,0,'provider-2');
		CREATE TABLE migration_source.subscription_orders(id bigint PRIMARY KEY,user_id bigint,plan_id bigint,money numeric,trade_no text,payment_method text,
			payment_provider text,status text,fulfillment_status text,create_time bigint,complete_time bigint,purchase_type text,target_subscription_id bigint,
			group_buy_id bigint,fuel_quota bigint,fuel_unit_price numeric,fuel_expires_at bigint,original_money numeric,first_purchase_discount_applied bool,
			first_purchase_discount_multiplier numeric,provider_payload text);
		INSERT INTO migration_source.subscription_orders VALUES
			(1,7,5,12.12,'legacy-sub-1','alipay','epay','success','completed',1700000000,1700000100,'normal',0,0,0,0,0,12.12,false,0,'{}'),
			(2,8,5,1.00,'legacy-sub-2','alipay','epay','success','completed',1700000000,1700000100,'subscription_fuel',10,0,500,0.1,1702592000,2,true,0.5,'{}');
		CREATE TABLE migration_source.redemptions(id bigint PRIMARY KEY,user_id bigint,key text,status int,name text,redeem_type text,quota bigint,
			wallet_type text,plan_id bigint,plan_title text,blind_box_quantity bigint,created_time bigint,redeemed_time bigint,used_user_id bigint,expired_time bigint,deleted_at timestamptz);
		INSERT INTO migration_source.redemptions VALUES
			(1,7,'original-unused-code',1,'Quota','quota',123,'claude',0,'',0,1700000000,0,0,0,NULL),
			(2,7,'original-subscription-code',1,'Plan','subscription',0,'claude',5,'Daily-cycle',0,1700000000,0,0,0,NULL),
			(3,7,'original-blind-box-code',3,'Boxes','blind_box',0,'claude',0,'',2,1700000000,1700000100,8,0,NULL);
		CREATE TABLE migration_source.wallet_transfers(id bigint PRIMARY KEY,request_id text,sender_user_id bigint,recipient_user_id bigint,sender_external_id text,
			recipient_external_id text,sender_display_name_masked text,recipient_display_name_masked text,amount_quota bigint,fee_quota bigint,total_debit_quota bigint,
			sender_balance_after bigint,recipient_balance_after bigint,status text,created_at bigint);
		INSERT INTO migration_source.wallet_transfers VALUES(21,'old-transfer-21',7,8,'old-7','old-8','A***','B***',50,1,51,500,750,'completed',1700000200);
		CREATE TABLE migration_source.wallet_transfer_securities(user_id bigint PRIMARY KEY,password_hash text,failed_attempts bigint,locked_until bigint,created_at bigint,updated_at bigint);
		INSERT INTO migration_source.wallet_transfer_securities VALUES(7,'$2a$10$7EqJtq98hPqEX7fNZaFWoO5FrJBJuJDHoBdOlwwsiVzrqGoHdPp9i',2,1700001000,1700000000,1700000200);
		CREATE TABLE migration_source.invoice_requests(id bigint PRIMARY KEY,user_id bigint,source_type text,trade_no text,order_amount numeric,currency text,order_title text,
			order_count bigint,invoice_type text,title text,tax_number text,email text,remark text,status text,invoice_number text,delivery_method text,document_url text,
			admin_note text,handled_by bigint,issued_at bigint,created_at bigint,updated_at bigint);
		INSERT INTO migration_source.invoice_requests VALUES(31,7,'topup','legacy-topup-1',2.35,'CNY','Topup',1,'personal','Alice','','alice@example.invalid','Old invoice',
			'issued','INV-31','email','https://invoice.invalid/31','Reviewed',7,1700000200,1700000100,1700000200);
		CREATE TABLE migration_source.invoice_request_items(id bigint PRIMARY KEY,invoice_id bigint,user_id bigint,source_type text,trade_no text,order_amount numeric,currency text,order_title text,paid_at bigint);
		INSERT INTO migration_source.invoice_request_items VALUES(32,31,7,'topup','legacy-topup-1',2.35,'CNY','Topup',1700000100);
		CREATE TABLE migration_source.subscription_pre_consume_records(id bigint PRIMARY KEY,request_id text,user_id bigint,user_subscription_id bigint,model_name text,pre_consumed bigint,status text,created_at bigint,updated_at bigint);
		INSERT INTO migration_source.subscription_pre_consume_records VALUES(41,'refunded-request',7,9,'chat-model',10,'refunded',1700000000,1700000100);`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.Exec(context.Background(), `CREATE TABLE billing.funding_lots(lot_id text PRIMARY KEY,account_id text,source text,idempotency_key text,original_amount bigint,remaining_amount bigint,
	 reference_type text NOT NULL DEFAULT '',reference_id text NOT NULL DEFAULT '',revenue_multiplier numeric NOT NULL DEFAULT 0,created_at timestamptz NOT NULL);
	 INSERT INTO billing.funding_lots VALUES('old-paid-lot','wallet-7','topup','topup:legacy-topup-1:unified',1000000,400,'topup','legacy-topup-1',1,'2023-11-14T22:13:20Z');`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCommerceIndependentReadOnlySourceImport(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedCommerceFixture(t, source)
	ctx := context.Background()
	reader := readonlySource(t, source)
	tx, err := reader.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	data, err := loadCommerce(ctx, tx, sources)
	if err != nil {
		t.Fatal(err)
	}
	report := Report{}
	data.validate(&report)
	if len(report.Issues) != 0 || report.Amounts["subscription_opening_micro_credits"] != "102" {
		t.Fatalf("preview=%+v", report)
	}
	var count int
	if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.plans`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("preview mutated target: %d %v", count, err)
	}
	users, err := loadUsers(ctx, tx, sources)
	if err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(reader, target, crypto)
	for i := 0; i < 2; i++ {
		err = pgx.BeginFunc(ctx, target, func(targetTx pgx.Tx) error {
			if err := importer.importUsers(ctx, targetTx, users); err != nil {
				return err
			}
			return importer.importCommerce(ctx, targetTx, data)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for table, want := range map[string]int{"plans": 1, "subscriptions": 3, "orders": 4, "redemption_codes": 3, "wallet_transfers": 1, "invoices": 1, "invoice_items": 1, "subscription_preconsumes": 1} {
		if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.`+table).Scan(&count); err != nil || count != want {
			t.Fatalf("%s count=%d want=%d err=%v", table, count, want, err)
		}
	}
	var balance, cycleUsed int64
	if err = target.QueryRow(ctx, `SELECT a.balance,s.period_used FROM v3_commerce.subscriptions s JOIN v3_billing.accounts a ON a.id=s.account_id WHERE s.id=9`).Scan(&balance, &cycleUsed); err != nil || balance != 100 || cycleUsed != 700 {
		t.Fatalf("balance=%d cycleUsed=%d err=%v", balance, cycleUsed, err)
	}
	var credits, minor int64
	if err = target.QueryRow(ctx, `SELECT credits,amount_minor FROM v3_commerce.orders WHERE id=2 AND legacy_id=1 AND kind='topup'`).Scan(&credits, &minor); err != nil || credits != 2000000 || minor != 235 {
		t.Fatalf("topup credits=%d minor=%d err=%v", credits, minor, err)
	}
	var password string
	if err = target.QueryRow(ctx, `SELECT password_hash FROM v3_commerce.wallet_transfer_security WHERE user_id=7`).Scan(&password); err != nil || !strings.HasPrefix(password, "$2a$") {
		t.Fatal("transfer credential not preserved")
	}
	var originRemaining int64
	if err = target.QueryRow(ctx, `SELECT remaining_credits FROM v3_commerce.user_refund_origins WHERE order_id=2`).Scan(&originRemaining); err != nil || originRemaining != 800 {
		t.Fatalf("refundable origin=%d %v", originRemaining, err)
	}
	check := func() Report {
		t.Helper()
		r := Report{}
		if err := pgx.BeginTxFunc(ctx, target, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(readTx pgx.Tx) error {
			return importer.checkCommerce(ctx, readTx, data, &r)
		}); err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := check(); len(r.Issues) != 0 || r.Counts["check:commerce"] != 16 {
		t.Fatalf("initial check=%+v", r)
	}
	if _, err = target.Exec(ctx, `UPDATE v3_commerce.orders SET credits=credits+1 WHERE id=2; DELETE FROM v3_commerce.invoice_items WHERE id=32`); err != nil {
		t.Fatal(err)
	}
	if r := check(); len(r.Issues) != 2 {
		t.Fatalf("changed amount and deleted item were not detected: %+v", r)
	}
	if _, err = target.Exec(ctx, `UPDATE v3_commerce.orders SET credits=credits-1 WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if err = pgx.BeginFunc(ctx, target, func(targetTx pgx.Tx) error { return importer.importCommerce(ctx, targetTx, data) }); err != nil {
		t.Fatal(err)
	}
	if r := check(); len(r.Issues) != 0 {
		t.Fatalf("repaired check=%+v", r)
	}
	// A spend after import is preserved on subsequent import; history must not
	// re-credit the subscription or restore its old projection.
	_, err = target.Exec(ctx, `INSERT INTO v3_billing.ledger_entries(account_id,amount,balance_after,kind,operation_id)
		SELECT id,-10,90,'usage','commerce-test-spend' FROM v3_billing.accounts WHERE owner_type='subscription' AND owner_id=9;
		UPDATE v3_billing.accounts SET balance=90,version=2 WHERE owner_type='subscription' AND owner_id=9`)
	if err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(ctx, target, func(targetTx pgx.Tx) error { return importer.importCommerce(ctx, targetTx, data) })
	if err != nil {
		t.Fatal(err)
	}
	if err = target.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE owner_type='subscription' AND owner_id=9`).Scan(&balance); err != nil || balance != 90 {
		t.Fatalf("reimport restored used balance: %d %v", balance, err)
	}
}
