//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestImportedCashBoxLateSignedPaymentIsQuarantinedOnceAndVisibleToOwnerAndAdmins(t *testing.T) {
	s, _, pool, p := cashBoxFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET role='root' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_orders
	 (user_id,pool_id,trade_no,quantity,amount_minor,currency,payment_method,payment_provider,source,status)
	 VALUES(1,$1,'v2-expired-cash',2,500,'cny','alipay','epay','purchase','expired'),
	 (1,$1,'v2-second-cash',2,500,'cny','alipay','epay','purchase','expired')`, p.ID); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux, func(*http.Request) (commerce.Actor, error) { return commerce.Actor{UserID: 1, Role: "user"}, nil })
	values := url.Values{"pid": {"merchant"}, "out_trade_no": {"v2-expired-cash"}, "trade_no": {"real-platform-identity"},
		"trade_status": {"TRADE_SUCCESS"}, "money": {"5.00"}, "type": {"alipay"}, "sign_type": {"MD5"}}
	values.Set("sign", "invalid")
	bad := httptest.NewRecorder()
	mux.ServeHTTP(bad, httptest.NewRequest("GET", "/api/blind-box/epay/notify?"+values.Encode(), nil))
	if bad.Code == 200 || cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.orders`) != 0 {
		t.Fatal("unsigned callback created a financial record")
	}
	values.Set("money", "5.01")
	values.Set("sign", cashBoxSign(values))
	bad = httptest.NewRecorder()
	mux.ServeHTTP(bad, httptest.NewRequest("GET", "/api/blind-box/epay/notify?"+values.Encode(), nil))
	if bad.Code == 200 || cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.orders`) != 0 {
		t.Fatal("signed wrong amount created a financial record")
	}
	values.Set("money", "5.00")
	values.Set("sign", cashBoxSign(values))
	if _, err := pool.Exec(ctx, `UPDATE v3_marketplace.blind_box_orders SET currency='usd' WHERE trade_no='v2-expired-cash'`); err != nil {
		t.Fatal(err)
	}
	bad = httptest.NewRecorder()
	mux.ServeHTTP(bad, httptest.NewRequest("GET", "/api/blind-box/epay/notify?"+values.Encode(), nil))
	if bad.Code == 200 || cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.orders`) != 0 {
		t.Fatal("signed wrong currency created a financial record")
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_marketplace.blind_box_orders SET currency='cny' WHERE trade_no='v2-expired-cash'`); err != nil {
		t.Fatal(err)
	}
	values.Set("sign", cashBoxSign(values))
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/blind-box/epay/notify?"+values.Encode(), nil))
			if w.Code != 200 || w.Body.String() != "success" {
				t.Errorf("verified legacy payment was not durably acknowledged: %d %s", w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	o, err := s.GetOrder(ctx, 1, "v2-expired-cash")
	if err != nil || o.State != "paid" || o.Kind != "blind_box" || o.Credits != 0 || o.AmountMinor != 500 || o.Currency != "cny" || o.PaymentURL != "" || o.FulfillmentState != "requires_review" || o.Selection.PaymentMethod != "alipay" {
		t.Fatalf("original money and delivery uncertainty lost: %+v %v", o, err)
	}
	if _, err := s.GetOrder(ctx, 2, o.TradeNo); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatal("financial record escaped its original owner")
	}
	status, err := s.CashBoxOrder(ctx, 1, o.TradeNo)
	if err != nil || status.Status != "requires_review" {
		t.Fatal("quarantined legacy payment was reported as successful inventory")
	}
	reviews, err := s.ListPackagePaymentReviews(ctx)
	if err != nil || len(reviews) != 1 || !strings.Contains(reviews[0].Reason, "real-platform-identity") {
		t.Fatal("administrator cannot review original platform identity")
	}
	for query, want := range map[string]int64{
		`SELECT count(*) FROM v3_commerce.orders`:                                                    1,
		`SELECT count(*) FROM v3_commerce.payment_events`:                                            1,
		`SELECT count(*) FROM v3_commerce.package_payment_reviews`:                                   1,
		`SELECT count(*) FROM v3_identity.notifications WHERE kind='legacy_cash_box_payment_review'`: 2,
		`SELECT count(*) FROM v3_identity.notifications WHERE kind='order_paid'`:                     0,
		`SELECT count(*) FROM v3_billing.ledger_entries`:                                             0,
		`SELECT count(*) FROM v3_marketplace.blind_box_items`:                                        0,
		`SELECT count(*) FROM v3_marketplace.blind_box_purchases`:                                    0,
		`SELECT count(*) FROM v3_marketplace.blind_box_orders WHERE provider_payload->'v3_late_payment'->>'event_id'='real-platform-identity' AND status='expired'`: 1,
	} {
		if got := cashBoxCount(t, pool, query); got != want {
			t.Fatalf("quarantine invariant: %s got %d want %d", query, got, want)
		}
	}
	if err := s.ResolvePackagePaymentReview(ctx, o.ID); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatal("mere administrator acknowledgement erased unresolved payment")
	}
	for field, value := range map[string]string{"money": "5.01", "pid": "other-merchant", "trade_no": "different-platform-identity", "out_trade_no": "unknown-order"} {
		copy := url.Values{}
		for k, v := range values {
			copy[k] = append([]string(nil), v...)
		}
		copy.Set(field, value)
		copy.Set("sign", cashBoxSign(copy))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/blind-box/epay/notify?"+copy.Encode(), nil))
		if w.Code == 200 {
			t.Errorf("conflicting signed %s was acknowledged", field)
		}
	}
	values.Set("out_trade_no", "v2-second-cash")
	values.Set("sign", cashBoxSign(values))
	if err := s.HandleWebhook(ctx, "epay", nil, []byte(values.Encode())); !errors.Is(err, commerce.ErrPaymentMismatch) {
		t.Fatal("one provider transaction could pay a different original order", err)
	}
	if cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.orders`) != 1 {
		t.Fatal("reused platform identity left another financial record")
	}
	if cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.payment_events`) != 1 || cashBoxCount(t, pool, `SELECT count(*) FROM v3_identity.notifications`) != 2 {
		t.Fatal("conflicting callbacks mutated receipts or notifications")
	}
}

func TestImportedCashBoxLatePaymentFailureRollsBackFinancialRecordAndReceipt(t *testing.T) {
	s, _, pool, p := cashBoxFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_orders
	 (user_id,pool_id,trade_no,quantity,amount_minor,currency,payment_method,payment_provider,source,status)
	 VALUES(1,$1,'v2-quarantine-rollback',2,500,'cny','alipay','epay','purchase','expired')`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE v3_identity.notifications ADD CONSTRAINT reject_fixture_notice CHECK(kind<>'legacy_cash_box_payment_review')`); err != nil {
		t.Fatal(err)
	}
	v := url.Values{"pid": {"merchant"}, "out_trade_no": {"v2-quarantine-rollback"}, "trade_no": {"rollback-platform-id"}, "trade_status": {"TRADE_SUCCESS"}, "money": {"5.00"}, "sign_type": {"MD5"}}
	v.Set("sign", cashBoxSign(v))
	if err := s.HandleWebhook(ctx, "epay", nil, []byte(v.Encode())); err == nil {
		t.Fatal("notification failure was swallowed")
	}
	for _, query := range []string{`SELECT count(*) FROM v3_commerce.orders`, `SELECT count(*) FROM v3_commerce.payment_events`, `SELECT count(*) FROM v3_commerce.package_payment_reviews`, `SELECT count(*) FROM v3_identity.notifications`, `SELECT count(*) FROM v3_marketplace.blind_box_orders WHERE provider_payload ? 'v3_late_payment'`} {
		if cashBoxCount(t, pool, query) != 0 {
			t.Fatal("failed quarantine committed partial state")
		}
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE v3_identity.notifications DROP CONSTRAINT reject_fixture_notice`); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleWebhook(ctx, "epay", nil, []byte(v.Encode())); err != nil {
		t.Fatal("durable receipt could not retry after rollback", err)
	}
}

