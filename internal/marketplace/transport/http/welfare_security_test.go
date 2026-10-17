package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
)

func setupWelfareSecurityTest(t *testing.T) (*gin.Engine, int64) {
	t.Helper()
	engine := setupCreditPackFilterAPITest(t)
	db := platformdb.DB
	require.NoError(t, db.AutoMigrate(
		&billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{},
		&billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{},
		&billingschema.FundingLot{}, &billingschema.FundingAllocation{},
		&commerceschema.WalletTransfer{}, &commerceschema.WalletTransferSecurity{}, &auditschema.Log{},
	))
	oldLog := platformdb.LogDB
	platformdb.LogDB = db
	t.Cleanup(func() { platformdb.LogDB = oldLog })
	unit := int64(platformruntime.QuotaPerUnit)
	require.NoError(t, db.Model(&identityschema.User{}).Where("id = ?", 42).Updates(map[string]any{
		"external_id": "WEL042", "status": constant.UserStatusEnabled, "claude_quota": 10 * unit,
	}).Error)
	require.NoError(t, db.Create(&identityschema.User{Id: 11_042, ExternalId: "WEL043", Username: "welfare-recipient", AffCode: "WEL043", Status: constant.UserStatusEnabled}).Error)
	require.NoError(t, commerceapp.ConfigureWalletTransferPassword(42, "", "Paypass123"))
	engine.POST("/welfare/:id", func(c *gin.Context) { c.Set("id", 42) }, BatchWelfare)
	return engine, unit
}

func welfareSecurityRequest(t *testing.T, engine *gin.Engine, channel, body string) (bool, MarketplaceWelfareTestResult) {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/welfare/"+channel, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, req)
	var envelope struct {
		Success bool                         `json:"success"`
		Data    MarketplaceWelfareTestResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope), recorder.Body.String())
	return envelope.Success, envelope.Data
}

type MarketplaceWelfareTestResult struct {
	SuccessCount int `json:"success_count"`
	FailedCount  int `json:"failed_count"`
}

func TestBatchWelfareDebitsOwnerAndReplaysWithoutMinting(t *testing.T) {
	engine, unit := setupWelfareSecurityTest(t)
	// Existing fixture api-supported belongs to user 10; assign this fixture to the caller.
	require.NoError(t, platformdb.DB.Table("marketplace_channels").Where("id = ?", "api-supported").Update("owner_user_id", 42).Error)
	body := fmt.Sprintf(`{"user_ids":["WEL043"],"type":"transfer","amount":%d,"request_id":"stable-batch","payment_password":"Paypass123"}`, unit)
	for range 2 {
		ok, result := welfareSecurityRequest(t, engine, "api-supported", body)
		require.True(t, ok)
		require.Equal(t, 1, result.SuccessCount)
	}
	sender, err := billingapp.GetUserClaudeWalletQuota(42)
	require.NoError(t, err)
	recipient, err := billingapp.GetUserClaudeWalletQuota(11_042)
	require.NoError(t, err)
	require.EqualValues(t, 10*unit-unit-unit/100, sender)
	require.EqualValues(t, unit, recipient)
	var transfers int64
	require.NoError(t, platformdb.DB.Model(&commerceschema.WalletTransfer{}).Count(&transfers).Error)
	require.EqualValues(t, 1, transfers)
	var unfunded int64
	require.NoError(t, platformdb.DB.Model(&billingschema.BillingLedgerEntry{}).Where("reason_code = ?", "marketplace_welfare").Count(&unfunded).Error)
	require.Zero(t, unfunded)
}

func TestBatchWelfareRejectsSelfUnauthorizedMissingPasswordAndOversizedBatch(t *testing.T) {
	engine, unit := setupWelfareSecurityTest(t)
	require.NoError(t, platformdb.DB.Table("marketplace_channels").Where("id = ?", "api-supported").Update("owner_user_id", 42).Error)
	base := fmt.Sprintf(`"type":"transfer","amount":%d,"request_id":"invalid","payment_password":"Paypass123"`, unit)
	for _, body := range []string{
		`{"user_ids":["WEL043"],"type":"transfer","amount":1}`, // old unauthenticated money grant contract
		`{"user_ids":["WEL043","WEL043"],` + base + `}`,
		`{"user_ids":[],` + base + `}`,
		`{"user_ids":[` + strings.Repeat(`"WEL043",`, 50) + `"WEL043"],` + base + `}`,
	} {
		ok, _ := welfareSecurityRequest(t, engine, "api-supported", body)
		require.False(t, ok)
	}
	ok, _ := welfareSecurityRequest(t, engine, "api-unsupported", `{"user_ids":["WEL043"],`+base+`}`)
	require.False(t, ok)
	for _, body := range []string{
		`{"user_ids":["WEL042"],` + base + `}`,
		fmt.Sprintf(`{"user_ids":["WEL043"],"type":"transfer","amount":%d,"request_id":"no-funds","payment_password":"Paypass123"}`, 11*unit),
		fmt.Sprintf(`{"user_ids":["WEL043"],"type":"transfer","amount":%d,"request_id":"wrong-pass","payment_password":"wrong"}`, unit),
	} {
		ok, result := welfareSecurityRequest(t, engine, "api-supported", body)
		require.True(t, ok)
		require.Zero(t, result.SuccessCount)
		require.Equal(t, 1, result.FailedCount)
	}
	var transfers int64
	require.NoError(t, platformdb.DB.Model(&commerceschema.WalletTransfer{}).Count(&transfers).Error)
	require.Zero(t, transfers)
	var recipient identityschema.User
	require.NoError(t, platformdb.DB.First(&recipient, 11_042).Error)
	require.Zero(t, recipient.ClaudeQuota)
}
