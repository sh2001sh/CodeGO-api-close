package app

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/stretchr/testify/require"
)

func TestCommercialInvoiceBatchPostgresConcurrentClaims(t *testing.T) {
	db := setupEpayPostgresTestDB(t)
	require.NoError(t, db.AutoMigrate(&commerceschema.SubscriptionOrder{}, &commerceschema.SubscriptionPlan{}, &commerceschema.InvoiceRequest{}, &commerceschema.InvoiceRequestItem{}))
	orders := make([]commerceschema.TopUp, 3)
	for i := range orders {
		orders[i] = commerceschema.TopUp{UserId: 4, TradeNo: fmt.Sprintf("concurrent-batch-%d", i), Money: float64(i+1) * 10, Status: "success", CompleteTime: 1792000000, PaymentProvider: "epay"}
	}
	require.NoError(t, db.Create(&orders).Error)
	input := func(a, b int) CommercialInvoiceInput {
		return CommercialInvoiceInput{InvoiceBuyer: InvoiceBuyer{BuyerName: "Fixture buyer", BuyerAddress: "Hong Kong"}, Orders: []InvoiceOrderInput{{SourceType: "topup", TradeNo: orders[a].TradeNo}, {SourceType: "topup", TradeNo: orders[b].TradeNo}}}
	}
	var pdfs sync.Map
	results := commercialConcurrentCalls(12, func(i int) error {
		buyer := input(i%2, 1-i%2)
		docs, err := CommercialInvoiceBatch(4, &buyer)
		if err == nil {
			pdfs.Store(i, docs[0].PDF)
		}
		return err
	})
	for _, err := range results {
		require.NoError(t, err)
	}
	var original []byte
	pdfs.Range(func(_, value any) bool {
		if original == nil {
			original = value.([]byte)
		} else {
			require.True(t, bytes.Equal(original, value.([]byte)))
		}
		return true
	})
	var count int64
	require.NoError(t, db.Model(&commerceschema.InvoiceRequest{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	// Competing sets overlap on a claimed member; no order may be claimed twice.
	results = commercialConcurrentCalls(12, func(i int) error {
		buyer := input(i%2, 2)
		_, err := CommercialInvoiceBatch(4, &buyer)
		if errors.Is(err, ErrCommercialInvoiceConflict) {
			return nil
		}
		if err == nil {
			return errors.New("overlapping batch issued")
		}
		return err
	})
	for _, err := range results {
		require.NoError(t, err)
	}
	require.NoError(t, db.Model(&commerceschema.InvoiceRequestItem{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
	// A new single-order claim racing a batch is serialized by the same payment
	// lock. Whichever wins, the shared payment belongs to exactly one invoice.
	for i := 0; i < 2; i++ {
		order := commerceschema.TopUp{UserId: 4, TradeNo: fmt.Sprintf("race-member-%d", i), Money: 10, Status: "success", CompleteTime: 1792000000, PaymentProvider: "epay"}
		require.NoError(t, db.Create(&order).Error)
	}
	results = commercialConcurrentCalls(12, func(i int) error {
		buyer := CommercialInvoiceInput{InvoiceBuyer: InvoiceBuyer{BuyerName: "Fixture buyer", BuyerAddress: "Hong Kong"}}
		var err error
		if i%2 == 0 {
			buyer.Orders = []InvoiceOrderInput{{SourceType: "topup", TradeNo: "race-member-0"}, {SourceType: "topup", TradeNo: "race-member-1"}}
			_, err = CommercialInvoiceBatch(4, &buyer)
		} else {
			_, err = CommercialInvoice(4, "topup", "race-member-0", "issue", "", &buyer)
		}
		if err == nil || errors.Is(err, ErrCommercialInvoiceConflict) {
			return nil
		}
		return err
	})
	for _, err := range results {
		require.NoError(t, err)
	}
	require.NoError(t, db.Model(&commerceschema.InvoiceRequestItem{}).Where("trade_no = ?", "race-member-0").Count(&count).Error)
	require.EqualValues(t, 1, count)
	t.Log("PostgreSQL: 12 identical/reordered claims return one PDF; overlapping claims and batch/single races cannot invoice a payment twice")
}
