//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestInvoicePartialRefundPreventsRequestAndPendingIssuance(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	first, second := create(t, s, 0), create(t, s, 0)
	for _, o := range []commerce.Order{first, second} {
		if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
			t.Fatal(err)
		}
	}
	request, err := s.CreateInvoiceRequest(ctx, 1, invoiceInput(second))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []commerce.Order{first, second} {
		if err := s.ConfirmRefundTotal(ctx, "test", o.TradeNo, "partial-"+o.TradeNo, o.Currency, 100); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.CreateInvoiceRequest(ctx, 1, invoiceInput(first)); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("partially refunded request: %v", err)
	}
	eligible, err := s.ListInvoiceEligibleOrders(ctx, 1)
	if err != nil || len(eligible) != 0 {
		t.Fatalf("partially refunded eligibility: %+v err=%v", eligible, err)
	}
	if _, err = s.UpdateAdminInvoiceRequest(ctx, request.ID, 2, commerce.UpdateInvoiceRequestInput{Status: "issued", InvoiceNumber: "INV-P"}); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("partially refunded issuance: %v", err)
	}
}

func TestInvoiceUserRefundReservationBlocksClaimUntilFailure(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	_, err := pool.Exec(ctx, `INSERT INTO v3_commerce.user_refunds
		(refund_no,order_id,user_id,account_id,provider_order_id,gross_minor,fee_minor,amount_minor,refund_credits,reserved_credits,status)
		SELECT 'refund-invoice-reserved',$1,1,id,'provider-order',100,0,100,1000000,1000000,'processing'
		FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=1 AND kind='wallet'`, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"processing", "success"} {
		if _, err = pool.Exec(ctx, `UPDATE v3_commerce.user_refunds SET status=$1 WHERE order_id=$2`, state, o.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = s.CreateInvoiceRequest(ctx, 1, invoiceInput(o)); !errors.Is(err, commerce.ErrNotFound) {
			t.Fatalf("%s refund allowed claim: %v", state, err)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.user_refunds SET status='failed' WHERE order_id=$1`, o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateInvoiceRequest(ctx, 1, invoiceInput(o)); err != nil {
		t.Fatalf("failed refund left order blocked: %v", err)
	}
}
