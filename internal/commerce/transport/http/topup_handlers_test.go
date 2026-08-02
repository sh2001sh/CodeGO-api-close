package http

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"

	"gorm.io/gorm"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type commerceAPIResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func setupCommerceHTTPTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	platformdb.UsingSQLite = true
	platformdb.UsingMySQL = false
	platformdb.UsingPostgreSQL = false
	platformcache.RedisEnabled = false

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	platformdb.DB = db
	platformdb.LogDB = db

	if err := db.AutoMigrate(
		&identityschema.User{},
		&commerceschema.TopUp{},
		&commerceschema.BlindBoxProp{},
		&commerceschema.Redemption{},
		&auditschema.Log{},
		&billingschema.BillingAccount{},
		&billingschema.BillingBalanceSnapshot{},
		&billingschema.BillingLedgerEntry{},
		&billingschema.BillingReservation{},
		&billingschema.BillingSettlement{},
		&billingschema.BillingOutboxEvent{},
		&commerceschema.SubscriptionPlan{},
		&commerceschema.SubscriptionOrder{},
		&commerceschema.UserSubscription{},
		&commerceschema.SubscriptionClaudeConversion{},
		&commerceschema.WalletQuotaConversion{},
		&commerceschema.SubscriptionResetOpportunityAccount{},
		&commerceschema.SubscriptionResetOpportunityLedger{},
		&commerceschema.GroupBuyOrder{},
		&commerceschema.GroupBuyMember{},
	); err != nil {
		t.Fatalf("failed to migrate commerce http tables: %v", err)
	}

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func newCommerceContext(t *testing.T, method string, target string, body any, userID int) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	var requestBody *bytes.Reader
	if body != nil {
		payload, err := platformencoding.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		requestBody = bytes.NewReader(payload)
	} else {
		requestBody = bytes.NewReader(nil)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, requestBody)
	if body != nil {
		ctx.Request.Header.Set("Content-Type", "application/json")
	}
	ctx.Set("id", userID)
	return ctx, recorder
}

func decodeCommerceResponse(t *testing.T, recorder *httptest.ResponseRecorder) commerceAPIResponse {
	t.Helper()

	var response commerceAPIResponse
	if err := platformencoding.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode api response: %v", err)
	}
	return response
}

func confirmTopupComplianceForTest(t *testing.T) {
	t.Helper()
	paymentSetting := commercestore.GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
	})
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = commercestore.CurrentComplianceTermsVersion
}

func TestGetTopUpInfoReturnsSuccess(t *testing.T) {
	setupCommerceHTTPTestDB(t)
	confirmTopupComplianceForTest(t)

	originalPayMethods := commercestore.PayMethods
	t.Cleanup(func() {
		commercestore.PayMethods = originalPayMethods
	})
	commercestore.PayMethods = []map[string]string{{"type": "alipay", "name": "支付宝"}}

	ctx, recorder := newCommerceContext(t, stdhttp.MethodGet, "/api/user/topup/info", nil, 0)
	GetTopUpInfo(ctx)

	response := decodeCommerceResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected topup info success, got %#v", response)
	}
}

func TestRedeemTopUpCodeReturnsRedemptionResult(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	confirmTopupComplianceForTest(t)

	user := &identityschema.User{Id: 1, Username: "redeem-user", Password: "password123", DisplayName: "Redeem User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default", AffCode: "RD01"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	if err := db.Create(&commerceschema.Redemption{
		Id:          1,
		Key:         "redeem-key-1",
		Name:        "quota pack",
		Status:      constant.RedemptionCodeStatusEnabled,
		RedeemType:  commerceschema.RedemptionTypeQuota,
		Quota:       5,
		WalletType:  commerceschema.WalletTypeDefault,
		CreatedTime: platformruntime.GetTimestamp(),
	}).Error; err != nil {
		t.Fatalf("failed to seed redemption: %v", err)
	}

	ctx, recorder := newCommerceContext(t, stdhttp.MethodPost, "/api/user/topup", map[string]any{
		"key": "redeem-key-1",
	}, user.Id)
	RedeemTopUpCode(ctx)

	response := decodeCommerceResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected redemption success, got %#v", response)
	}

	reloaded, err := loadUserByIDForTest(user.Id, true)
	if err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	if reloaded.Quota != 5 {
		t.Fatalf("expected redeemed quota 5, got %d", reloaded.Quota)
	}
}

