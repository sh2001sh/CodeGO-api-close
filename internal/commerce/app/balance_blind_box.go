package app

import (
	"errors"
	"strings"
	"time"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const (
	balanceBlindBoxPoolVersion = "unified-box-v4-open-draw"
	balanceBlindBoxMaxBatch    = 100
)

var balanceBlindBoxLocation = time.FixedZone("UTC+8", 8*60*60)

type BalanceBlindBoxOverview struct {
	Enabled                bool                           `json:"enabled"`
	PriceUSD               float64                        `json:"price_usd"`
	BalanceUSD             float64                        `json:"balance_usd"`
	Tiers                  []blindboxsettings.TierSetting `json:"tiers"`
	InventoryCount         int64                          `json:"inventory_count"`
	PurchasedToday         int64                          `json:"purchased_today"`
	DailyPurchaseLimit     int                            `json:"daily_purchase_limit"`
	RemainingPurchaseLimit int64                          `json:"remaining_purchase_limit"`
	PityProgress           int                            `json:"pity_progress"`
	PityThreshold          int                            `json:"pity_threshold"`
	PityGuaranteeUSD       float64                        `json:"pity_guarantee_usd"`
	SmallPityProgress      int                            `json:"small_pity_progress"`
	SmallPityThreshold     int                            `json:"small_pity_threshold"`
	SmallPityGuaranteeUSD  float64                        `json:"small_pity_guarantee_usd"`
	FirstDrawGuaranteeUSD  float64                        `json:"first_draw_guarantee_usd"`
	FirstDrawEligible      bool                           `json:"first_draw_eligible"`
	FirstDrawRewardMinUSD  float64                        `json:"first_draw_reward_min_usd"`
	FirstDrawRewardMaxUSD  float64                        `json:"first_draw_reward_max_usd"`
	SmallPityRewardMinUSD  float64                        `json:"small_pity_reward_min_usd"`
	SmallPityRewardMaxUSD  float64                        `json:"small_pity_reward_max_usd"`
	PityRewardMinUSD       float64                        `json:"pity_reward_min_usd"`
	PityRewardMaxUSD       float64                        `json:"pity_reward_max_usd"`
}

type BalanceBlindBoxOpenResult struct {
	Record     commerceschema.BlindBoxOpenRecord   `json:"record"`
	Records    []commerceschema.BlindBoxOpenRecord `json:"records"`
	BalanceUSD float64                             `json:"balance_usd"`
	Overview   BalanceBlindBoxOverview             `json:"overview"`
}

type BalanceBlindBoxPurchaseResult struct {
	Purchase commerceschema.BalanceBlindBoxPurchase `json:"purchase"`
	Overview BalanceBlindBoxOverview                `json:"overview"`
}

type BalanceBlindBoxGiftResult struct {
	Gift      commerceschema.BalanceBlindBoxGift `json:"gift"`
	Overview  BalanceBlindBoxOverview            `json:"overview"`
	Recipient WalletTransferRecipient            `json:"recipient"`
}

// GetBalanceBlindBoxOverview returns wallet, inventory and issuance state.
func GetBalanceBlindBoxOverview(userID int) (*BalanceBlindBoxOverview, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	setting := blindboxsettings.Get()
	balance, err := billingapp.GetUserClaudeWalletQuota(userID)
	if err != nil {
		return nil, err
	}
	var pity commerceschema.BalanceBlindBoxPityState
	err = platformdb.DB.Where("user_id = ?", userID).First(&pity).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && !isBalanceBlindBoxSchemaMissing(err) {
		return nil, err
	}
	inventory, purchasedToday, openedCount, err := loadBalanceBlindBoxCounts(platformdb.DB, userID)
	if err != nil && !isBalanceBlindBoxSchemaMissing(err) {
		return nil, err
	}
	return buildBalanceBlindBoxOverview(setting, balance, inventory, purchasedToday, pity, openedCount == 0), nil
}

func loadBalanceBlindBoxCounts(db *gorm.DB, userID int) (inventory, purchasedToday, openedCount int64, err error) {
	err = db.Model(&commerceschema.BalanceBlindBoxItem{}).
		Where("owner_user_id = ? AND status = ?", userID, commerceschema.BalanceBlindBoxItemStatusAvailable).
		Count(&inventory).Error
	if err != nil {
		return
	}
	err = db.Model(&commerceschema.BlindBoxOpenRecord{}).
		Where("user_id = ? AND pool_type = ?", userID, commerceschema.BlindBoxPoolTypeUnified).
		Count(&openedCount).Error
	if err != nil {
		return
	}
	err = db.Model(&commerceschema.BalanceBlindBoxPurchase{}).
		Where("user_id = ? AND purchase_date = ?", userID, currentBalanceBlindBoxDate()).
		Select("COALESCE(SUM(quantity), 0)").Scan(&purchasedToday).Error
	if err != nil {
		return
	}
	var pendingCount int64
	pendingCount, err = countPendingPaidBlindBoxOrdersTx(db, userID)
	purchasedToday += pendingCount
	return
}

func buildBalanceBlindBoxOverview(setting blindboxsettings.Setting, balance int, inventory, purchasedToday int64, pity commerceschema.BalanceBlindBoxPityState, firstEligible bool) *BalanceBlindBoxOverview {
	remaining := int64(setting.BalanceBlindBoxDailyPurchaseLimit) - purchasedToday
	if remaining < 0 {
		remaining = 0
	}
	firstMin, firstMax := balanceBlindBoxTierRange(
		setting.BalanceBlindBoxFirstDrawTiers,
		setting.BalanceBlindBoxFirstDrawGuaranteeUSD,
	)
	smallMin, smallMax := balanceBlindBoxTierRange(
		setting.BalanceBlindBoxSmallPityTiers,
		setting.BalanceBlindBoxSmallPityGuaranteeUSD,
	)
	bigMin, bigMax := balanceBlindBoxTierRange(
		setting.BalanceBlindBoxPityTiers,
		setting.BalanceBlindBoxPityGuaranteeUSD,
	)
	return &BalanceBlindBoxOverview{
		Enabled: setting.Enabled && setting.BalanceBlindBoxEnabled, PriceUSD: setting.BalanceBlindBoxPriceUSD,
		BalanceUSD: float64(balance) / float64(platformruntime.QuotaPerUnit), Tiers: setting.BalanceBlindBoxTiers,
		InventoryCount: inventory, PurchasedToday: purchasedToday, DailyPurchaseLimit: setting.BalanceBlindBoxDailyPurchaseLimit,
		RemainingPurchaseLimit: remaining, PityProgress: pity.ConsecutiveUnder35USD,
		PityThreshold: setting.BalanceBlindBoxPityThreshold, PityGuaranteeUSD: setting.BalanceBlindBoxPityGuaranteeUSD,
		SmallPityProgress: pity.ConsecutiveUnder6USD, SmallPityThreshold: setting.BalanceBlindBoxSmallPityThreshold,
		SmallPityGuaranteeUSD: setting.BalanceBlindBoxSmallPityGuaranteeUSD,
		FirstDrawGuaranteeUSD: setting.BalanceBlindBoxFirstDrawGuaranteeUSD, FirstDrawEligible: firstEligible,
		FirstDrawRewardMinUSD: firstMin, FirstDrawRewardMaxUSD: firstMax,
		SmallPityRewardMinUSD: smallMin, SmallPityRewardMaxUSD: smallMax,
		PityRewardMinUSD: bigMin, PityRewardMaxUSD: bigMax,
	}
}

func balanceBlindBoxTierRange(tiers []blindboxsettings.TierSetting, guaranteeUSD float64) (float64, float64) {
	var minimum, maximum float64
	for _, tier := range tiers {
		if blindboxsettings.NormalizeRewardType(tier.RewardType) == commerceschema.BlindBoxRewardTypeProp {
			continue
		}
		if minimum == 0 || tier.MinUSD < minimum {
			minimum = tier.MinUSD
		}
		if tier.MaxUSD > maximum {
			maximum = tier.MaxUSD
		}
	}
	guaranteeMinimum := guaranteeUSD / balanceBlindBoxEquivalentValue(commerceschema.BlindBoxRewardTypeClaudeQuota, 1)
	if minimum < guaranteeMinimum {
		minimum = guaranteeMinimum
	}
	if maximum < guaranteeMinimum {
		maximum = guaranteeMinimum
	}
	return minimum, maximum
}

func currentBalanceBlindBoxDate() string {
	return time.Now().In(balanceBlindBoxLocation).Format("2006-01-02")
}

func isBalanceBlindBoxSchemaMissing(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such table") || strings.Contains(message, "does not exist")
}
