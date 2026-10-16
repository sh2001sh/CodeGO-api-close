package http

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
)

func TestCommercialInvoiceLifecycleAndOwnership(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	user := identityschema.User{Id: 1701, Username: "invoice-commercial", Password: "password123", Quota: 123456, Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	order := commerceschema.TopUp{UserId: user.Id, TradeNo: "commercial-paid", Status: constant.TopUpStatusSuccess, Money: 25.5, Amount: 25, CompleteTime: 1792000000, PaymentProvider: "stripe", ExternalPaymentID: "pi-fixture", RefundStatus: commerceschema.RefundStatusFailed}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	call := func(action string, body any, userID int, number string) (int, []byte) {
		t.Helper()
		method := http.MethodGet
		if action == "issue" || action == "correct" || action == "credit" {
			method = http.MethodPost
		}
		ctx, rec := newCommerceContext(t, method, "/api/invoices/orders/topup/commercial-paid/document?number="+number, body, userID)
		ctx.Params = gin.Params{{Key: "source", Value: "topup"}, {Key: "trade_no", Value: order.TradeNo}}
		commercialInvoiceHandler(action)(ctx)
		return rec.Code, rec.Body.Bytes()
	}
	buyer := commerceapp.CommercialInvoiceInput{InvoiceBuyer: commerceapp.InvoiceBuyer{BuyerName: "繁體買方有限公司", BuyerAddress: "香港九龍測試道 12 號\n16 樓", BuyerCountry: "Hong Kong"}}
	if status, _ := call("download", nil, user.Id, ""); status != 428 {
		t.Fatalf("missing purchaser: %d", status)
	}
	if status, _ := call("issue", buyer, user.Id+1, ""); status != 404 {
		t.Fatalf("foreign order: %d", status)
	}
	if status, _ := call("issue", map[string]string{"buyer_name": "x"}, user.Id, ""); status != 400 {
		t.Fatalf("missing address: %d", status)
	}
	status, original := call("issue", buyer, user.Id, "")
	if status != 200 || !bytes.HasPrefix(original, []byte("%PDF-1.4")) {
		t.Fatalf("issue: %d %s", status, original)
	}
	for _, text := range []string{"CodeGo AI Limited", "81318858", "USD 25.50", "INVOICE", hex.EncodeToString([]byte{0, 'p', 0, 'i', 0, '-', 0, 'f', 0, 'i', 0, 'x', 0, 't', 0, 'u', 0, 'r', 0, 'e'}), "/ActualText", "company chop"} {
		if !bytes.Contains(original, []byte(text)) {
			t.Errorf("missing PDF content %q", text)
		}
	}
	if status, repeat := call("issue", buyer, user.Id, ""); status != 200 || !bytes.Equal(original, repeat) {
		t.Fatal("retry changed frozen PDF")
	}
	var stored commerceschema.InvoiceRequest
	if err := db.Where("user_id = ?", user.Id).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	originalNumber := stored.InvoiceNumber
	if len(stored.Remark) > 65535 {
		t.Fatalf("initial archive exceeds MySQL TEXT: %d", len(stored.Remark))
	}
	if _, err := commerceapp.UpdateAdminInvoiceRequest(stored.ID, 1, commerceapp.UpdateInvoiceRequestInput{Status: "issued", InvoiceNumber: "altered"}); err == nil {
		t.Fatal("legacy admin can alter frozen invoice")
	}
	legacy := commerceschema.InvoiceRequest{UserID: user.Id, SourceType: "batch", TradeNo: "legacy", OrderAmount: 10, Currency: "CNY", OrderTitle: "Legacy order", InvoiceType: "personal", Title: "Buyer", Email: "fixture@example.test", Status: "pending"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	page, err := commerceapp.ListUserInvoiceRequests(user.Id, &platformpagination.PageInfo{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	legacyItems, ok := page.Items.([]commerceschema.InvoiceRequest)
	if !ok || len(legacyItems) != 1 || legacyItems[0].ID != legacy.ID {
		t.Fatalf("legacy history leaked archives or lost requests: %#v", page)
	}
	changed := buyer
	changed.BuyerName = "更正後買方"
	changed.Reason = "Company legal name corrected"
	changed.PreviousNumber = originalNumber
	changed.RequestID = "correction-1"
	if status, _ := call("issue", changed, user.Id, ""); status != 409 {
		t.Fatalf("silent buyer overwrite: %d", status)
	}
	status, corrected := call("correct", changed, user.Id, "")
	if status != 200 || bytes.Equal(original, corrected) {
		t.Fatalf("correction: %d", status)
	}
	if status, repeat := call("correct", changed, user.Id, ""); status != 200 || !bytes.Equal(corrected, repeat) {
		t.Fatal("correction retry changed PDF")
	}
	if status, repeat := call("download", nil, user.Id, originalNumber); status != 200 || !bytes.Equal(original, repeat) {
		t.Fatal("original lost after correction")
	}
	if status, _ := call("credit", nil, user.Id, ""); status != 404 {
		t.Fatalf("unconfirmed refund generated credit: %d", status)
	}
	if err := db.Model(&order).Updates(map[string]any{"refund_status": commerceschema.RefundStatusSuccess, "refund_amount": 5.25, "refund_updated_at": 1792001000}).Error; err != nil {
		t.Fatal(err)
	}
	status, credit := call("credit", nil, user.Id, "")
	if status != 200 || !bytes.Contains(credit, []byte("CREDIT NOTE")) || !bytes.Contains(credit, []byte("USD 5.25")) {
		t.Fatalf("credit: %d", status)
	}
	if status, repeat := call("credit", nil, user.Id, ""); status != 200 || !bytes.Equal(credit, repeat) {
		t.Fatal("credit retry changed PDF")
	}
	if status, repeat := call("download", nil, user.Id, originalNumber); status != 200 || !bytes.Equal(original, repeat) {
		t.Fatal("original lost after refund")
	}
	status, body := call("list", nil, user.Id, "")
	var list struct {
		Data []commerceapp.CommercialInvoiceDocument `json:"data"`
	}
	if status != 200 || json.Unmarshal(body, &list) != nil || len(list.Data) != 3 {
		t.Fatalf("document history: %d %s", status, body)
	}
	for _, d := range list.Data {
		if len(d.PDF) > 0 {
			t.Fatal("list exposed document binary")
		}
	}
	var reloaded identityschema.User
	if err := db.First(&reloaded, user.Id).Error; err != nil {
		t.Fatal(err)
	}
	if reloaded.Quota != user.Quota {
		t.Fatal("invoice changed wallet quota")
	}
	var reloadedOrder commerceschema.TopUp
	if err := db.First(&reloadedOrder, order.Id).Error; err != nil {
		t.Fatal(err)
	}
	if reloadedOrder.Money != 25.5 || reloadedOrder.Amount != 25 {
		t.Fatal("invoice changed order value")
	}
	t.Logf("PASS frozen PDF, multilingual buyer, ownership, validation, correction history, confirmed-refund credit note, unchanged order and quota (%d bytes)", len(original))
}

func TestCommercialInvoiceRejectsUnpaidAndInvalidSource(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	for index, source := range []string{"topup", "subscription", "../topup"} {
		trade := fmt.Sprintf("unpaid-%d", index)
		if err := db.Create(&commerceschema.TopUp{UserId: 4, TradeNo: trade, Status: constant.TopUpStatusPending, Money: 10}).Error; err != nil {
			t.Fatal(err)
		}
		_, err := commerceapp.CommercialInvoice(4, source, trade, "download", "", nil)
		if err == nil {
			t.Fatalf("accepted unpaid order or invalid source %q", source)
		}
	}
}

func TestCommercialInvoiceRequiresKnownPaymentSnapshot(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	buyer := commerceapp.CommercialInvoiceInput{InvoiceBuyer: commerceapp.InvoiceBuyer{BuyerName: "Buyer", BuyerAddress: "Hong Kong"}}
	for index, test := range []struct {
		name, provider, method, payload, currency string
		amount                                    float64
	}{
		{name: "missing provider"},
		{name: "unknown provider", provider: "unknown"},
		{name: "legacy payment method", method: "wxpay", currency: "CNY"},
		{name: "missing Creem snapshot", provider: "creem"},
		{name: "taxed Creem payment", provider: "creem", payload: `{"object":{"order":{"currency":"USD","amount_paid":1100,"tax_amount":100}}}`},
		{name: "different Creem payment", provider: "creem", payload: `{"object":{"order":{"currency":"USD","amount_paid":900}}}`},
		{name: "matching Creem payment", provider: "creem", payload: `{"object":{"order":{"currency":"USD","amount_paid":1000}}}`, currency: "USD"},
		{name: "USDT precision", provider: "nowpayments", currency: "USDT", amount: 10.123456},
	} {
		t.Run(test.name, func(t *testing.T) {
			amount := test.amount
			if amount == 0 {
				amount = 10
			}
			order := commerceschema.TopUp{UserId: 4, TradeNo: fmt.Sprintf("snapshot-%d", index), Status: constant.TopUpStatusSuccess, Money: amount, Amount: 10, CompleteTime: 1792000000, PaymentProvider: test.provider, PaymentMethod: test.method, ProviderPayload: test.payload}
			if err := db.Create(&order).Error; err != nil {
				t.Fatal(err)
			}
			documents, err := commerceapp.CommercialInvoice(4, "topup", order.TradeNo, "issue", "", &buyer)
			var count int64
			db.Model(&commerceschema.InvoiceRequest{}).Where("trade_no = ?", order.TradeNo).Count(&count)
			if test.currency == "" {
				if err == nil || count != 0 {
					t.Fatal("unverified payment created an invoice")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.currency == "USDT" && !bytes.Contains(documents[0].PDF, []byte("USDT 10.123456")) {
				t.Fatal("USDT precision lost")
			}
			var stored commerceschema.InvoiceRequest
			if err := db.Where("trade_no = ?", order.TradeNo).First(&stored).Error; err != nil || stored.Currency != test.currency || stored.OrderAmount != amount {
				t.Fatalf("incorrect frozen payment: %#v, %v", stored, err)
			}
		})
	}
}
