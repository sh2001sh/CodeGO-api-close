package http

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/sh2001sh/new-api/constant"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
)

func TestCommercialInvoiceBatchLifecycle(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	orders := []commerceschema.TopUp{
		{UserId: 4, TradeNo: "batch-paid-a", Status: constant.TopUpStatusSuccess, Money: 12.50, CompleteTime: 1792000000, PaymentProvider: "epay"},
		{UserId: 4, TradeNo: "batch-paid-b", Status: constant.TopUpStatusSuccess, Money: 18, CompleteTime: 1792000100, PaymentProvider: "epay"},
		{UserId: 4, TradeNo: "batch-paid-c", Status: constant.TopUpStatusSuccess, Money: 10, CompleteTime: 1792000200, PaymentProvider: "epay"},
	}
	if err := db.Create(&orders).Error; err != nil {
		t.Fatal(err)
	}
	buyer := commerceapp.CommercialInvoiceInput{InvoiceBuyer: commerceapp.InvoiceBuyer{BuyerName: "合并开票买方", BuyerAddress: "Hong Kong"}, Orders: []commerceapp.InvoiceOrderInput{{SourceType: "topup", TradeNo: orders[1].TradeNo}, {SourceType: "topup", TradeNo: orders[0].TradeNo}}}
	ctx, rec := newCommerceContext(t, http.MethodPost, "/api/invoices/batches/document", buyer, 4)
	commercialInvoiceHandler("batch")(ctx)
	if rec.Code != 200 || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF-1.4")) || !bytes.Contains(rec.Body.Bytes(), []byte("CNY 30.50")) {
		t.Fatalf("batch issue: %d %s", rec.Code, rec.Body.String())
	}
	original := append([]byte(nil), rec.Body.Bytes()...)
	for _, order := range orders[:2] {
		var encoded []byte
		for _, r := range utf16.Encode([]rune(order.TradeNo)) {
			encoded = append(encoded, byte(r>>8), byte(r))
		}
		if !bytes.Contains(original, []byte(strings.ToUpper(hex.EncodeToString(encoded)))) && !bytes.Contains(original, []byte(hex.EncodeToString(encoded))) {
			t.Fatalf("PDF omitted order %s", order.TradeNo)
		}
	}
	var stored commerceschema.InvoiceRequest
	if err := db.First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SourceType != "batch" || stored.OrderCount != 2 || stored.OrderAmount != 30.5 {
		t.Fatalf("unexpected invoice: %#v", stored)
	}
	var items []commerceschema.InvoiceRequestItem
	if err := db.Where("invoice_id = ?", stored.ID).Find(&items).Error; err != nil || len(items) != 2 {
		t.Fatalf("items: %v %v", items, err)
	}
	// Reordering selections and reloading either member return the identical file.
	buyer.Orders[0], buyer.Orders[1] = buyer.Orders[1], buyer.Orders[0]
	repeated, err := commerceapp.CommercialInvoiceBatch(4, &buyer)
	if err != nil || !bytes.Equal(repeated[0].PDF, original) {
		t.Fatalf("batch retry: %v", err)
	}
	for _, order := range orders[:2] {
		docs, err := commerceapp.CommercialInvoice(4, "topup", order.TradeNo, "download", "", nil)
		if err != nil || !bytes.Equal(docs[0].PDF, original) {
			t.Fatalf("member download: %v", err)
		}
		if _, err := commerceapp.CommercialInvoice(5, "topup", order.TradeNo, "download", "", nil); err == nil {
			t.Fatal("foreign owner downloaded batch")
		}
	}
	overlap := buyer
	overlap.Orders = []commerceapp.InvoiceOrderInput{{SourceType: "topup", TradeNo: orders[0].TradeNo}, {SourceType: "topup", TradeNo: orders[2].TradeNo}}
	if _, err := commerceapp.CommercialInvoiceBatch(4, &overlap); !errors.Is(err, commerceapp.ErrCommercialInvoiceConflict) {
		t.Fatalf("overlapping invoice: %v", err)
	}
	corrected := commerceapp.CommercialInvoiceInput{InvoiceBuyer: commerceapp.InvoiceBuyer{BuyerName: "Corrected buyer", BuyerAddress: "Hong Kong"}, PreviousNumber: stored.InvoiceNumber, Reason: "Correct legal name", RequestID: "batch-correction-1"}
	docs, err := commerceapp.CommercialInvoice(4, "topup", orders[0].TradeNo, "correct", "", &corrected)
	if err != nil || bytes.Equal(docs[0].PDF, original) || !bytes.Contains(docs[0].PDF, []byte("CNY 30.50")) {
		t.Fatalf("batch correction: %v", err)
	}
	if err := db.Model(&orders[1]).Updates(map[string]any{"refund_status": "success", "refund_amount": 5.25, "refund_updated_at": 1792001000}).Error; err != nil {
		t.Fatal(err)
	}
	page, err := commerceapp.ListInvoiceEligibleOrdersPage(4, orders[0].TradeNo, &platformpagination.PageInfo{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	listed := page.Items.([]commerceapp.InvoiceEligibleOrder)
	if len(listed) != 1 || !listed[0].Requested || listed[0].InvoiceOrderCount != 2 || listed[0].InvoiceNumber != stored.InvoiceNumber || listed[0].InvoiceRefundStatus != "success" {
		t.Fatalf("cross-page batch metadata: %#v", listed)
	}
	corrected.PreviousNumber, corrected.RequestID = docs[0].Number, "batch-correction-2"
	if _, err := commerceapp.CommercialInvoice(4, "topup", orders[0].TradeNo, "correct", "", &corrected); err == nil {
		t.Fatal("corrected batch after another member was refunded")
	}
	credit, err := commerceapp.CommercialInvoice(4, "topup", orders[0].TradeNo, "credit", "", nil)
	if err != nil || !bytes.Contains(credit[0].PDF, []byte("CNY 5.25")) || !bytes.Contains(credit[0].PDF, []byte("CREDIT NOTE")) {
		t.Fatalf("batch refund credit: %v", err)
	}
	repeated, err = commerceapp.CommercialInvoice(4, "topup", orders[1].TradeNo, "credit", "", nil)
	if err != nil || !bytes.Equal(credit[0].PDF, repeated[0].PDF) {
		t.Fatalf("credit replay: %v", err)
	}
	if err := db.Model(&orders[1]).Updates(map[string]any{"refund_amount": 6.25, "refund_updated_at": 1792001100}).Error; err != nil {
		t.Fatal(err)
	}
	delta, err := commerceapp.CommercialInvoice(4, "topup", orders[0].TradeNo, "credit", "", nil)
	if err != nil || !bytes.Contains(delta[0].PDF, []byte("CNY 1.00")) {
		t.Fatalf("incremental credit: %v", err)
	}
	old, err := commerceapp.CommercialInvoice(4, "topup", orders[1].TradeNo, "download", stored.InvoiceNumber, nil)
	if err != nil || !bytes.Equal(old[0].PDF, original) {
		t.Fatal("original lost after correction/refund")
	}
	listedDocs, err := commerceapp.CommercialInvoice(4, "topup", orders[0].TradeNo, "list", "", nil)
	if err != nil || len(listedDocs) != 4 {
		t.Fatalf("batch history: %v %d", err, len(listedDocs))
	}
	var count int64
	db.Model(&commerceschema.InvoiceRequest{}).Count(&count)
	if count != 1 {
		t.Fatalf("duplicate batch requests: %d", count)
	}
	var reloaded commerceschema.TopUp
	if err := db.First(&reloaded, orders[0].Id).Error; err != nil || reloaded.Money != 12.5 || reloaded.Status != "success" {
		t.Fatalf("invoice changed payment: %v", err)
	}
	t.Log("batch issuance, member lookup, reorder retry, overlap rejection, foreign ownership, correction, refund delta and immutable original passed")
}

func TestCommercialInvoiceBatchRejectsInvalidSelectionsAtomically(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	fixtures := []commerceschema.TopUp{
		{UserId: 4, TradeNo: "valid", Money: 10, PaymentProvider: "epay", Status: "success", CompleteTime: 1792000000},
		{UserId: 4, TradeNo: "usd", Money: 10, PaymentProvider: "stripe", Status: "success", CompleteTime: 1792000000},
		{UserId: 5, TradeNo: "foreign", Money: 10, PaymentProvider: "epay", Status: "success", CompleteTime: 1792000000},
		{UserId: 4, TradeNo: "refunding", Money: 10, PaymentProvider: "epay", Status: "success", CompleteTime: 1792000000, RefundStatus: "processing"},
		{UserId: 4, TradeNo: "unpaid", Money: 10, PaymentProvider: "epay", Status: "pending"},
		{UserId: 4, TradeNo: "unknown", Money: 10, Status: "success", CompleteTime: 1792000000},
		{UserId: 4, TradeNo: "legacy", Money: 10, PaymentProvider: "epay", Status: "success", CompleteTime: 1792000000},
	}
	if err := db.Create(&fixtures).Error; err != nil {
		t.Fatal(err)
	}
	legacy := commerceschema.InvoiceRequest{UserID: 4, SourceType: "topup", TradeNo: "legacy", OrderAmount: 10, Currency: "CNY", OrderCount: 1, InvoiceType: "personal", Title: "Buyer", Status: "pending"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	for _, other := range []string{"valid", "usd", "foreign", "refunding", "unpaid", "unknown", "legacy"} {
		t.Run(other, func(t *testing.T) {
			input := commerceapp.CommercialInvoiceInput{InvoiceBuyer: commerceapp.InvoiceBuyer{BuyerName: "Buyer", BuyerAddress: "Hong Kong"}, Orders: []commerceapp.InvoiceOrderInput{{SourceType: "topup", TradeNo: "valid"}, {SourceType: "topup", TradeNo: other}}}
			ctx, rec := newCommerceContext(t, http.MethodPost, "/api/invoices/batches/document", input, 4)
			commercialInvoiceHandler("batch")(ctx)
			if rec.Code < 400 || rec.Code >= 500 {
				t.Fatalf("invalid batch response: %d %s", rec.Code, rec.Body.String())
			}
			var response map[string]any
			if json.Unmarshal(rec.Body.Bytes(), &response) != nil || response["success"] != false {
				t.Fatal("invalid error response")
			}
			var requests, items int64
			db.Model(&commerceschema.InvoiceRequest{}).Count(&requests)
			db.Model(&commerceschema.InvoiceRequestItem{}).Count(&items)
			if requests != 1 || items != 0 {
				t.Fatal("failed batch reserved partial orders")
			}
		})
	}
	for _, count := range []int{0, 1, 101} {
		input := commerceapp.CommercialInvoiceInput{InvoiceBuyer: commerceapp.InvoiceBuyer{BuyerName: "Buyer", BuyerAddress: "Hong Kong"}}
		for i := 0; i < count; i++ {
			input.Orders = append(input.Orders, commerceapp.InvoiceOrderInput{SourceType: "topup", TradeNo: fmt.Sprintf("limit-%d", i)})
		}
		if _, err := commerceapp.CommercialInvoiceBatch(4, &input); !errors.Is(err, commerceapp.ErrCommercialInvoiceBatchInvalid) {
			t.Fatalf("batch size %d: %v", count, err)
		}
	}
	ctx, rec := newCommerceContext(t, http.MethodPost, "/api/invoices/batches/document", map[string]any{"buyer_name": "Buyer", "buyer_address": strings.Repeat("x", 70<<10)}, 4)
	commercialInvoiceHandler("batch")(ctx)
	if rec.Code != 400 {
		t.Fatalf("oversized body: %d", rec.Code)
	}
}

func TestCommercialInvoiceBatchFreezesMixedOrderPaymentsAndUSDTPrecision(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	plan := commerceschema.SubscriptionPlan{Id: 1, Title: "Original subscription", Currency: "USD", PriceAmount: 999, Enabled: true}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	for _, currency := range []string{"CNY", "USDT"} {
		provider, first, second := "epay", 0.1, 0.2
		if currency == "USDT" {
			provider, first, second = "nowpayments", 1.123456, 2.000001
		}
		top := commerceschema.TopUp{UserId: 4, TradeNo: "mixed-top-" + currency, Money: first, PaymentProvider: provider, Status: "success", CompleteTime: 1792000000}
		sub := commerceschema.SubscriptionOrder{UserId: 4, PlanId: plan.Id, TradeNo: "mixed-sub-" + currency, Money: second, PaymentProvider: provider, Status: "success", CompleteTime: 1792000001}
		if err := db.Create(&top).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
		input := commerceapp.CommercialInvoiceInput{InvoiceBuyer: commerceapp.InvoiceBuyer{BuyerName: "Buyer", BuyerAddress: "Hong Kong"}, Orders: []commerceapp.InvoiceOrderInput{{SourceType: "topup", TradeNo: top.TradeNo}, {SourceType: "subscription", TradeNo: sub.TradeNo}}}
		docs, err := commerceapp.CommercialInvoiceBatch(4, &input)
		if err != nil {
			t.Fatal(err)
		}
		amount := "CNY 0.30"
		if currency == "USDT" {
			amount = "USDT 3.123457"
		}
		if !bytes.Contains(docs[0].PDF, []byte(amount)) {
			t.Fatalf("payment sum lost precision or used plan price/currency: %s", amount)
		}
		var stored commerceschema.InvoiceRequest
		if err := db.Where("currency = ?", currency).First(&stored).Error; err != nil {
			t.Fatal(err)
		}
		// Emulate the legacy PostgreSQL DECIMAL(12,2) summary columns; only
		// immutable archived snapshots may determine regenerated documents.
		if err := db.Model(&stored).Update("order_amount", 3.12).Error; err != nil {
			t.Fatal(err)
		}
		correction := commerceapp.CommercialInvoiceInput{InvoiceBuyer: commerceapp.InvoiceBuyer{BuyerName: "Corrected buyer", BuyerAddress: "Hong Kong"}, PreviousNumber: stored.InvoiceNumber, Reason: "Correct buyer name", RequestID: "mixed-correction-" + currency}
		revised, err := commerceapp.CommercialInvoice(4, "topup", top.TradeNo, "correct", "", &correction)
		if err != nil || !bytes.Contains(revised[0].PDF, []byte(amount)) {
			t.Fatalf("frozen aggregate changed: %v", err)
		}
	}
}

func TestCommercialInvoiceBatchAcceptsMaximumOrderCount(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	input := commerceapp.CommercialInvoiceInput{InvoiceBuyer: commerceapp.InvoiceBuyer{BuyerName: "Batch boundary fixture", BuyerAddress: "Hong Kong"}}
	for i := 0; i < 100; i++ {
		trade := fmt.Sprintf("maximum-order-%03d", i)
		order := commerceschema.TopUp{UserId: 4, TradeNo: trade, Money: 0.01, PaymentProvider: "epay", Status: "success", CompleteTime: 1792000000}
		if err := db.Create(&order).Error; err != nil {
			t.Fatal(err)
		}
		input.Orders = append(input.Orders, commerceapp.InvoiceOrderInput{SourceType: "topup", TradeNo: trade})
	}
	documents, err := commerceapp.CommercialInvoiceBatch(4, &input)
	if err != nil || !bytes.Contains(documents[0].PDF, []byte("CNY 1.00")) {
		t.Fatalf("maximum-size batch: %v", err)
	}
	var count int64
	if err := db.Model(&commerceschema.InvoiceRequestItem{}).Count(&count).Error; err != nil || count != 100 {
		t.Fatalf("maximum batch reservations: %d %v", count, err)
	}
}