func TestImportedSuccessfulCashBoxAcknowledgesOnlyOriginalVerifiedReceiptWithoutDelivery(t *testing.T) {
	s, _, pool, p := cashBoxFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_orders
	 (user_id,pool_id,trade_no,quantity,opened_count,amount_minor,currency,payment_method,payment_provider,source,status,completed_at,provider_payload)
	 VALUES(1,$1,'v2-already-delivered',2,2,500,'cny','alipay','epay','purchase','success',now(),
	 '{"TradeNo":"original-paid-identity","ServiceTradeNo":"v2-already-delivered","Money":"5.00","TradeStatus":"TRADE_SUCCESS","VerifyStatus":true}')`, p.ID); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux, func(*http.Request) (commerce.Actor, error) { return commerce.Actor{UserID: 1, Role: "user"}, nil })
	v := url.Values{"pid": {"merchant"}, "out_trade_no": {"v2-already-delivered"}, "trade_no": {"original-paid-identity"}, "trade_status": {"TRADE_SUCCESS"}, "money": {"5.00"}, "sign_type": {"MD5"}}
	v.Set("sign", cashBoxSign(v))
	for range 2 {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/blind-box/epay/notify?"+v.Encode(), nil))
		if w.Code != 200 || w.Body.String() != "success" {
			t.Fatalf("original already-delivered receipt kept retrying: %d %s", w.Code, w.Body.String())
		}
	}
	v.Set("trade_no", "different-paid-identity")
	v.Set("sign", cashBoxSign(v))
	if err := s.HandleWebhook(ctx, "epay", nil, []byte(v.Encode())); !errors.Is(err, commerce.ErrPaymentMismatch) {
		t.Fatal("new signed payment impersonated historical delivered identity", err)
	}
	v.Set("trade_no", "original-paid-identity")
	v.Set("sign", cashBoxSign(v))
	if _, err := pool.Exec(ctx, `UPDATE v3_marketplace.blind_box_orders SET provider_payload='{}' WHERE trade_no='v2-already-delivered'`); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleWebhook(ctx, "epay", nil, []byte(v.Encode())); !errors.Is(err, commerce.ErrPaymentMismatch) {
		t.Fatal("missing historical identity silently acknowledged", err)
	}
	for _, query := range []string{`SELECT count(*) FROM v3_commerce.orders`, `SELECT count(*) FROM v3_commerce.payment_events`, `SELECT count(*) FROM v3_commerce.package_payment_reviews`, `SELECT count(*) FROM v3_identity.notifications`, `SELECT count(*) FROM v3_marketplace.blind_box_items`, `SELECT count(*) FROM v3_marketplace.blind_box_purchases`, `SELECT count(*) FROM v3_billing.ledger_entries`} {
		if cashBoxCount(t, pool, query) != 0 {
			t.Fatal("already delivered historical receipt recreated payment or benefits")
		}
	}
}
