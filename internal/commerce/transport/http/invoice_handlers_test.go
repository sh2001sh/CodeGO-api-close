package http

import (
	"encoding/json"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
)

func TestCreateInvoiceRequestReservesPaidTopUpOnce(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	user := &identityschema.User{Id: 89, Username: "invoice-user", Password: "password123", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&commerceschema.TopUp{UserId: user.Id, Money: 25.5, TradeNo: "invoice-topup-1", Status: constant.TopUpStatusSuccess, CreateTime: platformruntime.GetTimestamp(), CompleteTime: platformruntime.GetTimestamp()}).Error; err != nil {
		t.Fatal(err)
	}

	body := map[string]any{"source_type": commerceschema.InvoiceSourceTopUp, "trade_no": "invoice-topup-1", "invoice_type": commerceschema.InvoiceTypeEnterprise, "title": "测试科技工作室", "tax_number": "91330100TEST00001", "email": "invoice@example.com"}
	ctx, recorder := newCommerceContext(t, "POST", "/api/invoices/requests", body, user.Id)
	createInvoiceRequest(ctx)
	if response := decodeCommerceResponse(t, recorder); !response.Success {
		t.Fatalf("expected request success, got %#v", response)
	}

	ctx, recorder = newCommerceContext(t, "POST", "/api/invoices/requests", body, user.Id)
	createInvoiceRequest(ctx)
	if response := decodeCommerceResponse(t, recorder); response.Success {
		t.Fatalf("expected duplicate request rejection, got %#v", response)
	}
}

func TestInvoiceEligibleOrdersSkipSubscriptionTopUpMirror(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	user := &identityschema.User{Id: 90, Username: "invoice-sub-user", Password: "password123", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&commerceschema.SubscriptionPlan{Id: 1, Title: "月卡", Currency: "CNY", PriceAmount: 20, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&commerceschema.SubscriptionOrder{UserId: user.Id, PlanId: 1, Money: 20, TradeNo: "subscription-invoice-1", Status: constant.TopUpStatusSuccess, CreateTime: platformruntime.GetTimestamp(), CompleteTime: platformruntime.GetTimestamp()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&commerceschema.TopUp{UserId: user.Id, Money: 20, TradeNo: "subscription-invoice-1", Status: constant.TopUpStatusSuccess, CreateTime: platformruntime.GetTimestamp(), CompleteTime: platformruntime.GetTimestamp()}).Error; err != nil {
		t.Fatal(err)
	}

	ctx, recorder := newCommerceContext(t, "GET", "/api/invoices/eligible-orders", nil, user.Id)
	listInvoiceEligibleOrders(ctx)
	response := decodeCommerceResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected eligible orders success, got %#v", response)
	}
	var orders []map[string]any
	if err := json.Unmarshal(response.Data, &orders); err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || orders[0]["source_type"] != commerceschema.InvoiceSourceSubscription {
		t.Fatalf("expected one subscription order, got %#v", orders)
	}
}

func TestAdminIssueInvoiceRequest(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	request := &commerceschema.InvoiceRequest{UserID: 91, SourceType: commerceschema.InvoiceSourceTopUp, TradeNo: "admin-issue-1", OrderAmount: 18, Currency: "CNY", OrderTitle: "钱包充值", InvoiceType: commerceschema.InvoiceTypePersonal, Title: "测试用户", Email: "invoice@example.com", Status: commerceschema.InvoiceStatusPending}
	if err := db.Create(request).Error; err != nil {
		t.Fatal(err)
	}

	body := map[string]any{"status": commerceschema.InvoiceStatusIssued, "invoice_number": "01234567", "delivery_method": commerceschema.InvoiceDeliveryEmail, "document_url": "https://invoice.example.com/download/1.pdf"}
	ctx, recorder := newCommerceContext(t, "PUT", "/api/invoices/admin/requests/1", body, 1)
	ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: "1"})
	updateAdminInvoiceRequest(ctx)
	if response := decodeCommerceResponse(t, recorder); !response.Success {
		t.Fatalf("expected issue success, got %#v", response)
	}

	var reloaded commerceschema.InvoiceRequest
	if err := db.First(&reloaded, request.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != commerceschema.InvoiceStatusIssued || reloaded.IssuedAt == 0 {
		t.Fatalf("expected issued request, got %#v", reloaded)
	}
}
