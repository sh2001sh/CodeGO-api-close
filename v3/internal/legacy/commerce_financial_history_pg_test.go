//go:build pgintegration

package legacy

import (
	"context"
	"crypto/md5" // Epay's original signed callback protocol uses MD5.
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestCommerceFinancialHistoryImportedResetUseCannotRegainConversion(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedCommerceFixture(t, source)
	ctx := context.Background()
	_, err := source.Exec(ctx, `CREATE TABLE migration_source.subscription_reset_opportunity_ledgers(
	 id bigint PRIMARY KEY,user_id bigint,related_user_id bigint,change_type text,delta bigint,balance_after bigint,
	 used_month text,source_type text,source_ref text,event_key text,note text,created_at bigint,updated_at bigint);
	 INSERT INTO migration_source.subscription_reset_opportunity_ledgers VALUES
	 (1,7,9,'use',-1,0,'2022-01','','','past-use','','1640995200','1640995200'),
	 (2,7,12,'earn',1,1,'','','','unrelated-earn','','1700000000','1700000000'),
	 (3,7,999,'use',-1,0,'2022-01','','','unrelated-use','','1640995200','1640995200');
	 INSERT INTO migration_source.user_subscriptions SELECT 12,user_id,plan_id,amount_total,0,period_amount,0,status,start_time,end_time,last_reset_time,next_reset_time,
	 '{}','{}','admin',membership_tier,created_at,updated_at FROM migration_source.user_subscriptions WHERE id=9`)
	if err != nil {
		t.Fatal(err)
	}
	reader := readonlySource(t, source)
	importer := NewImporter(reader, target, crypto)
	for i := 0; i < 2; i++ {
		if report, err := importer.Import(ctx, true); err != nil || !report.Applied || report.Counts["deferred_game_history.subscription_reset_opportunity_ledgers"] != 3 {
			t.Fatalf("history import%d=%+v err=%v", i, report, err)
		}
	}
	if report, err := importer.Check(ctx); err != nil || len(report.Issues) != 0 {
		t.Fatalf("history source reconciliation=%+v err=%v", report, err)
	}
	var used, unused bool
	if err = target.QueryRow(ctx, `SELECT (SELECT reset_opportunity_used FROM v3_commerce.subscriptions WHERE id=9),
	 (SELECT reset_opportunity_used FROM v3_commerce.subscriptions WHERE id=12)`).Scan(&used, &unused); err != nil || !used || unused {
		t.Fatalf("lifetime reset guard used=%t unused=%t err=%v", used, unused, err)
	}
	if _, err = target.Exec(ctx, `UPDATE v3_commerce.subscriptions SET reset_opportunity_used=false WHERE id=9`); err != nil {
		t.Fatal(err)
	}
	if report, err := importer.Check(ctx); err == nil || len(report.Issues) != 1 {
		t.Fatalf("erased lifetime guard undetected=%+v err=%v", report, err)
	}
	if _, err = target.Exec(ctx, `UPDATE v3_commerce.subscriptions SET reset_opportunity_used=true WHERE id=9`); err != nil {
		t.Fatal(err)
	}
	service := commerce.New(target, ledger.NewPoster(target), nil, commerce.Config{Now: func() time.Time { return time.Unix(1700000300, 0).UTC() }})
	if _, err = service.ConvertSubscription(ctx, 7, 9, 1, "retained-used-conversion"); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("historical used subscription regained conversion=%v", err)
	}
	for i := 0; i < 2; i++ {
		result, err := service.ConvertSubscription(ctx, 7, 12, 1, "retained-unused-conversion")
		if err != nil || result.SourceCredits != 20 || result.TargetCredits != 121200 {
			t.Fatalf("unused conversion%d=%+v err=%v", i, result, err)
		}
	}
	var count int64
	if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscription_conversions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate/used conversions count=%d err=%v", count, err)
	}
	if err = source.QueryRow(ctx, `SELECT amount_used FROM migration_source.user_subscriptions WHERE id=12`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("source subscription history changed=%d err=%v", count, err)
	}
	if err = source.QueryRow(ctx, `SELECT count(*) FROM migration_source.subscription_reset_opportunity_ledgers`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("source deferred history changed=%d err=%v", count, err)
	}
}

type commerceImportedRefundProvider struct {
	calls   int
	request commerce.RefundPayment
}

func (p *commerceImportedRefundProvider) CreateRefund(_ context.Context, in commerce.RefundPayment) (commerce.RefundProviderResult, error) {
	p.calls++
	p.request = in
	return commerce.RefundProviderResult{RefundNo: in.RefundNo, RefundID: "offline-provider-receipt", AmountMinor: in.AmountMinor, State: "success"}, nil
}

func (p *commerceImportedRefundProvider) QueryRefund(_ context.Context, _ string, no string) (commerce.RefundProviderResult, error) {
	return commerce.RefundProviderResult{RefundNo: no, RefundID: "offline-provider-receipt", AmountMinor: p.request.AmountMinor, State: "success"}, nil
}

