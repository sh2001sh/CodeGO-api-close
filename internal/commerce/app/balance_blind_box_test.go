package app

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/constant"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBalanceBlindBoxPurchaseCreatesSealedInventoryAndDebitsOnce(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setBalanceBlindBoxTestSetting(t, 10)
	user := createBalanceBlindBoxTestUser(t, db, 8891, "BBX001", 100)

	first, err := PurchaseBalanceBlindBoxes(user.Id, "balance-purchase-1", 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), first.Overview.InventoryCount)
	require.InDelta(t, 85, first.Overview.BalanceUSD, 0.000001)

	replayed, err := PurchaseBalanceBlindBoxes(user.Id, "balance-purchase-1", 1)
	require.NoError(t, err)
	require.Equal(t, first.Purchase.Id, replayed.Purchase.Id)
	require.Equal(t, int64(1), replayed.Overview.InventoryCount)
	require.InDelta(t, 85, replayed.Overview.BalanceUSD, 0.000001)

	var item commerceschema.BalanceBlindBoxItem
	require.NoError(t, db.First(&item).Error)
	require.Equal(t, 10.0, item.RewardUSD)
	encoded, err := json.Marshal(item)
	require.NoError(t, err)
	jsonText := string(encoded)
	for _, hidden := range []string{"reward_usd", "reward_type", "reward_tier", "credit_amount", "guarantee_type"} {
		require.NotContains(t, jsonText, hidden)
	}
}

func TestBalanceBlindBoxOpenUsesInventoryWithoutDebitOrLuckyNumber(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setBalanceBlindBoxTestSetting(t, 10)
	user := createBalanceBlindBoxTestUser(t, db, 8892, "BBX002", 100)
	_, err := PurchaseBalanceBlindBoxes(user.Id, "balance-purchase-open", 1)
	require.NoError(t, err)

	opened, err := OpenBalanceBlindBox(user.Id, "balance-open-1", 1)
	require.NoError(t, err)
	require.Equal(t, 10.0, opened.Record.RewardUSD)
	require.Empty(t, opened.Record.LuckyNumber)
	require.InDelta(t, 95, opened.BalanceUSD, 0.000001)
	require.Zero(t, opened.Overview.InventoryCount)

	replayed, err := OpenBalanceBlindBox(user.Id, "balance-open-1", 1)
	require.NoError(t, err)
	require.Equal(t, opened.Record.Id, replayed.Record.Id)
	require.InDelta(t, 95, replayed.BalanceUSD, 0.000001)

	var luckyCount int64
	require.NoError(t, db.Model(&commerceschema.BlindBoxDailyLuckyNumber{}).Count(&luckyCount).Error)
	require.Zero(t, luckyCount)
}

func TestBalanceBlindBoxPurchaseEnforcesDailyLimit(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setBalanceBlindBoxTestSetting(t, 2)
	user := createBalanceBlindBoxTestUser(t, db, 8893, "BBX003", 100)

	_, err := PurchaseBalanceBlindBoxes(user.Id, "balance-limit-1", 2)
	require.NoError(t, err)
	_, err = PurchaseBalanceBlindBoxes(user.Id, "balance-limit-2", 1)
	require.ErrorContains(t, err, "每日最多购买 2 个")

	overview, err := GetBalanceBlindBoxOverview(user.Id)
	require.NoError(t, err)
	require.Equal(t, int64(2), overview.PurchasedToday)
	require.Zero(t, overview.RemainingPurchaseLimit)
	require.Equal(t, int64(2), overview.InventoryCount)
}

func TestBalanceBlindBoxGiftAndRegiftPreserveSealedGuarantee(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setBalanceBlindBoxTestSetting(t, 10)
	purchaser := createBalanceBlindBoxTestUser(t, db, 8894, "BBX004", 100)
	recipient := createBalanceBlindBoxTestUser(t, db, 8895, "BBX005", 0)
	finalOwner := createBalanceBlindBoxTestUser(t, db, 8896, "BBX006", 0)
	_, err := PurchaseBalanceBlindBoxes(purchaser.Id, "balance-gift-purchase", 1)
	require.NoError(t, err)

	firstGift, err := GiftBalanceBlindBoxes(purchaser.Id, GiftBalanceBlindBoxRequest{
		RecipientExternalId: recipient.ExternalId, RequestId: "balance-gift-1", Count: 1,
	})
	require.NoError(t, err)
	require.Equal(t, recipient.ExternalId, firstGift.Recipient.ExternalId)
	require.Zero(t, firstGift.Overview.InventoryCount)

	_, err = GiftBalanceBlindBoxes(recipient.Id, GiftBalanceBlindBoxRequest{
		RecipientExternalId: finalOwner.ExternalId, RequestId: "balance-gift-2", Count: 1,
	})
	require.NoError(t, err)
	opened, err := OpenBalanceBlindBox(finalOwner.Id, "balance-gift-open", 1)
	require.NoError(t, err)
	require.True(t, opened.Record.IsPity)
	require.Equal(t, 10.0, opened.Record.RewardUSD)

	var links []commerceschema.BalanceBlindBoxGiftItem
	require.NoError(t, db.Order("id asc").Find(&links).Error)
	require.Len(t, links, 2)
	require.Equal(t, purchaser.Id, links[0].FromUserId)
	require.Equal(t, recipient.Id, links[0].ToUserId)
	require.Equal(t, recipient.Id, links[1].FromUserId)
	require.Equal(t, finalOwner.Id, links[1].ToUserId)
	require.Equal(t, links[0].ItemId, links[1].ItemId)
}

