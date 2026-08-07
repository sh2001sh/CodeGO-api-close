package app

import (
	"testing"

	"github.com/sh2001sh/new-api/constant"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestConvertWalletQuotaSupportsBothDirectionsAndIdempotency(t *testing.T) {
	db := setupRedemptionTestDB(t)
	unit := int(platformruntime.QuotaPerUnit)
	user := &identityschema.User{
		Id:          9911,
		Username:    "wallet-conversion-user",
		Status:      constant.UserStatusEnabled,
		Quota:       8 * unit,
		ClaudeQuota: 2 * unit,
	}
	require.NoError(t, db.Create(user).Error)

	standardToClaude, err := ConvertWalletQuota(user.Id, CreateWalletQuotaConversionRequest{
		Direction:   commerceschema.WalletQuotaConversionStandardToClaude,
		SourceQuota: int64(4 * unit),
		RequestId:   "wallet-conversion-standard-to-claude",
	})
	require.NoError(t, err)
	require.Equal(t, int64(unit), standardToClaude.TargetQuota)
	require.Equal(t, int64(4*unit), standardToClaude.StandardQuotaAfter)
	require.Equal(t, int64(3*unit), standardToClaude.ClaudeQuotaAfter)

	duplicate, err := ConvertWalletQuota(user.Id, CreateWalletQuotaConversionRequest{
		Direction:   commerceschema.WalletQuotaConversionStandardToClaude,
		SourceQuota: int64(4 * unit),
		RequestId:   "wallet-conversion-standard-to-claude",
	})
	require.NoError(t, err)
	require.Equal(t, standardToClaude.Id, duplicate.Id)

	claudeToStandard, err := ConvertWalletQuota(user.Id, CreateWalletQuotaConversionRequest{
		Direction:   commerceschema.WalletQuotaConversionClaudeToStandard,
		SourceQuota: int64(unit),
		RequestId:   "wallet-conversion-claude-to-standard",
	})
	require.NoError(t, err)
	require.Equal(t, int64(4*unit), claudeToStandard.TargetQuota)
	require.Equal(t, int64(8*unit), claudeToStandard.StandardQuotaAfter)
	require.Equal(t, int64(2*unit), claudeToStandard.ClaudeQuotaAfter)

	var saved identityschema.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&saved).Error)
	require.Equal(t, 8*unit, saved.Quota)
	require.Equal(t, 2*unit, saved.ClaudeQuota)
	require.Equal(t, int64(saved.Quota), loadCommerceBillingSnapshot(t, user.Id, "wallet").AvailableBalance)
	require.Equal(t, int64(saved.ClaudeQuota), loadCommerceBillingSnapshot(t, user.Id, "claude_wallet").AvailableBalance)

	var count int64
	require.NoError(t, db.Model(&commerceschema.WalletQuotaConversion{}).Where("user_id = ?", user.Id).Count(&count).Error)
	require.Equal(t, int64(2), count)
}

func TestConvertWalletQuotaRejectsInexactAndInsufficientAmounts(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{
		Id:          9912,
		Username:    "wallet-conversion-limits",
		Status:      constant.UserStatusEnabled,
		Quota:       100,
		ClaudeQuota: 25,
	}
	require.NoError(t, db.Create(user).Error)

	_, err := ConvertWalletQuota(user.Id, CreateWalletQuotaConversionRequest{
		Direction:   commerceschema.WalletQuotaConversionStandardToClaude,
		SourceQuota: 99,
		RequestId:   "wallet-conversion-inexact",
	})
	require.ErrorIs(t, err, commerceschema.ErrWalletQuotaConversionInexact)

	_, err = ConvertWalletQuota(user.Id, CreateWalletQuotaConversionRequest{
		Direction:   commerceschema.WalletQuotaConversionClaudeToStandard,
		SourceQuota: 26,
		RequestId:   "wallet-conversion-insufficient",
	})
	require.ErrorIs(t, err, commerceschema.ErrWalletQuotaConversionInsufficient)

	var saved identityschema.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&saved).Error)
	require.Equal(t, 100, saved.Quota)
	require.Equal(t, 25, saved.ClaudeQuota)

	var count int64
	require.NoError(t, db.Model(&commerceschema.WalletQuotaConversion{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestConvertWalletQuotaUsesLedgerBalanceShownToUser(t *testing.T) {
	db := setupRedemptionTestDB(t)
	user := &identityschema.User{
		Id:       9913,
		Username: "wallet-conversion-ledger-balance",
		Status:   constant.UserStatusEnabled,
		Quota:    100,
	}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return billingapp.CreditWalletQuotaTx(tx, user.Id, 300, "wallet-conversion-ledger-credit", "test_credit")
	}))
	// Reproduce an asynchronous compatibility projection lag: the page reads
	// the ledger snapshot (400), while the old conversion path read 100 here.
	require.NoError(t, db.Model(&identityschema.User{}).Where("id = ?", user.Id).UpdateColumn("quota", 100).Error)

	conversion, err := ConvertWalletQuota(user.Id, CreateWalletQuotaConversionRequest{
		Direction:   commerceschema.WalletQuotaConversionStandardToClaude,
		SourceQuota: 400,
		RequestId:   "wallet-conversion-ledger-consistent",
	})
	require.NoError(t, err)
	require.EqualValues(t, 100, conversion.TargetQuota)

	var saved identityschema.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&saved).Error)
	require.Zero(t, saved.Quota)
	require.EqualValues(t, 100, saved.ClaudeQuota)
	require.Zero(t, loadCommerceBillingSnapshot(t, user.Id, "wallet").AvailableBalance)
	require.EqualValues(t, 100, loadCommerceBillingSnapshot(t, user.Id, "claude_wallet").AvailableBalance)
}