func TestCommerceFinancialHistoryImportedProviderTransactionSupportsRefundAndSignedReplay(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedCommerceFixture(t, source)
	ctx := context.Background()
	_, err := source.Exec(ctx, `UPDATE migration_source.users SET claude_quota=1000000 WHERE id=7;
	 UPDATE billing.balance_snapshots SET available_balance=1000000 WHERE account_id='wallet-7';
	 UPDATE billing.funding_lots SET remaining_amount=1000000 WHERE lot_id='old-paid-lot';
	 UPDATE migration_source.top_ups SET external_payment_id='retained-provider-payment',refund_status='',refund_no='',refund_amount=0,refund_quota=0,refund_updated_at=0 WHERE id=1;
	 UPDATE migration_source.subscription_orders SET provider_payload='{"TradeNo":"retained-sub-payment","ServiceTradeNo":"legacy-sub-1","VerifyStatus":true}' WHERE id=1`)
	if err != nil {
		t.Fatal(err)
	}
	reader := readonlySource(t, source)
	importer := NewImporter(reader, target, crypto)
	for i := 0; i < 2; i++ {
		if report, err := importer.Import(ctx, true); err != nil || !report.Applied {
			t.Fatalf("refund history import%d=%+v err=%v", i, report, err)
		}
	}
	if report, err := importer.Check(ctx); err != nil || len(report.Issues) != 0 {
		t.Fatalf("payment reconciliation=%+v err=%v", report, err)
	}
	var reference, event, subEvent string
	if err = target.QueryRow(ctx, `SELECT provider_reference,payment_event_id,(SELECT payment_event_id FROM v3_commerce.orders WHERE id=3) FROM v3_commerce.orders WHERE id=2`).Scan(&reference, &event, &subEvent); err != nil || reference != "legacy-topup-1" || event != "retained-provider-payment" || subEvent != "retained-sub-payment" {
		t.Fatalf("payment identity merchant=%s event=%s subscription=%s err=%v", reference, event, subEvent, err)
	}
	if _, err = target.Exec(ctx, `UPDATE v3_commerce.orders SET payment_event_id='wrong-provider-transaction' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if report, err := importer.Check(ctx); err == nil || len(report.Issues) != 1 {
		t.Fatalf("altered provider transaction undetected=%+v err=%v", report, err)
	}
	if _, err = target.Exec(ctx, `UPDATE v3_commerce.orders SET payment_event_id='retained-provider-payment' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	poster := ledger.NewPoster(target)
	epay := commerce.NewEpay(commerce.EpayConfig{MerchantID: "offline-merchant", Secret: "offline-test-secret"})
	service := commerce.New(target, poster, []commerce.PaymentProvider{epay}, commerce.Config{})
	body := commerceImportedEpayBody("legacy-topup-1", "retained-provider-payment", "2.35")
	var before, after int64
	if err = target.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = service.HandleWebhook(ctx, "epay", http.Header{}, body); err != nil {
			t.Fatalf("signed historical callback%d=%v", i, err)
		}
	}
	if err = service.HandleWebhook(ctx, "epay", http.Header{}, commerceImportedEpayBody("legacy-topup-1", "retained-provider-payment", "2.36")); !errors.Is(err, commerce.ErrPaymentMismatch) {
		t.Fatalf("conflicting signed payment accepted=%v", err)
	}
	if err = target.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet'`).Scan(&after); err != nil || after != before || after != 2000000 {
		t.Fatalf("historical callback recredited wallet=%d/%d err=%v", before, after, err)
	}
	provider := &commerceImportedRefundProvider{}
	refunds := commerce.NewUserRefunds(target, poster, nil, provider)
	eligible, err := refunds.Eligible(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, order := range eligible {
		if order.TradeNo == "legacy-topup-1" {
			found = order.Refundable && order.RefundAmountMinor == 230
		}
	}
	if !found {
		t.Fatalf("original provider payment not refundable=%+v", eligible)
	}
	for i := 0; i < 2; i++ {
		result, err := refunds.Create(ctx, 7, commerce.UserRefundRequest{OrderType: "balance", TradeNo: "legacy-topup-1"})
		if err != nil || result.Status != "success" || result.AmountMinor != 230 {
			t.Fatalf("imported refund%d=%+v err=%v", i, result, err)
		}
	}
	if provider.calls != 1 || provider.request.OrderID != "retained-provider-payment" || provider.request.AmountMinor != 230 {
		t.Fatalf("refund did not use original platform transaction once=%+v", provider)
	}
	if err = source.QueryRow(ctx, `SELECT available_balance FROM billing.balance_snapshots WHERE account_id='wallet-7'`).Scan(&after); err != nil || after != 1000000 {
		t.Fatalf("source wallet changed=%d err=%v", after, err)
	}
}

func commerceImportedEpayBody(trade, event, money string) []byte {
	values := url.Values{"pid": {"offline-merchant"}, "out_trade_no": {trade}, "trade_no": {event}, "money": {money}, "trade_status": {"TRADE_SUCCESS"}, "type": {"alipay"}}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}
	digest := md5.Sum([]byte(strings.Join(parts, "&") + "offline-test-secret"))
	values.Set("sign", hex.EncodeToString(digest[:]))
	values.Set("sign_type", "MD5")
	return []byte(values.Encode())
}
