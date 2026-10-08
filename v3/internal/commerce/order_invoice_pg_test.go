//go:build pgintegration

package commerce_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestOrderInvoiceOwnedPaidOrderSelfServiceIssuanceAndDownload(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	if _, err := s.DownloadOrderInvoice(ctx, 1, o.TradeNo); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("unpaid invoice: %v", err)
	}
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DownloadOrderInvoice(ctx, 2, o.TradeNo); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("another user's invoice: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.orders SET amount_minor=9223372036854775807 WHERE id=$1`, o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DownloadOrderInvoice(ctx, 1, o.TradeNo); !errors.Is(err, commerce.ErrInvoiceDetailsRequired) {
		t.Fatalf("invoice without purchaser details: %v", err)
	}
	var balanceBefore, entriesBefore int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(balance),0),(SELECT count(*) FROM v3_billing.ledger_entries) FROM v3_billing.accounts`).Scan(&balanceBefore, &entriesBefore); err != nil {
		t.Fatal(err)
	}
	invoice, err := s.IssueOrderInvoice(ctx, 1, o.TradeNo, purchaser())
	if err != nil || !bytes.Contains(invoice.PDF, []byte("USD 92233720368547758.07")) {
		t.Fatalf("invoice amount lost precision: %v", err)
	}
	for range 2 {
		copy, err := s.DownloadOrderInvoice(ctx, 1, o.TradeNo)
		if err != nil || invoice.Number != copy.Number || !bytes.Equal(invoice.PDF, copy.PDF) {
			t.Fatalf("download retry changed invoice: %v", err)
		}
	}
	var requests, claims int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_commerce.invoices),(SELECT count(*) FROM v3_commerce.invoice_items)`).Scan(&requests, &claims); err != nil || requests != 0 || claims != 0 {
		t.Fatalf("download created manual invoice state: requests=%d claims=%d %v", requests, claims, err)
	}
	var balanceAfter, entriesAfter int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(balance),0),(SELECT count(*) FROM v3_billing.ledger_entries) FROM v3_billing.accounts`).Scan(&balanceAfter, &entriesAfter); err != nil || balanceBefore != balanceAfter || entriesBefore != entriesAfter {
		t.Fatalf("invoice changed money: %d/%d -> %d/%d %v", balanceBefore, entriesBefore, balanceAfter, entriesAfter, err)
	}
	mux := http.NewServeMux()
	s.Register(mux, func(*http.Request) (commerce.Actor, error) { return commerce.Actor{UserID: 1, Role: "user"}, nil })
	reply := httptest.NewRecorder()
	mux.ServeHTTP(reply, httptest.NewRequest(http.MethodGet, "/api/commerce/orders/"+o.TradeNo+"/invoice", nil))
	if reply.Code != http.StatusOK || reply.Header().Get("Content-Type") != "application/pdf" || reply.Header().Get("Cache-Control") != "private, no-store" || reply.Header().Get("Content-Disposition") != `attachment; filename="`+invoice.Number+`.pdf"` || !bytes.Equal(reply.Body.Bytes(), invoice.PDF) {
		t.Fatalf("invoice download failed status=%d headers=%+v", reply.Code, reply.Header())
	}
}

func TestOrderInvoiceRefundsAndPartialRefunds(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	original, err := s.IssueOrderInvoice(ctx, 1, o.TradeNo, purchaser())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmRefund(ctx, "test", o.TradeNo, "invoice-refund"); err != nil {
		t.Fatal(err)
	}
	if saved, err := s.DownloadOrderInvoice(ctx, 1, o.TradeNo); err != nil || !bytes.Equal(saved.PDF, original.PDF) {
		t.Fatalf("refunded original invoice changed or blocked: %v", err)
	}
	// A paid state with confirmed refund progress still retains the original document.
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.orders SET state='paid' WHERE id=$1`, o.ID); err != nil {
		t.Fatal(err)
	}
	if saved, err := s.DownloadOrderInvoice(ctx, 1, o.TradeNo); err != nil || !bytes.Equal(saved.PDF, original.PDF) {
		t.Fatalf("partially refunded original invoice changed or blocked: %v", err)
	}
}

