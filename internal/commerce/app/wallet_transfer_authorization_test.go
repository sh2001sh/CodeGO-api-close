package app

import (
	"context"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAuthorizedWalletTransferHasNoPasswordAndFreezesOriginalParameters(t *testing.T) {
	db := setupRedemptionTestDB(t)
	unit := int64(platformruntime.QuotaPerUnit)
	sender := identityschema.User{Id: 9921, ExternalId: "AUS001", Username: "authorized-sender", AffCode: "AUS001", Status: constant.UserStatusEnabled, ClaudeQuota: int(10 * unit)}
	recipient := identityschema.User{Id: 9922, ExternalId: "AUS002", Username: "authorized-recipient", AffCode: "AUS002", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(&sender).Error)
	require.NoError(t, db.Create(&recipient).Error)
	require.NoError(t, ConfigureWalletTransferPassword(sender.Id, "", "Paypass123"))
	req := CreateWalletTransferRequest{RecipientExternalId: " aus002 ", AmountQuota: unit,
		RequestId: " authorized-snapshot ", PaymentPassword: "Paypass123"}
	authorized, err := AuthorizeWalletTransfer(context.Background(), sender.Id, req)
	require.NoError(t, err)
	require.Empty(t, authorized.request.PaymentPassword)
	req.AmountQuota, req.RequestId, req.RecipientExternalId = unit*2, "mutated-id", "AUS003"
	var actualDeadline time.Time
	callback := "test:authorized-deadline"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "users" && actualDeadline.IsZero() {
			actualDeadline, _ = tx.Statement.Context.Deadline()
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
	started := time.Now()
	item, err := TransferAuthorizedWalletQuota(context.Background(), authorized)
	require.NoError(t, err)
	require.Equal(t, "authorized-snapshot", item.RequestId)
	require.Equal(t, unit, item.AmountQuota)
	require.Equal(t, recipient.ExternalId, item.CounterpartyExternalId)
	require.Greater(t, actualDeadline.Sub(started), 170*time.Second)
	require.LessOrEqual(t, actualDeadline.Sub(started), WalletTransferTimeout+time.Second)
	lookup, err := LookupWalletTransferReceipt(context.Background(), sender.Id, item.RequestId)
	require.NoError(t, err)
	require.Equal(t, item, lookup)
	for _, userID := range []int{recipient.Id, sender.Id + 100} {
		lookup, err = LookupWalletTransferReceipt(context.Background(), userID, item.RequestId)
		require.NoError(t, err)
		require.Nil(t, lookup)
	}
	for _, invalid := range []*AuthorizedWalletTransfer{nil, {}} {
		_, err = TransferAuthorizedWalletQuota(context.Background(), invalid)
		require.ErrorIs(t, err, commerceschema.ErrWalletTransferInvalid)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = TransferAuthorizedWalletQuota(canceled, authorized)
	require.ErrorIs(t, err, context.Canceled, "synchronous callers retain their own cancellation")
}

func TestAuthorizedWalletTransferRechecksLiveUsersBeforeAnyFundsWrite(t *testing.T) {
	for _, state := range []string{"sender-disabled", "recipient-disabled", "sender-deleted", "recipient-deleted"} {
		t.Run(state, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			unit := int64(platformruntime.QuotaPerUnit)
			sender := identityschema.User{Id: 9923, ExternalId: "ACT001", Username: "active-sender", AffCode: "ACT001", Status: constant.UserStatusEnabled, ClaudeQuota: int(10 * unit)}
			recipient := identityschema.User{Id: 9924, ExternalId: "ACT002", Username: "active-recipient", AffCode: "ACT002", Status: constant.UserStatusEnabled}
			require.NoError(t, db.Create(&sender).Error)
			require.NoError(t, db.Create(&recipient).Error)
			require.NoError(t, ConfigureWalletTransferPassword(sender.Id, "", "Paypass123"))
			req := CreateWalletTransferRequest{RecipientExternalId: recipient.ExternalId, AmountQuota: unit, RequestId: state, PaymentPassword: "Paypass123"}
			authorized, err := AuthorizeWalletTransfer(context.Background(), sender.Id, req)
			require.NoError(t, err)
			changed := &sender
			if state == "recipient-disabled" || state == "recipient-deleted" {
				changed = &recipient
			}
			if state == "sender-deleted" || state == "recipient-deleted" {
				require.NoError(t, db.Delete(changed).Error)
			} else {
				require.NoError(t, db.Model(changed).Update("status", constant.UserStatusDisabled).Error)
			}
			_, err = TransferAuthorizedWalletQuota(context.Background(), authorized)
			require.ErrorIs(t, err, commerceschema.ErrWalletTransferRecipientNotFound)
			for _, model := range []any{&commerceschema.WalletTransfer{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingAccount{}, &billingschema.FundingLot{}} {
				var count int64
				require.NoError(t, db.Model(model).Count(&count).Error)
				require.Zero(t, count, "rejected authorization must not initialize or mutate money")
			}
			var from, to identityschema.User
			require.NoError(t, db.Unscoped().First(&from, sender.Id).Error)
			require.NoError(t, db.Unscoped().First(&to, recipient.Id).Error)
			require.Equal(t, int(10*unit), from.ClaudeQuota)
			require.Zero(t, to.ClaudeQuota)
		})
	}
}
