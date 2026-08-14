package http

import (
	"encoding/json"
	stdhttp "net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
)

func TestWalletTransferHandlersRequireAccountPasswordAndCompleteTransfer(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	unit := int(platformruntime.QuotaPerUnit)
	accountHash, err := platformsecurity.Password2Hash("Loginpass123")
	if err != nil {
		t.Fatalf("failed to hash account password: %v", err)
	}
	sender := &identityschema.User{
		Id: 9941, ExternalId: "HTP001", Username: "http-transfer-sender", DisplayName: "HTTP Sender",
		Password: accountHash, AffCode: "HTP001", Status: constant.UserStatusEnabled, Quota: 5 * unit,
	}
	recipient := &identityschema.User{
		Id: 9942, ExternalId: "HTP002", Username: "http-transfer-recipient", DisplayName: "HTTP Recipient",
		AffCode: "HTP002", Status: constant.UserStatusEnabled,
	}
	if err := db.Create(sender).Error; err != nil {
		t.Fatalf("failed to seed sender: %v", err)
	}
	if err := db.Create(recipient).Error; err != nil {
		t.Fatalf("failed to seed recipient: %v", err)
	}

	wrongCtx, wrongRecorder := newCommerceContext(t, stdhttp.MethodPut, "/api/wallet/transfers/payment-password", map[string]any{
		"current_password": "wrong-login", "new_payment_password": "Paypass123", "confirm_password": "Paypass123",
	}, sender.Id)
	configureWalletTransferPassword(wrongCtx)
	if response := decodeCommerceResponse(t, wrongRecorder); response.Success {
		t.Fatal("expected wrong account password to be rejected")
	}

	setupCtx, setupRecorder := newCommerceContext(t, stdhttp.MethodPut, "/api/wallet/transfers/payment-password", map[string]any{
		"current_password": "Loginpass123", "new_payment_password": "Paypass123", "confirm_password": "Paypass123",
	}, sender.Id)
	configureWalletTransferPassword(setupCtx)
	if response := decodeCommerceResponse(t, setupRecorder); !response.Success {
		t.Fatalf("expected payment password setup success, got %#v", response)
	}

	lookupCtx, lookupRecorder := newCommerceContext(t, stdhttp.MethodGet, "/api/wallet/transfers/recipients/HTP002", nil, sender.Id)
	lookupCtx.Params = gin.Params{{Key: "external_id", Value: recipient.ExternalId}}
	getWalletTransferRecipient(lookupCtx)
	lookupResponse := decodeCommerceResponse(t, lookupRecorder)
	if !lookupResponse.Success {
		t.Fatalf("expected recipient lookup success, got %#v", lookupResponse)
	}
	var lookup commerceapp.WalletTransferRecipient
	if err := json.Unmarshal(lookupResponse.Data, &lookup); err != nil {
		t.Fatalf("failed to decode lookup: %v", err)
	}
	if lookup.DisplayNameMasked == recipient.DisplayName {
		t.Fatal("recipient display name must be masked")
	}

	transferCtx, transferRecorder := newCommerceContext(t, stdhttp.MethodPost, "/api/wallet/transfers", map[string]any{
		"recipient_external_id": recipient.ExternalId,
		"amount_quota":          2 * unit,
		"payment_password":      "Paypass123",
		"request_id":            "wallet-transfer-http-request",
	}, sender.Id)
	createWalletTransfer(transferCtx)
	transferResponse := decodeCommerceResponse(t, transferRecorder)
	if !transferResponse.Success {
		t.Fatalf("expected transfer success, got %#v", transferResponse)
	}

	overviewCtx, overviewRecorder := newCommerceContext(t, stdhttp.MethodGet, "/api/wallet/transfers?p=1&page_size=10", nil, sender.Id)
	getWalletTransferOverview(overviewCtx)
	overviewResponse := decodeCommerceResponse(t, overviewRecorder)
	if !overviewResponse.Success {
		t.Fatalf("expected overview success, got %#v", overviewResponse)
	}
	var overview commerceapp.WalletTransferOverview
	if err := json.Unmarshal(overviewResponse.Data, &overview); err != nil {
		t.Fatalf("failed to decode overview: %v", err)
	}
	if overview.Balance != int64(3*unit) || overview.History.Total != 1 || !overview.Security.PasswordSet {
		t.Fatalf("unexpected transfer overview: %#v", overview)
	}
}