func TestOrderInvoiceFrozenPlanDescriptionAndPendingRefund(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	plan, err := s.SavePlan(ctx, commerce.Plan{Name: "original-plan", PriceMinor: 500, Currency: "usd", Credits: 8_000_000, PeriodSeconds: 60, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	o := create(t, s, plan.ID)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.plans SET name='changed-catalog' WHERE id=$1`, plan.ID); err != nil {
		t.Fatal(err)
	}
	invoice, err := s.IssueOrderInvoice(ctx, 1, o.TradeNo, purchaser())
	var nameBytes []byte
	for _, c := range utf16.Encode([]rune("AI subscription / original-plan")) {
		nameBytes = append(nameBytes, byte(c>>8), byte(c))
	}
	if err != nil || !bytes.Contains(invoice.PDF, []byte(hex.EncodeToString(nameBytes))) {
		t.Fatalf("invoice used mutable plan catalog: %v", err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO v3_commerce.user_refunds
		(refund_no,order_id,user_id,account_id,provider_order_id,gross_minor,fee_minor,amount_minor,refund_credits,reserved_credits,status)
		VALUES('invoice-pending',$1,1,(SELECT account_id FROM v3_commerce.subscriptions WHERE order_id=$1),$2,100,0,100,1,1,'processing')`, o.ID, o.TradeNo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DownloadOrderInvoice(ctx, 1, o.TradeNo); err != nil {
		t.Fatalf("pending refund invoice: %v", err)
	}
	if _, err := s.IssueOrderInvoice(ctx, 1, o.TradeNo, purchaser()); err != nil {
		t.Fatalf("pending refund invoice replay: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.user_refunds SET status='failed' WHERE refund_no='invoice-pending'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DownloadOrderInvoice(ctx, 1, o.TradeNo); err != nil {
		t.Fatalf("failed refund incorrectly blocked invoice: %v", err)
	}
}

func purchaser() commerce.IssueOrderInvoiceInput {
	return commerce.IssueOrderInvoiceInput{BuyerName: "张三 Buyer", BuyerAddress: "Flat 18, Example Street, Kowloon, Hong Kong"}
}

func TestOrderInvoiceConcurrentIssuanceFreezesExactDocument(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(48 * time.Hour)
	const count = 12
	results := make(chan commerce.OrderInvoice, count)
	errorsCh := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() {
			invoice, err := s.IssueOrderInvoice(ctx, 1, o.TradeNo, purchaser())
			results <- invoice
			errorsCh <- err
		})
	}
	wg.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent identical issuance: %v", err)
		}
	}
	var first commerce.OrderInvoice
	for document := range results {
		if first.Number == "" {
			first = document
		}
		if first.Number != document.Number || !bytes.Equal(first.PDF, document.PDF) {
			t.Fatal("concurrent calls returned different documents")
		}
	}
	var issued time.Time
	var storedCount int
	if err := pool.QueryRow(ctx, `SELECT issued_at,(SELECT count(*) FROM v3_commerce.order_invoice_documents) FROM v3_commerce.order_invoice_documents WHERE order_id=$1`, o.ID).Scan(&issued, &storedCount); err != nil || !issued.Equal(*now) || storedCount != 1 {
		t.Fatalf("issue timestamp/count: %v %d %v", issued, storedCount, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET display_name='another name',email='changed@example.test' WHERE id=1;
		UPDATE v3_platform.settings SET value='"Changed seller address"'::jsonb WHERE key='InvoiceSellerAddress'`); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(48 * time.Hour)
	replay, err := s.IssueOrderInvoice(ctx, 1, o.TradeNo, purchaser())
	if err != nil || !bytes.Equal(first.PDF, replay.PDF) {
		t.Fatalf("profile/settings/time changed issued document: %v", err)
	}
	changed := purchaser()
	changed.BuyerAddress = "Another address"
	if _, err := s.IssueOrderInvoice(ctx, 1, o.TradeNo, changed); !errors.Is(err, commerce.ErrInvoiceDetailsConflict) {
		t.Fatalf("changed purchaser not rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM v3_platform.settings WHERE key='InvoiceSellerAddress'`); err != nil {
		t.Fatal(err)
	}
	replay, err = s.DownloadOrderInvoice(ctx, 1, o.TradeNo)
	if err != nil || !bytes.Equal(first.PDF, replay.PDF) {
		t.Fatalf("missing new configuration broke historical document: %v", err)
	}
}

func TestOrderInvoiceMissingSellerDetailsRefuseIssuanceAndHTTPExplains(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM v3_platform.settings WHERE key='InvoiceSellerAddress'`); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux, func(*http.Request) (commerce.Actor, error) { return commerce.Actor{UserID: 1, Role: "root"}, nil })
	for _, test := range []struct {
		method, body, message string
		status                int
	}{
		{http.MethodGet, "", "请先填写发票抬头和购买方地址", http.StatusPreconditionRequired},
		{http.MethodPost, `{"buyer_name":"Buyer","buyer_address":"Street"}`, "开票主体地址未配置，请联系平台", http.StatusServiceUnavailable},
		{http.MethodPost, `{"buyer_name":"Buyer"}`, "invalid request", http.StatusBadRequest},
	} {
		reply := httptest.NewRecorder()
		mux.ServeHTTP(reply, httptest.NewRequest(test.method, "/api/commerce/orders/"+o.TradeNo+"/invoice", strings.NewReader(test.body)))
		if reply.Code != test.status || !strings.Contains(reply.Body.String(), test.message) {
			t.Fatalf("response %d %s", reply.Code, reply.Body.String())
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.order_invoice_documents`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed issuance wrote document: %d %v", count, err)
	}
}