func TestBalanceBlindBoxSupportsOpeningOneHundredOwnedItems(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setBalanceBlindBoxTestSetting(t, 10)
	user := createBalanceBlindBoxTestUser(t, db, 8897, "BBX007", 0)
	items := make([]commerceschema.BalanceBlindBoxItem, 100)
	for index := range items {
		items[index] = commerceschema.BalanceBlindBoxItem{
			PurchaseUserId: user.Id, OwnerUserId: user.Id, PoolVersion: balanceBlindBoxPoolVersion,
			RewardType: commerceschema.BlindBoxRewardTypeQuota, RewardTier: "$1 普通额度",
			RewardUSD: 1, CreditAmount: quotaUnitsFromBlindBoxUSD(1), RewardTitle: "1.00 美元奖励",
			RewardWalletType: string(commerceschema.BlindBoxRewardWalletTypeDefault), GuaranteeType: balanceBlindBoxGuaranteeNone,
		}
	}
	require.NoError(t, db.Create(&items).Error)

	result, err := OpenBalanceBlindBox(user.Id, "balance-open-100", 100)
	require.NoError(t, err)
	require.Len(t, result.Records, 100)
	require.InDelta(t, 100, result.BalanceUSD, 0.000001)
	require.Zero(t, result.Overview.InventoryCount)
}

func TestBalanceBlindBoxGiftRejectsSelfAndInsufficientInventory(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setBalanceBlindBoxTestSetting(t, 10)
	user := createBalanceBlindBoxTestUser(t, db, 8898, "BBX008", 0)
	other := createBalanceBlindBoxTestUser(t, db, 8899, "BBX009", 0)

	_, err := GiftBalanceBlindBoxes(user.Id, GiftBalanceBlindBoxRequest{RecipientExternalId: user.ExternalId, RequestId: "gift-self", Count: 1})
	require.Error(t, err)
	_, err = GiftBalanceBlindBoxes(user.Id, GiftBalanceBlindBoxRequest{RecipientExternalId: strings.ToLower(other.ExternalId), RequestId: "gift-empty", Count: 1})
	require.ErrorContains(t, err, "库存不足")
}

func setBalanceBlindBoxTestSetting(t *testing.T, dailyLimit int) {
	t.Helper()
	original := blindboxsettings.Get()
	setting := original
	setting.Enabled = true
	setting.BalanceBlindBoxEnabled = true
	setting.BalanceBlindBoxPriceUSD = 15
	setting.BalanceBlindBoxDailyPurchaseLimit = dailyLimit
	setting.BalanceBlindBoxPityThreshold = 50
	setting.BalanceBlindBoxSmallPityThreshold = 10
	setting.BalanceBlindBoxPityGuaranteeUSD = 35
	setting.BalanceBlindBoxSmallPityGuaranteeUSD = 10
	setting.BalanceBlindBoxFirstDrawGuaranteeUSD = 10
	setting.BalanceBlindBoxTiers = []blindboxsettings.TierSetting{{
		Name: "$1 普通额度", MinUSD: 1, MaxUSD: 1, Probability: 1,
		RewardType: commerceschema.BlindBoxRewardTypeQuota, WalletType: "default",
	}}
	blindboxsettings.Set(setting)
	t.Cleanup(func() { blindboxsettings.Set(original) })
}

func createBalanceBlindBoxTestUser(t *testing.T, db *gorm.DB, id int, externalID string, balanceUSD float64) *identityschema.User {
	t.Helper()
	user := &identityschema.User{
		Id: id, Username: "balance_box_" + externalID, ExternalId: externalID,
		Quota:   int(math.Round(balanceUSD * float64(platformruntime.QuotaPerUnit))),
		AffCode: "aff-" + externalID, Status: constant.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)
	return user
}
