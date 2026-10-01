//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func invoiceInput(orders ...commerce.Order) commerce.CreateInvoiceRequestInput {
	input := commerce.CreateInvoiceRequestInput{InvoiceType: "personal", Title: "测试抬头", Email: "payer@example.test"}
	for _, o := range orders {
		input.Orders = append(input.Orders, commerce.InvoiceOrderInput{SourceType: o.Kind, TradeNo: o.TradeNo})
	}
	return input
}

func TestInvoiceConcurrentClaimsExactlyOneRequest(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 20)
	input := invoiceInput(o)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateInvoiceRequest(ctx, 1, input)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, commerce.ErrStateConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	var requests, items int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_commerce.invoices),(SELECT count(*) FROM v3_commerce.invoice_items)`).Scan(&requests, &items); err != nil {
		t.Fatal(err)
	}
	if success != 1 || conflict != 19 || requests != 1 || items != 1 {
		t.Fatalf("success=%d conflicts=%d requests=%d items=%d", success, conflict, requests, items)
	}
	eligible, err := s.ListInvoiceEligibleOrders(ctx, 1)
	if err != nil || len(eligible) != 1 || !eligible[0].Requested || eligible[0].OrderAmount.String() != "12.00" {
		t.Fatalf("eligible=%+v err=%v", eligible, err)
	}
	other, err := s.ListInvoiceRequests(ctx, 2, "", 1, 100)
	if err != nil || other.Total != 0 || len(other.Items) != 0 {
		t.Fatalf("foreign requests disclosed: %+v err=%v", other, err)
	}
}

func TestInvoiceBatchRollbackAndImmutableAmounts(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	first, second := create(t, s, 0), create(t, s, 0)
	for _, o := range []commerce.Order{first, second} {
		if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateInvoiceRequest(ctx, 2, invoiceInput(first)); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("foreign request: %v", err)
	}
	unpaid := create(t, s, 0)
	if _, err := s.CreateInvoiceRequest(ctx, 1, invoiceInput(first, unpaid)); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("unpaid batch: %v", err)
	}
	var requests, claims int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_commerce.invoices),(SELECT count(*) FROM v3_commerce.invoice_items)`).Scan(&requests, &claims); err != nil || requests != 0 || claims != 0 {
		t.Fatalf("failed batch leaked requests=%d claims=%d err=%v", requests, claims, err)
	}
	result, err := s.CreateInvoiceRequest(ctx, 1, invoiceInput(first, second))
	if err != nil || result.SourceType != "batch" || result.OrderCount != 2 || result.OrderAmountMinor != 2400 || result.OrderAmount.String() != "24.00" || result.Status != "pending" {
		t.Fatalf("batch=%+v err=%v", result, err)
	}
	if err := s.ConfirmRefund(ctx, "test", first.TradeNo, "refund-invoice"); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListInvoiceRequests(ctx, 1, "pending", 1, 20)
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].OrderAmountMinor != 2400 {
		t.Fatalf("snapshot mutated: %+v err=%v", page, err)
	}
	if _, err = s.UpdateAdminInvoiceRequest(ctx, result.ID, 2, commerce.UpdateInvoiceRequestInput{Status: "issued", InvoiceNumber: "INV-1"}); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("refunded batch was issued: %v", err)
	}
}

func TestInvoiceRejectsRefundedMixedCurrencyAndOverflowOrders(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	first, second := create(t, s, 0), create(t, s, 0)
	for _, o := range []commerce.Order{first, second} {
		if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.orders SET currency='cny' WHERE id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateInvoiceRequest(ctx, 1, invoiceInput(first, second)); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("mixed currency: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.orders SET currency='usd',amount_minor=9223372036854775807 WHERE id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateInvoiceRequest(ctx, 1, invoiceInput(first, second)); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("overflow: %v", err)
	}
	if err := s.ConfirmRefund(ctx, "test", first.TradeNo, "refund-before-invoice"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateInvoiceRequest(ctx, 1, invoiceInput(first)); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("refunded request: %v", err)
	}
}

func TestInvoiceManualOutcomeIsGuardedAndIdempotent(t *testing.T) {
	s, _, now := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	request, err := s.CreateInvoiceRequest(ctx, 1, invoiceInput(o))
	if err != nil {
		t.Fatal(err)
	}
	issued := commerce.UpdateInvoiceRequestInput{Status: "issued", InvoiceNumber: "INV-1", AdminNote: "sent"}
	for range 2 {
		result, err := s.UpdateAdminInvoiceRequest(ctx, request.ID, 2, issued)
		if err != nil || result.Status != "issued" || result.HandledBy != 2 || result.IssuedAt != now.Unix() {
			t.Fatalf("issued=%+v err=%v", result, err)
		}
	}
	if _, err = s.UpdateAdminInvoiceRequest(ctx, request.ID, 2, commerce.UpdateInvoiceRequestInput{Status: "rejected", AdminNote: "changed"}); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("issued transition: %v", err)
	}
	issued.InvoiceNumber = "INV-2"
	if _, err = s.UpdateAdminInvoiceRequest(ctx, request.ID, 2, issued); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("issued number mutation: %v", err)
	}
	o = create(t, s, 0)
	if err = s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	request, err = s.CreateInvoiceRequest(ctx, 1, invoiceInput(o))
	if err != nil {
		t.Fatal(err)
	}
	reject := commerce.UpdateInvoiceRequestInput{Status: "rejected", AdminNote: "missing tax detail"}
	for range 2 {
		result, err := s.UpdateAdminInvoiceRequest(ctx, request.ID, 2, reject)
		if err != nil || result.Status != "rejected" || result.IssuedAt != 0 {
			t.Fatalf("rejected=%+v err=%v", result, err)
		}
	}
	if _, err = s.CreateInvoiceRequest(ctx, 1, invoiceInput(o)); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("rejected order claim released: %v", err)
	}
}
