//go:build pgintegration

package commerce_test

import (
	"context"
	"encoding/json"
	"testing"
)

func TestHostedCallbackResolvesOnlyStoredSignedProviderReference(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	e := payment(o)
	e.TradeNo = ""
	body, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleWebhook(ctx, "test", nil, body); err != nil {
		t.Fatal(err)
	}
	result, err := s.GetOrder(ctx, 1, o.TradeNo)
	if err != nil || result.State != "paid" {
		t.Fatalf("state=%s err=%v", result.State, err)
	}
	e.ID = "unknown_event"
	e.Reference = "unknown_provider_order"
	body, _ = json.Marshal(e)
	if err = s.HandleWebhook(ctx, "test", nil, body); err == nil {
		t.Fatal("unknown provider order callback accepted")
	}
}

func TestSignedFailureTracksOrderAndCannotUndoCompletedPayment(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	e := payment(o)
	e.ID = "failed_" + o.TradeNo
	e.Paid = false
	e.State = "failed"
	body, _ := json.Marshal(e)
	if err := s.HandleWebhook(ctx, "test", nil, body); err != nil {
		t.Fatal(err)
	}
	result, err := s.GetOrder(ctx, 1, o.TradeNo)
	if err != nil || result.State != "failed" {
		t.Fatalf("state=%s err=%v", result.State, err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_billing.ledger_entries`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed payment posted entries=%d err=%v", count, err)
	}
	if err = s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	if err = s.HandleWebhook(ctx, "test", nil, body); err != nil {
		t.Fatal(err)
	}
	result, err = s.GetOrder(ctx, 1, o.TradeNo)
	if err != nil || result.State != "paid" {
		t.Fatalf("stale failure reversed payment: %s err=%v", result.State, err)
	}
}
