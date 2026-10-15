package app

import (
	"testing"
	"time"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestConsumeWalletRewardHoldsReducesTransferLock(t *testing.T) {
	db := setupRedemptionTestDB(t)
	require.NoError(t, db.AutoMigrate(&billingschema.WalletRewardHold{}))
	unit := int64(platformruntime.QuotaPerUnit)
	now := time.Now().Unix()
	user := &identityschema.User{Id: 9941, ExternalId: "WRH001", Username: "wallet-reward-hold", Status: 1, CreatedAt: now - int64(time.Hour.Seconds())}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := billingapp.CreditClaudeWalletQuotaTx(tx, user.Id, int(5*unit), "wallet-reward-credit", "blind_box_reward"); err != nil {
			return err
		}
		return billingapp.CreateWalletRewardHoldTx(tx, user.Id, 5*unit, "wallet-reward-credit")
	}))
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		return billingapp.EnsureWalletTransferQuotaTx(tx, user.Id, unit)
	}), billingapp.ErrWalletRewardTransferLocked)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return billingapp.DebitClaudeWalletQuotaTxWithReason(tx, user.Id, int(2*unit), "wallet-reward-use", "unified_blind_box_purchase")
	}))
	var hold billingschema.WalletRewardHold
	require.NoError(t, db.First(&hold, "idempotency_key = ?", "wallet-reward-credit").Error)
	require.Equal(t, int64(2*unit), hold.ConsumedAmount)
}
