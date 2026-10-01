//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestInvoiceOverlappingBatchesReserveAllOrdersOnce(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	first, second := create(t, s, 0), create(t, s, 0)
	for _, o := range []commerce.Order{first, second} {
		if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, input := range []commerce.CreateInvoiceRequestInput{invoiceInput(first, second), invoiceInput(second, first)} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateInvoiceRequest(ctx, 1, input)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, commerce.ErrStateConflict) {
			t.Fatal(err)
		}
	}
	var invoices, items, amount int64
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_commerce.invoices),
		(SELECT count(*) FROM v3_commerce.invoice_items),(SELECT sum(order_amount_minor) FROM v3_commerce.invoices)`).
		Scan(&invoices, &items, &amount); err != nil || success != 1 || invoices != 1 || items != 2 || amount != 2400 {
		t.Fatalf("winners=%d invoices=%d items=%d amount=%d err=%v", success, invoices, items, amount, err)
	}
}
