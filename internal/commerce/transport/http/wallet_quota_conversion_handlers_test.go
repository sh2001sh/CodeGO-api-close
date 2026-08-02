package http

import (
	"encoding/json"
	stdhttp "net/http"
	"testing"

	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
)

func TestWalletQuotaConversionHandlersReturnOverviewAndConvert(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	unit := int(platformruntime.QuotaPerUnit)
	user := &identityschema.User{
		Id:          9921,
		Username:    "wallet-conversion-http",
		Status:      constant.UserStatusEnabled,
		Quota:       8 * unit,
		ClaudeQuota: unit,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	createCtx, createRecorder := newCommerceContext(t, stdhttp.MethodPost, "/api/wallet/quota-conversions", map[string]any{
		"direction":    commerceschema.WalletQuotaConversionStandardToClaude,
		"source_quota": 4 * unit,
		"request_id":   "wallet-conversion-http-request",
	}, user.Id)
	createWalletQuotaConversion(createCtx)
	createResponse := decodeCommerceResponse(t, createRecorder)
	if !createResponse.Success {
		t.Fatalf("expected conversion success, got %#v", createResponse)
	}

	var conversion commerceschema.WalletQuotaConversion
	if err := json.Unmarshal(createResponse.Data, &conversion); err != nil {
		t.Fatalf("failed to decode conversion: %v", err)
	}
	if conversion.TargetQuota != int64(unit) {
		t.Fatalf("expected target quota %d, got %d", unit, conversion.TargetQuota)
	}

	listCtx, listRecorder := newCommerceContext(t, stdhttp.MethodGet, "/api/wallet/quota-conversions", nil, user.Id)
	getWalletQuotaConversions(listCtx)
	listResponse := decodeCommerceResponse(t, listRecorder)
	if !listResponse.Success {
		t.Fatalf("expected overview success, got %#v", listResponse)
	}
	var overview struct {
		StandardPerClaude int64                                  `json:"standard_per_claude"`
		StandardQuota     int64                                  `json:"standard_quota"`
		ClaudeQuota       int64                                  `json:"claude_quota"`
		RecentConversions []commerceschema.WalletQuotaConversion `json:"recent_conversions"`
	}
	if err := json.Unmarshal(listResponse.Data, &overview); err != nil {
		t.Fatalf("failed to decode overview: %v", err)
	}
	if overview.StandardPerClaude != 4 || overview.StandardQuota != int64(4*unit) || overview.ClaudeQuota != int64(2*unit) {
		t.Fatalf("unexpected overview: %#v", overview)
	}
	if len(overview.RecentConversions) != 1 {
		t.Fatalf("expected one conversion, got %d", len(overview.RecentConversions))
	}
}
