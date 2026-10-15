package app

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

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
	require.InDelta(t, 97.5, first.Overview.BalanceUSD, 0.000001)

	replayed, err := PurchaseBalanceBlindBoxes(user.Id, "balance-purchase-1", 1)
	require.NoError(t, err)
	require.Equal(t, first.Purchase.Id, replayed.Purchase.Id)
	require.Equal(t, int64(1), replayed.Overview.InventoryCount)
	require.InDelta(t, 97.5, replayed.Overview.BalanceUSD, 0.000001)

	var item commerceschema.BalanceBlindBoxItem
	require.NoError(t, db.First(&item).Error)
	require.Equal(t, balanceBlindBoxPoolVersion, item.PoolVersion)
	require.Empty(t, item.RewardType)
	require.Empty(t, item.RewardTier)
	require.Empty(t, item.RewardWalletType)
	encoded, err := json.Marshal(item)
	require.NoError(t, err)
	jsonText := string(encoded)
	for _, hidden := range []string{"reward_usd", "reward_type", "reward_tier", "credit_amount", "guarantee_type"} {
		require.NotContains(t, jsonText, hidden)
	}
}

func TestBalanceBlindBoxOpenUsesInventoryWithoutDebitAndIssuesLuckyNumber(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setBalanceBlindBoxTestSetting(t, 10)
	user := createBalanceBlindBoxTestUser(t, db, 8892, "BBX002", 100)
	_, err := PurchaseBalanceBlindBoxes(user.Id, "balance-purchase-open", 1)
	require.NoError(t, err)

	opened, err := OpenBalanceBlindBox(user.Id, "balance-open-1", 1)
	require.NoError(t, err)
	if opened.Record.RewardType != commerceschema.BlindBoxRewardTypeProp {
		require.GreaterOrEqual(t, opened.Record.RewardUSD, 1.0)
	}
	require.Regexp(t, `^\d{4}$`, opened.Record.LuckyNumber)
	require.NotEmpty(t, opened.Record.LuckyDrawDate)
	require.Greater(t, opened.Record.LuckyExpiresAt, time.Now().Unix())
	require.GreaterOrEqual(t, opened.BalanceUSD, 97.5)
	require.Zero(t, opened.Overview.InventoryCount)

	replayed, err := OpenBalanceBlindBox(user.Id, "balance-open-1", 1)
	require.NoError(t, err)
	require.Equal(t, opened.Record.Id, replayed.Record.Id)
	require.InDelta(t, opened.BalanceUSD, replayed.BalanceUSD, 0.000001)

	var luckyCount int64
	require.NoError(t, db.Model(&commerceschema.BlindBoxDailyLuckyNumber{}).Count(&luckyCount).Error)
	require.Equal(t, int64(1), luckyCount)
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

func TestBalanceBlindBoxPurchaseCountsPendingCashOrdersAgainstDailyLimit(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setBalanceBlindBoxTestSetting(t, 10)
	user := createBalanceBlindBoxTestUser(t, db, 8902, "BBX012", 100)
	require.NoError(t, db.Create(&commerceschema.BlindBoxOrder{
		UserId: user.Id, Quantity: 10, Money: 25, TradeNo: "pending-cash-limit",
		Status: constant.TopUpStatusPending, CreateTime: platformruntime.GetTimestamp(),
	}).Error)

	_, err := PurchaseBalanceBlindBoxes(user.Id, "balance-after-pending-cash", 1)
	require.ErrorContains(t, err, "今日还可购买 0 个")
	overview, err := GetBalanceBlindBoxOverview(user.Id)
	require.NoError(t, err)
	require.Equal(t, int64(10), overview.PurchasedToday)
	require.Zero(t, overview.RemainingPurchaseLimit)
}

func TestBalanceBlindBoxGiftAndRegiftDrawsForFinalOwnerOnOpen(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setBalanceBlindBoxTestSetting(t, 10)
	purchaser := createBalanceBlindBoxTestUser(t, db, 8894, "BBX004", 100)
	recipient := createBalanceBlindBoxTestUser(t, db, 8895, "BBX005", 0)
	finalOwner := createBalanceBlindBoxTestUser(t, db, 8896, "BBX006", 0)
	_, err := PurchaseBalanceBlindBoxes(purchaser.Id, "balance-gift-purchase", 1)
	require.NoError(t, err)
	var sealed commerceschema.BalanceBlindBoxItem
	require.NoError(t, db.Where("purchase_user_id = ?", purchaser.Id).First(&sealed).Error)

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
	require.Empty(t, sealed.RewardType)
	require.True(t, opened.Record.IsPity)
	if opened.Record.RewardType != commerceschema.BlindBoxRewardTypeProp {
		require.GreaterOrEqual(t, opened.Record.RewardUSD, 1.0)
	}

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
			PurchaseUserId: user.Id, OwnerUserId: user.Id, PoolVersion: "unified-box-v3",
			RewardType: commerceschema.BlindBoxRewardTypeClaudeQuota, RewardTier: "1 统一额度",
			RewardUSD: 1, CreditAmount: quotaUnitsFromBlindBoxUSD(1), RewardTitle: "1.00 美元奖励",
			RewardWalletType: string(commerceschema.BlindBoxRewardWalletTypeClaude), GuaranteeType: balanceBlindBoxGuaranteeNone,
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

func TestBalanceBlindBoxPityUsesClaudeEquivalentValue(t *testing.T) {
	setting := blindboxsettings.Get()
	setting.BalanceBlindBoxSmallPityGuaranteeUSD = 10
	setting.BalanceBlindBoxPityGuaranteeUSD = 35
	pity := commerceschema.BalanceBlindBoxPityState{
		ConsecutiveUnder6USD:  9,
		ConsecutiveUnder35USD: 10,
	}

	advanceBalanceBlindBoxPity(
		&pity,
		commerceschema.BlindBoxRewardTypeClaudeQuota,
		4,
		setting,
	)
	require.Zero(t, pity.ConsecutiveUnder6USD)
	require.Equal(t, 11, pity.ConsecutiveUnder35USD)

	advanceBalanceBlindBoxPity(
		&pity,
		commerceschema.BlindBoxRewardTypeClaudeQuota,
		10,
		setting,
	)
	require.Zero(t, pity.ConsecutiveUnder6USD)
	require.Zero(t, pity.ConsecutiveUnder35USD)
}

func TestBalanceBlindBoxPityAdvancesOnlyAfterOpen(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setBalanceBlindBoxTestSetting(t, 10)
	setting := blindboxsettings.Get()
	setting.BalanceBlindBoxTiers = fixedGuaranteePool("low", 0.2)
	blindboxsettings.Set(setting)
	user := createBalanceBlindBoxTestUser(t, db, 8900, "BBX010", 100)
	require.NoError(t, db.Create(&commerceschema.BlindBoxOpenRecord{
		UserId: user.Id, PoolType: commerceschema.BlindBoxPoolTypeUnified,
		RewardType: commerceschema.BlindBoxRewardTypeClaudeQuota, RewardTitle: "历史奖励",
	}).Error)

	purchased, err := PurchaseBalanceBlindBoxes(user.Id, "pity-open-only-purchase", 1)
	require.NoError(t, err)
	require.Zero(t, purchased.Overview.SmallPityProgress)
	require.Zero(t, purchased.Overview.PityProgress)

	opened, err := OpenBalanceBlindBox(user.Id, "pity-open-only-open", 1)
	require.NoError(t, err)
	require.Equal(t, 1, opened.Overview.SmallPityProgress)
	require.Equal(t, 1, opened.Overview.PityProgress)
}

func TestBalanceBlindBoxExtraDrawAddsInventoryWithoutPurchaseCount(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setBalanceBlindBoxTestSetting(t, 10)
	setting := blindboxsettings.Get()
	setting.BalanceBlindBoxFirstDrawTiers = []blindboxsettings.TierSetting{{
		Name: "再来一抽", Probability: 1, RewardType: commerceschema.BlindBoxRewardTypeProp,
	}}
	blindboxsettings.Set(setting)
	user := createBalanceBlindBoxTestUser(t, db, 8901, "BBX011", 100)

	_, err := PurchaseBalanceBlindBoxes(user.Id, "extra-draw-purchase", 1)
	require.NoError(t, err)
	opened, err := OpenBalanceBlindBox(user.Id, "extra-draw-open", 1)
	require.NoError(t, err)
	require.Equal(t, "再来一抽", opened.Record.RewardTitle)
	require.Equal(t, commerceschema.BlindBoxPropTypeExtraDraw, opened.Record.PropType)
	require.Equal(t, commerceschema.BlindBoxPropStatusUsed, opened.Record.PropStatus)
	require.Equal(t, int64(1), opened.Overview.InventoryCount)
	require.Equal(t, int64(1), opened.Overview.PurchasedToday)
	require.Equal(t, 1, opened.Overview.SmallPityProgress)
	require.Equal(t, 1, opened.Overview.PityProgress)

	var propCount int64
	require.NoError(t, db.Model(&commerceschema.BlindBoxProp{}).Count(&propCount).Error)
	require.Zero(t, propCount)
}

func setBalanceBlindBoxTestSetting(t *testing.T, dailyLimit int) {
	t.Helper()
	original := blindboxsettings.Get()
	setting := original
	setting.Enabled = true
	setting.BalanceBlindBoxEnabled = true
	setting.BalanceBlindBoxPriceUSD = 2.5
	setting.BalanceBlindBoxDailyPurchaseLimit = dailyLimit
	setting.BalanceBlindBoxPityThreshold = 50
	setting.BalanceBlindBoxSmallPityThreshold = 10
	setting.BalanceBlindBoxPityGuaranteeUSD = 35
	setting.BalanceBlindBoxSmallPityGuaranteeUSD = 10
	setting.BalanceBlindBoxFirstDrawGuaranteeUSD = 10
	blindboxsettings.Set(setting)
	t.Cleanup(func() { blindboxsettings.Set(original) })
}

func createBalanceBlindBoxTestUser(t *testing.T, db *gorm.DB, id int, externalID string, balanceUSD float64) *identityschema.User {
	t.Helper()
	user := &identityschema.User{
		Id: id, Username: "balance_box_" + externalID, ExternalId: externalID,
		ClaudeQuota: int(math.Round(balanceUSD * float64(platformruntime.QuotaPerUnit))),
		AffCode:     "aff-" + externalID, Status: constant.UserStatusEnabled,
		CreatedAt: platformruntime.GetTimestamp() - int64((73 * time.Hour).Seconds()),
	}
	require.NoError(t, db.Create(user).Error)
	return user
}
