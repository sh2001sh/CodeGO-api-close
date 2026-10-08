//go:build pgintegration

package commerce_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestOrderInvoiceHTTPPostAndGetEnforceOwnershipAndPaymentState(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	actor := commerce.Actor{UserID: 1, Role: "user"}
	mux := http.NewServeMux()
	s.Register(mux, func(*http.Request) (commerce.Actor, error) { return actor, nil })
	body, err := json.Marshal(purchaser())
	if err != nil {
		t.Fatal(err)
	}
	call := func(method string) *httptest.ResponseRecorder {
		reply := httptest.NewRecorder()
		mux.ServeHTTP(reply, httptest.NewRequest(method, "/api/commerce/orders/"+o.TradeNo+"/invoice", bytes.NewReader(body)))
		return reply
	}
	checkDenied := func(status int) {
		t.Helper()
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			response := call(method)
			if response.Code != status || response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("%s status %d body %s", method, response.Code, response.Body.String())
			}
		}
	}
	checkDenied(http.StatusConflict) // unpaid
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	actor = commerce.Actor{UserID: 2, Role: "root"}
	checkDenied(http.StatusNotFound) // administrators still cannot claim foreign orders
	actor = commerce.Actor{}
	checkDenied(http.StatusUnauthorized)
	actor = commerce.Actor{UserID: 1, Role: "user"}
	issued := call(http.MethodPost)
	if issued.Code != http.StatusOK || issued.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("POST issuance %d %s", issued.Code, issued.Body.String())
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		replay := call(method)
		if replay.Code != http.StatusOK || !bytes.Equal(issued.Body.Bytes(), replay.Body.Bytes()) {
			t.Fatalf("%s replay changed issued PDF", method)
		}
	}
	if err := s.ConfirmRefund(ctx, "test", o.TradeNo, "invoice-http-refund"); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		replay := call(method)
		if replay.Code != http.StatusOK || !bytes.Equal(issued.Body.Bytes(), replay.Body.Bytes()) {
			t.Fatalf("%s refund blocked original PDF", method)
		}
	}
}

func TestOrderInvoiceRacingDifferentPurchasersAndMultilineAddress(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	inputs := []commerce.IssueOrderInvoiceInput{
		{BuyerName: "First buyer", BuyerAddress: "Flat 18\r\nHong Kong"},
		{BuyerName: "Second buyer", BuyerAddress: "Flat 19\nHong Kong"},
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, input := range inputs {
		wg.Go(func() { _, err := s.IssueOrderInvoice(ctx, 1, o.TradeNo, input); results <- err })
	}
	wg.Wait()
	close(results)
	var successes, conflicts int
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, commerce.ErrInvoiceDetailsConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("different purchasers successes=%d conflicts=%d", successes, conflicts)
	}
	var address string
	if err := pool.QueryRow(ctx, `SELECT buyer_address FROM v3_commerce.order_invoice_documents WHERE order_id=$1`, o.ID).Scan(&address); err != nil || strings.Contains(address, "\r") || !strings.Contains(address, "\n") {
		t.Fatalf("multiline address canonicalization: %q %v", address, err)
	}
}

func TestOrderInvoiceRejectsInvalidSellerConfiguration(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`" "`, `{}`, `null`, `"Street\u0001Number"`} {
		if _, err := pool.Exec(ctx, `UPDATE v3_platform.settings SET value=$1 WHERE key='InvoiceSellerAddress'`, raw); err != nil {
			t.Fatal(err)
		}
		if _, err := s.IssueOrderInvoice(ctx, 1, o.TradeNo, purchaser()); !errors.Is(err, commerce.ErrInvoiceSellerMissing) {
			t.Fatalf("invalid seller %s: %v", raw, err)
		}
	}
}