func TestRedeemTopUpCodeRequiresComplianceConfirmation(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)

	user := &identityschema.User{Id: 1, Username: "redeem-no-compliance", Password: "password123", DisplayName: "Redeem No Compliance", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default", AffCode: "RD02"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	ctx, recorder := newCommerceContext(t, stdhttp.MethodPost, "/api/user/topup", map[string]any{
		"key": "redeem-key-2",
	}, user.Id)
	RedeemTopUpCode(ctx)

	response := decodeCommerceResponse(t, recorder)
	if response.Success {
		t.Fatalf("expected compliance failure, got %#v", response)
	}
}

func TestRequestAmountSupportsClaudeWallet(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	user := &identityschema.User{Id: 1, Username: "quote-user", Password: "password123", DisplayName: "Quote User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default", AffCode: "QT01"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	ctx, recorder := newCommerceContext(t, stdhttp.MethodPost, "/api/user/amount", map[string]any{
		"amount":      2,
		"wallet_type": commerceschema.WalletTypeClaude,
	}, user.Id)
	RequestAmount(ctx)

	var payload struct {
		Message string `json:"message"`
		Data    string `json:"data"`
	}
	if err := platformencoding.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode amount response: %v", err)
	}
	if payload.Message != "success" || payload.Data != "2.00" {
		t.Fatalf("unexpected amount response: %#v", payload)
	}
}

func TestGetUserTopUpsReturnsSuccess(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	user := &identityschema.User{Id: 1, Username: "history-user", Password: "password123", DisplayName: "History User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default", AffCode: "TH01"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	if err := db.Create(&commerceschema.TopUp{UserId: user.Id, Amount: 2, Money: 2, TradeNo: "history-1", PaymentMethod: commerceschema.PaymentMethodStripe, PaymentProvider: commerceschema.PaymentProviderStripe, CreateTime: platformruntime.GetTimestamp(), Status: constant.TopUpStatusSuccess}).Error; err != nil {
		t.Fatalf("failed to seed topup: %v", err)
	}

	ctx, recorder := newCommerceContext(t, stdhttp.MethodGet, "/api/user/topup/self?p=1&size=10", nil, user.Id)
	GetUserTopUps(ctx)

	response := decodeCommerceResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected topup history success, got %#v", response)
	}
}

func TestGetAllTopUpsReturnsSuccess(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	if err := db.Create(&commerceschema.TopUp{UserId: 1, Amount: 3, Money: 3, TradeNo: "admin-history-1", PaymentMethod: commerceschema.PaymentMethodStripe, PaymentProvider: commerceschema.PaymentProviderStripe, CreateTime: platformruntime.GetTimestamp(), Status: constant.TopUpStatusSuccess}).Error; err != nil {
		t.Fatalf("failed to seed topup: %v", err)
	}

	ctx, recorder := newCommerceContext(t, stdhttp.MethodGet, "/api/user/topup?p=1&size=10", nil, 0)
	GetAllTopUps(ctx)

	response := decodeCommerceResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected admin topup page success, got %#v", response)
	}
}

func TestAdminCompleteTopUpCreditsQuota(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	user := &identityschema.User{Id: 1, Username: "complete-user", Password: "password123", DisplayName: "Complete User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default", AffCode: "CP01"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	if err := db.Create(&commerceschema.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           2,
		TradeNo:         "pending-topup-1",
		PaymentMethod:   commerceschema.PaymentMethodStripe,
		PaymentProvider: commerceschema.PaymentProviderStripe,
		WalletType:      commerceschema.WalletTypeDefault,
		CreateTime:      platformruntime.GetTimestamp(),
		Status:          constant.TopUpStatusPending,
	}).Error; err != nil {
		t.Fatalf("failed to seed pending topup: %v", err)
	}

	ctx, recorder := newCommerceContext(t, stdhttp.MethodPost, "/api/user/topup/complete", map[string]any{
		"trade_no": "pending-topup-1",
	}, 0)
	AdminCompleteTopUp(ctx)

	response := decodeCommerceResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected admin complete topup success, got %#v", response)
	}

	reloaded, err := loadUserByIDForTest(user.Id, true)
	if err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	if reloaded.Quota != int(platformruntime.QuotaPerUnit*2) {
		t.Fatalf("expected quota %d, got %d", int(platformruntime.QuotaPerUnit*2), reloaded.Quota)
	}
}
