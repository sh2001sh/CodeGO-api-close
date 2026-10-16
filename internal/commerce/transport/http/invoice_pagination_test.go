package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/sh2001sh/new-api/constant"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
)

func TestInvoiceEligibleOrdersPagedHTTPContract(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	const userID = 801
	orders := make([]commerceschema.TopUp, 0, 125)
	for i := 1; i <= 125; i++ {
		orders = append(orders, commerceschema.TopUp{UserId: userID, TradeNo: fmt.Sprintf("page-%03d", i), Money: 12.5, Status: constant.TopUpStatusSuccess, CompleteTime: 1000, PaymentProvider: "epay"})
	}
	if err := db.Create(&orders).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&commerceschema.TopUp{UserId: 802, TradeNo: "other-user", Money: 99, Status: constant.TopUpStatusSuccess}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&commerceschema.SubscriptionPlan{Id: 1, Title: "旧套餐", Currency: "USD", PriceAmount: 20}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&commerceschema.SubscriptionOrder{UserId: userID, PlanId: 1, TradeNo: "page-125", Money: 12.5, CompleteTime: 1000, Status: constant.TopUpStatusSuccess, PaymentProvider: "epay"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&commerceschema.InvoiceRequestItem{UserID: userID, InvoiceID: 1, SourceType: commerceschema.InvoiceSourceTopUp, TradeNo: "page-001", OrderAmount: 12.5, Currency: "CNY", OrderTitle: "钱包充值"}).Error; err != nil {
		t.Fatal(err)
	}
	get := func(query string) (platformpagination.PageInfo, []commerceapp.InvoiceEligibleOrder) {
		t.Helper()
		ctx, recorder := newCommerceContext(t, "GET", "/api/invoices/eligible-orders"+query, nil, userID)
		listInvoiceEligibleOrders(ctx)
		response := decodeCommerceResponse(t, recorder)
		if !response.Success {
			t.Fatalf("query %s: %s", query, response.Message)
		}
		var page platformpagination.PageInfo
		var payload struct {
			Items []commerceapp.InvoiceEligibleOrder `json:"items"`
		}
		if err := json.Unmarshal(response.Data, &page); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(response.Data, &payload); err != nil {
			t.Fatal(err)
		}
		return page, payload.Items
	}
	first, items := get("?p=1&page_size=50")
	if first.Total != 125 || len(items) != 50 || items[0].SourceType != commerceschema.InvoiceSourceSubscription || items[0].Currency != "CNY" {
		t.Fatalf("first page: %#v items=%#v", first, items)
	}
	last, items := get("?p=3&page_size=50")
	if last.Total != 125 || len(items) != 25 || items[24].TradeNo != "page-001" || !items[24].Requested {
		t.Fatalf("last page: %#v items=%#v", last, items)
	}
	_, repeat := get("?p=3&page_size=50")
	for i := range items {
		if items[i].TradeNo != repeat[i].TradeNo {
			t.Fatal("tied payment times must have a stable order")
		}
	}
	for _, query := range []string{"page-001", "12.5", "CNY", "旧套餐"} {
		page, matches := get("?p=1&page_size=50&keyword=" + url.QueryEscape(query))
		if page.Total == 0 || len(matches) == 0 {
			t.Fatalf("global search %q found no orders", query)
		}
	}
	for _, query := range []string{"missing", "%", "_", "' OR 1=1 --"} {
		page, matches := get("?p=1&page_size=50&keyword=" + url.QueryEscape(query))
		if page.Total != 0 || len(matches) != 0 {
			t.Fatalf("literal query %q unexpectedly matched", query)
		}
	}
	page, items := get("?p=999&page_size=50")
	if page.Total != 125 || len(items) != 0 {
		t.Fatalf("out of range: %#v", page)
	}
	page, items = get("?p=-3&page_size=10000")
	if page.Page != 1 || page.PageSize != 100 || len(items) != 100 {
		t.Fatalf("bounded page: %#v count=%d", page, len(items))
	}
}

func TestInvoiceEligibleOrdersSearchValidatedCreemCurrency(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	const userID = 803
	for _, input := range []struct {
		trade, payload string
	}{
		{"valid-euro", `{"object":{"order":{"currency":"eur","amount_paid":1250,"tax_amount":0}}}`},
		{"invalid-amount", `{"object":{"order":{"currency":"eur","amount_paid":1200,"tax_amount":0}}}`},
		{"taxed", `{"object":{"order":{"currency":"eur","amount_paid":1250,"tax_amount":50}}}`},
		{"broken-json", "eur{"},
	} {
		if err := db.Create(&commerceschema.TopUp{UserId: userID, TradeNo: input.trade, Money: 12.5, Status: constant.TopUpStatusSuccess, PaymentProvider: "creem", ProviderPayload: input.payload}).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx, recorder := newCommerceContext(t, "GET", "/api/invoices/eligible-orders?p=1&page_size=50&keyword=EUR", nil, userID)
	listInvoiceEligibleOrders(ctx)
	response := decodeCommerceResponse(t, recorder)
	var page struct {
		Total int                                `json:"total"`
		Items []commerceapp.InvoiceEligibleOrder `json:"items"`
	}
	if err := json.Unmarshal(response.Data, &page); err != nil {
		t.Fatal(err)
	}
	if !response.Success || page.Total != 1 || len(page.Items) != 1 || page.Items[0].TradeNo != "valid-euro" || page.Items[0].Currency != "EUR" {
		t.Fatalf("must validate payment snapshot, response=%#v page=%#v", response, page)
	}
}

func TestInvoiceEligibleOrdersCancelledReadStopsDatabaseWork(t *testing.T) {
	setupCommerceHTTPTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := commerceapp.ListInvoiceEligibleOrdersPageWithContext(ctx, 803, "", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancelled query, got %v", err)
	}
	if _, err := commerceapp.ListInvoiceEligibleOrdersPage(0, "", nil); err == nil {
		t.Fatal("missing user must be rejected")
	}
}
