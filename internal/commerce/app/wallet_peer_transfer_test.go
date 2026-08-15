package app

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"github.com/stretchr/testify/require"
)

func TestWalletTransferPasswordHashAndLockout(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{Id: 9931, ExternalId: "TRS001", Username: "transfer-security", AffCode: "TRS001", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, ConfigureWalletTransferPassword(user.Id, "", "Paypass123"))

	var security commerceschema.WalletTransferSecurity
	require.NoError(t, db.First(&security, "user_id = ?", user.Id).Error)
	require.NotEqual(t, "Paypass123", security.PasswordHash)
	require.True(t, platformsecurity.ValidatePasswordAndHash("Paypass123", security.PasswordHash))

	for attempt := 1; attempt < walletTransferMaxFailedAttempts; attempt++ {
		require.ErrorIs(t, verifyWalletTransferPassword(user.Id, "wrong-pass"), commerceschema.ErrWalletTransferPasswordIncorrect)
	}
	require.ErrorIs(t, verifyWalletTransferPassword(user.Id, "wrong-pass"), commerceschema.ErrWalletTransferPasswordLocked)
	require.ErrorIs(t, verifyWalletTransferPassword(user.Id, "Paypass123"), commerceschema.ErrWalletTransferPasswordLocked)

	require.NoError(t, db.First(&security, "user_id = ?", user.Id).Error)
	require.Greater(t, security.LockedUntil, time.Now().Unix())
}

func TestTransferWalletQuotaIsAtomicIdempotentAndPrivate(t *testing.T) {
	db := setupRedemptionTestDB(t)
	unit := int(platformruntime.QuotaPerUnit)
	sender := &identityschema.User{
		Id: 9932, ExternalId: "TRS002", Username: "transfer-sender", DisplayName: "Sender Person",
		AffCode: "TRS002", Status: constant.UserStatusEnabled, ClaudeQuota: 10 * unit,
	}
	recipient := &identityschema.User{
		Id: 9933, ExternalId: "TRS003", Username: "transfer-recipient", DisplayName: "Recipient Person",
		AffCode: "TRS003", Status: constant.UserStatusEnabled, ClaudeQuota: unit,
	}
	require.NoError(t, db.Create(sender).Error)
	require.NoError(t, db.Create(recipient).Error)
	require.NoError(t, ConfigureWalletTransferPassword(sender.Id, "", "Paypass123"))

	request := CreateWalletTransferRequest{
		RecipientExternalId: recipient.ExternalId,
		AmountQuota:         int64(2 * unit),
		PaymentPassword:     "Paypass123",
		RequestId:           "wallet-transfer-atomic-idempotent",
	}
	created, err := TransferWalletQuota(sender.Id, request)
	require.NoError(t, err)
	require.Equal(t, "outgoing", created.Direction)
	require.Equal(t, recipient.ExternalId, created.CounterpartyExternalId)
	require.Equal(t, int64(2*unit/100), created.FeeQuota)
	require.Equal(t, int64(2*unit+2*unit/100), created.TotalDebitQuota)
	require.NotContains(t, created.CounterpartyDisplayName, "Recipient")

	replayed, err := TransferWalletQuota(sender.Id, request)
	require.NoError(t, err)
	require.Equal(t, created.Id, replayed.Id)

	var savedSender, savedRecipient identityschema.User
	require.NoError(t, db.First(&savedSender, sender.Id).Error)
	require.NoError(t, db.First(&savedRecipient, recipient.Id).Error)
	require.Equal(t, 798*unit/100, savedSender.ClaudeQuota)
	require.Equal(t, 3*unit, savedRecipient.ClaudeQuota)
	require.Zero(t, savedSender.Quota)

	recipientHistory, err := ListWalletTransfers(recipient.Id, 1, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, recipientHistory.Total)
	require.Equal(t, "incoming", recipientHistory.Items[0].Direction)
	require.Equal(t, sender.ExternalId, recipientHistory.Items[0].CounterpartyExternalId)

	var count int64
	require.NoError(t, db.Model(&commerceschema.WalletTransfer{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestTransferWalletQuotaRejectsUnsafeRecipientsAndInsufficientBalance(t *testing.T) {
	db := setupRedemptionTestDB(t)
	unit := int(platformruntime.QuotaPerUnit)
	sender := &identityschema.User{Id: 9934, ExternalId: "TRS004", Username: "sender-limits", AffCode: "TRS004", Status: constant.UserStatusEnabled, ClaudeQuota: unit}
	disabled := &identityschema.User{Id: 9935, ExternalId: "TRS005", Username: "disabled-recipient", AffCode: "TRS005", Status: constant.UserStatusDisabled}
	require.NoError(t, db.Create(sender).Error)
	require.NoError(t, db.Create(disabled).Error)
	require.NoError(t, ConfigureWalletTransferPassword(sender.Id, "", "Paypass123"))

	_, err := LookupWalletTransferRecipient(sender.Id, sender.ExternalId)
	require.ErrorIs(t, err, commerceschema.ErrWalletTransferSelf)
	_, err = LookupWalletTransferRecipient(sender.Id, disabled.ExternalId)
	require.ErrorIs(t, err, commerceschema.ErrWalletTransferRecipientNotFound)

	_, err = TransferWalletQuota(sender.Id, CreateWalletTransferRequest{
		RecipientExternalId: disabled.ExternalId, AmountQuota: int64(unit),
		PaymentPassword: "Paypass123", RequestId: "wallet-transfer-disabled",
	})
	require.ErrorIs(t, err, commerceschema.ErrWalletTransferRecipientNotFound)

	require.NoError(t, db.Model(&disabled).Update("status", constant.UserStatusEnabled).Error)
	_, err = TransferWalletQuota(sender.Id, CreateWalletTransferRequest{
		RecipientExternalId: disabled.ExternalId, AmountQuota: int64(2 * unit),
		PaymentPassword: "Paypass123", RequestId: "wallet-transfer-insufficient",
	})
	require.ErrorIs(t, err, commerceschema.ErrWalletTransferInsufficientBalance)

	var savedSender, savedRecipient identityschema.User
	require.NoError(t, db.First(&savedSender, sender.Id).Error)
	require.NoError(t, db.First(&savedRecipient, disabled.Id).Error)
	require.Equal(t, unit, savedSender.ClaudeQuota)
	require.Zero(t, savedRecipient.ClaudeQuota)
}
