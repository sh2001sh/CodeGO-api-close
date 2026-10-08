//go:build pgintegration

package commerce_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestOrderInvoiceCorrectionsRetainOriginalAndStableRequestIdentity(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	in := purchaser()
	in.BuyerCountry = "Hong Kong"
	in.BuyerTaxID = "Buyer-123"
	original, err := s.IssueOrderInvoice(ctx, 1, o.TradeNo, in)
	if err != nil {
		t.Fatal(err)
	}
	var balanceBefore, entriesBefore int64
	if err = pool.QueryRow(ctx, `SELECT sum(balance),(SELECT count(*) FROM v3_billing.ledger_entries) FROM v3_billing.accounts`).Scan(&balanceBefore, &entriesBefore); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_platform.settings SET value='"Changed seller"' WHERE key='InvoiceSellerAddress'`); err != nil {
		t.Fatal(err)
	}
	in.BuyerName = "Correct Buyer"
	correction := commerce.CorrectOrderInvoiceInput{IssueOrderInvoiceInput: in, PreviousNumber: original.Number, RequestID: "buyer-fix-1", Reason: "Correct legal buyer name"}
	current, err := s.CorrectOrderInvoice(ctx, 1, o.TradeNo, correction)
	if err != nil || current.Number == original.Number {
		t.Fatalf("correction %s %v", current.Number, err)
	}
	replay, err := s.CorrectOrderInvoice(ctx, 1, o.TradeNo, correction)
	if err != nil || !bytes.Equal(current.PDF, replay.PDF) {
		t.Fatalf("idempotency %v", err)
	}
	bad := correction
	bad.Reason = "Different reason"
	if _, err = s.CorrectOrderInvoice(ctx, 1, o.TradeNo, bad); !errors.Is(err, commerce.ErrInvoiceDetailsConflict) {
		t.Fatalf("request ID mutation: %v", err)
	}
	bad.RequestID = "new-request"
	if _, err = s.CorrectOrderInvoice(ctx, 1, o.TradeNo, bad); !errors.Is(err, commerce.ErrInvoiceDetailsConflict) {
		t.Fatalf("stale version: %v", err)
	}
	correction2 := correction
	correction2.PreviousNumber = current.Number
	correction2.RequestID = "buyer-fix-2"
	correction2.BuyerAddress = "Corrected street"
	latest, err := s.CorrectOrderInvoice(ctx, 1, o.TradeNo, correction2)
	if err != nil {
		t.Fatal(err)
	}
	replay, err = s.CorrectOrderInvoice(ctx, 1, o.TradeNo, correction)
	if err != nil || !bytes.Equal(current.PDF, replay.PDF) {
		t.Fatalf("historical retry %v", err)
	}
	retrieved, err := s.DownloadOrderInvoiceNumber(ctx, 1, o.TradeNo, original.Number)
	if err != nil || !bytes.Equal(original.PDF, retrieved.PDF) {
		t.Fatalf("original mutated %v", err)
	}
	retrieved, err = s.DownloadOrderInvoice(ctx, 1, o.TradeNo)
	if err != nil || !bytes.Equal(latest.PDF, retrieved.PDF) {
		t.Fatalf("latest %v", err)
	}
	page, err := s.ListOrderInvoiceDocuments(ctx, 1, o.TradeNo, 1, 2)
	if err != nil || page.Total != 3 || len(page.Items) != 2 {
		t.Fatalf("page %+v %v", page, err)
	}
	var seller string
	if err = pool.QueryRow(ctx, `SELECT seller_address FROM v3_commerce.order_invoice_revisions WHERE number=$1`, latest.Number).Scan(&seller); err != nil || seller == "Changed seller" {
		t.Fatalf("seller not frozen %q %v", seller, err)
	}
	var balanceAfter, entriesAfter int64
	if err = pool.QueryRow(ctx, `SELECT sum(balance),(SELECT count(*) FROM v3_billing.ledger_entries) FROM v3_billing.accounts`).Scan(&balanceAfter, &entriesAfter); err != nil || balanceBefore != balanceAfter || entriesBefore != entriesAfter {
		t.Fatalf("correction changed money %d/%d %d/%d %v", balanceBefore, entriesBefore, balanceAfter, entriesAfter, err)
	}
}

func TestOrderInvoiceConcurrentCorrectionsAndReplay(t *testing.T) {
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
	input := commerce.CorrectOrderInvoiceInput{IssueOrderInvoiceInput: purchaser(), PreviousNumber: original.Number, RequestID: "concurrent-fix", Reason: "Address typo"}
	input.BuyerAddress = "Updated address"
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	docs := make(chan commerce.OrderInvoice, 8)
	for range 8 {
		wg.Go(func() { d, err := s.CorrectOrderInvoice(ctx, 1, o.TradeNo, input); errs <- err; docs <- d })
	}
	wg.Wait()
	close(errs)
	close(docs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first commerce.OrderInvoice
	for d := range docs {
		if first.Number == "" {
			first = d
		}
		if d.Number != first.Number || !bytes.Equal(d.PDF, first.PDF) {
			t.Fatal("concurrent corrections not identical")
		}
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.order_invoice_revisions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d %v", count, err)
	}
	// Two different users' edits based on the same previous number cannot both win.
	input.PreviousNumber = first.Number
	input.RequestID = "another-1"
	input.BuyerName = "Second buyer"
	other := input
	other.RequestID = "another-2"
	other.BuyerName = "Third buyer"
	results := make(chan error, 2)
	for _, v := range []commerce.CorrectOrderInvoiceInput{input, other} {
		wg.Go(func() { _, err := s.CorrectOrderInvoice(ctx, 1, o.TradeNo, v); results <- err })
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, commerce.ErrInvoiceDetailsConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}

func TestOrderInvoiceCreditNotesConfirmedDeltasAndNoOverlapDoubleCount(t *testing.T) {
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
	if _, err = s.DownloadOrderCreditNote(ctx, 1, o.TradeNo); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("no refund note %v", err)
	}
	if err = s.ConfirmRefundTotal(ctx, "test", o.TradeNo, "part-1", o.Currency, 300); err != nil {
		t.Fatal(err)
	}
	first, err := s.DownloadOrderCreditNote(ctx, 1, o.TradeNo)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.DownloadOrderCreditNote(ctx, 1, o.TradeNo)
	if err != nil || !bytes.Equal(first.PDF, replay.PDF) {
		t.Fatalf("note retry %v", err)
	}
	if err = s.ConfirmRefundTotal(ctx, "test", o.TradeNo, "part-2", o.Currency, 700); err != nil {
		t.Fatal(err)
	}
	second, err := s.DownloadOrderCreditNote(ctx, 1, o.TradeNo)
	if err != nil || second.Number == first.Number {
		t.Fatalf("next note %v", err)
	}
	if err = s.ConfirmRefund(ctx, "test", o.TradeNo, "part-3"); err != nil {
		t.Fatal(err)
	}
	// Success in the second source describes the same confirmed total, not a second refund.
	_, err = pool.Exec(ctx, `INSERT INTO v3_commerce.user_refunds(refund_no,order_id,user_id,account_id,provider_order_id,gross_minor,fee_minor,amount_minor,refund_credits,reserved_credits,status)
 VALUES('overlap',$1,1,(SELECT id FROM v3_billing.accounts WHERE owner_id=1 AND kind='wallet'),$2,1200,0,1200,1,0,'success')`, o.ID, o.TradeNo)
	if err != nil {
		t.Fatal(err)
	}
	last, err := s.DownloadOrderCreditNote(ctx, 1, o.TradeNo)
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	var count int
	if err = pool.QueryRow(ctx, `SELECT sum(amount_minor),count(*) FROM v3_commerce.order_invoice_revisions WHERE document_type='credit_note'`).Scan(&total, &count); err != nil || total != 1200 || count != 3 {
		t.Fatalf("credit total=%d count=%d %v", total, count, err)
	}
	for _, d := range []commerce.OrderInvoice{original, first, second, last} {
		copy, err := s.DownloadOrderInvoiceNumber(ctx, 1, o.TradeNo, d.Number)
		if err != nil || !bytes.Equal(d.PDF, copy.PDF) {
			t.Fatalf("refund changed historic PDF %s %v", d.Number, err)
		}
	}
	correction := commerce.CorrectOrderInvoiceInput{IssueOrderInvoiceInput: purchaser(), PreviousNumber: original.Number, RequestID: "after-refund", Reason: "Edit"}
	correction.BuyerName = "Other"
	if _, err = s.CorrectOrderInvoice(ctx, 1, o.TradeNo, correction); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("corrected refunded %v", err)
	}
	if _, err = s.DownloadOrderCreditNote(ctx, 2, o.TradeNo); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("foreign note %v", err)
	}
}

func TestOrderInvoicePendingRefundDoesNotCreateCreditNote(t *testing.T) {
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
	_, err = pool.Exec(ctx, `INSERT INTO v3_commerce.user_refunds(refund_no,order_id,user_id,account_id,provider_order_id,gross_minor,fee_minor,amount_minor,refund_credits,reserved_credits,status)
 VALUES('pending-note',$1,1,(SELECT id FROM v3_billing.accounts WHERE owner_id=1 AND kind='wallet'),$2,200,0,200,1,1,'processing')`, o.ID, o.TradeNo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DownloadOrderCreditNote(ctx, 1, o.TradeNo); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("pending became confirmed %v", err)
	}
	copy, err := s.DownloadOrderInvoice(ctx, 1, o.TradeNo)
	if err != nil || !bytes.Equal(original.PDF, copy.PDF) {
		t.Fatalf("pending blocked original %v", err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.order_invoice_revisions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unconfirmed wrote note %d %v", count, err)
	}
}

func TestOrderInvoiceDocumentHTTPAndValidation(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	in := purchaser()
	in.BuyerTaxID = strings.Repeat("a", 65)
	if _, err := s.IssueOrderInvoice(ctx, 1, o.TradeNo, in); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("tax ID limit %v", err)
	}
	in.BuyerTaxID = "valid"
	in.BuyerCountry = strings.Repeat("界", 101)
	if _, err := s.IssueOrderInvoice(ctx, 1, o.TradeNo, in); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("country limit %v", err)
	}
	original, err := s.IssueOrderInvoice(ctx, 1, o.TradeNo, purchaser())
	if err != nil {
		t.Fatal(err)
	}
	actor := commerce.Actor{UserID: 1, Role: "user"}
	mux := http.NewServeMux()
	s.Register(mux, func(*http.Request) (commerce.Actor, error) { return actor, nil })
	call := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	base := "/api/commerce/orders/" + o.TradeNo
	w := call(http.MethodGet, base+"/invoice/documents", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), original.Number) {
		t.Fatalf("history %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("buyer document history must not enter HTTP caches")
	}
	w = call(http.MethodGet, base+"/invoice/documents?page=0", "")
	if w.Code != 400 {
		t.Fatalf("invalid page %d", w.Code)
	}
	w = call(http.MethodGet, base+"/invoice?number="+original.Number, "")
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), original.PDF) {
		t.Fatalf("number download %d", w.Code)
	}
	w = call(http.MethodPost, base+"/invoice/corrections", `{"buyer_name":"New","buyer_address":"Street","previous_number":"`+original.Number+`","reason":"Fix"}`)
	if w.Code != 400 {
		t.Fatalf("missing identity %d", w.Code)
	}
	actor = commerce.Actor{UserID: 2, Role: "root"}
	for _, path := range []string{"/invoice/documents", "/invoice?number=" + original.Number} {
		w = call(http.MethodGet, base+path, "")
		if w.Code != 404 {
			t.Fatalf("foreign %s %d", path, w.Code)
		}
	}
	actor = commerce.Actor{}
	w = call(http.MethodPost, base+"/credit-note", "")
	if w.Code != 401 {
		t.Fatalf("anonymous credit note %d", w.Code)
	}
	w = call(http.MethodGet, base+"/invoice/documents", "")
	if w.Code != 401 {
		t.Fatalf("anonymous %d", w.Code)
	}
	actor = commerce.Actor{UserID: 2, Role: "root"}
	w = call(http.MethodPost, base+"/credit-note", "")
	if w.Code != 404 {
		t.Fatalf("foreign credit note %d", w.Code)
	}
	actor = commerce.Actor{UserID: 1, Role: "user"}
	for _, body := range []string{`{"amount_minor":1}`, `{"refund":true}`, `null`, `[]`, `{}`, ""} {
		w = call(http.MethodPost, base+"/credit-note", body)
		want := 409
		if body != "{}" && body != "" {
			want = 400
		}
		if w.Code != want {
			t.Fatalf("credit note request body %s status=%d", body, w.Code)
		}
	}
}

func TestOrderInvoiceUnsupportedCharactersDoNotPersistOriginalOrCorrection(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	in := purchaser()
	in.BuyerName = "Buyer 😀"
	if _, err := s.IssueOrderInvoice(ctx, 1, o.TradeNo, in); !errors.Is(err, commerce.ErrInvoiceCharactersUnsupported) || !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("unsupported original %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.order_invoice_documents`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed font saved original %d %v", count, err)
	}
	original, err := s.IssueOrderInvoice(ctx, 1, o.TradeNo, purchaser())
	if err != nil {
		t.Fatal(err)
	}
	correction := commerce.CorrectOrderInvoiceInput{IssueOrderInvoiceInput: in, PreviousNumber: original.Number, RequestID: "unsupported-fix", Reason: "Correct name"}
	if _, err = s.CorrectOrderInvoice(ctx, 1, o.TradeNo, correction); !errors.Is(err, commerce.ErrInvoiceCharactersUnsupported) {
		t.Fatalf("unsupported correction %v", err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.order_invoice_revisions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed font saved correction %d %v", count, err)
	}
	latest, err := s.DownloadOrderInvoice(ctx, 1, o.TradeNo)
	if err != nil || !bytes.Equal(original.PDF, latest.PDF) {
		t.Fatalf("invalid render replaced document %v", err)
	}
	mux := http.NewServeMux()
	s.Register(mux, func(*http.Request) (commerce.Actor, error) { return commerce.Actor{UserID: 1, Role: "user"}, nil })
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/commerce/orders/"+o.TradeNo+"/invoice/corrections", strings.NewReader(`{"buyer_name":"Buyer 😀","buyer_address":"Street","previous_number":"`+original.Number+`","request_id":"emoji-http","reason":"Correct name"}`)))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "暂不支持") {
		t.Fatalf("unsupported HTTP %d %s", w.Code, w.Body.String())
	}
}
