package app

import (
	"errors"
	"fmt"
	"strings"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

type BalanceBlindBoxOverview struct {
	Enabled               bool                           `json:"enabled"`
	PriceUSD              float64                        `json:"price_usd"`
	BalanceUSD            float64                        `json:"balance_usd"`
	Tiers                 []blindboxsettings.TierSetting `json:"tiers"`
	PityProgress          int                            `json:"pity_progress"`
	PityThreshold         int                            `json:"pity_threshold"`
	PityGuaranteeUSD      float64                        `json:"pity_guarantee_usd"`
	SmallPityProgress     int                            `json:"small_pity_progress"`
	SmallPityThreshold    int                            `json:"small_pity_threshold"`
	SmallPityGuaranteeUSD float64                        `json:"small_pity_guarantee_usd"`
	FirstDrawGuaranteeUSD float64                        `json:"first_draw_guarantee_usd"`
	FirstDrawEligible     bool                           `json:"first_draw_eligible"`
}

type BalanceBlindBoxOpenResult struct {
	Record     commerceschema.BlindBoxOpenRecord   `json:"record"`
	Records    []commerceschema.BlindBoxOpenRecord `json:"records"`
	BalanceUSD float64                             `json:"balance_usd"`
	Overview   BalanceBlindBoxOverview             `json:"overview"`
}

// GetBalanceBlindBoxOverview returns the balance draw price, pool and independent pity state.
func GetBalanceBlindBoxOverview(userID int) (*BalanceBlindBoxOverview, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	setting := blindboxsettings.Get()
	balance, err := billingapp.GetUserWalletQuota(userID)
	if err != nil {
		return nil, err
	}
	var pity commerceschema.BalanceBlindBoxPityState
	err = platformdb.DB.Where("user_id = ?", userID).First(&pity).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && !isBalanceBlindBoxSchemaMissing(err) {
		return nil, err
	}
	var openedCount int64
	if err := platformdb.DB.Model(&commerceschema.BlindBoxOpenRecord{}).
		Where("user_id = ? AND pool_type = ?", userID, commerceschema.BlindBoxPoolTypeBalance15).
		Count(&openedCount).Error; err != nil {
		return nil, err
	}
	return buildBalanceBlindBoxOverview(setting, balance, pity.ConsecutiveUnder6USD, pity.ConsecutiveUnder35USD, openedCount == 0), nil
}

func isBalanceBlindBoxSchemaMissing(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such table") || strings.Contains(message, "does not exist")
}

// OpenBalanceBlindBox atomically spends $15 wallet balance and grants one reward.
func OpenBalanceBlindBox(userID int, requestID string, requestedCount ...int) (*BalanceBlindBoxOpenResult, error) {
	if userID <= 0 || requestID == "" || len(requestID) > 64 {
		return nil, errors.New("invalid balance blind box request")
	}
	setting := blindboxsettings.Get()
	if !setting.Enabled || !setting.BalanceBlindBoxEnabled {
		return nil, commercedomain.ErrBlindBoxDisabled
	}
	count := 1
	if len(requestedCount) > 0 && requestedCount[0] > 0 {
		count = requestedCount[0]
	}
	if count > 100 {
		return nil, errors.New("余额盲盒单次最多开启 100 个")
	}
	priceQuota := quotaUnitsFromBlindBoxUSD(setting.BalanceBlindBoxPriceUSD)
	if priceQuota <= 0 {
		return nil, errors.New("invalid balance blind box price")
	}

	result := &BalanceBlindBoxOpenResult{Records: make([]commerceschema.BlindBoxOpenRecord, 0, count)}
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var existingRecords []commerceschema.BlindBoxOpenRecord
		if err := tx.Where("user_id = ? AND (request_id = ? OR request_id LIKE ?)", userID, requestID, requestID+":%").Order("id asc").Find(&existingRecords).Error; err == nil && len(existingRecords) > 0 {
			result.Record = existingRecords[0]
			result.Records = existingRecords
			return nil
		} else if err != nil {
			return err
		}
		var openedCount int64
		if err := tx.Model(&commerceschema.BlindBoxOpenRecord{}).
			Where("user_id = ? AND pool_type = ?", userID, commerceschema.BlindBoxPoolTypeBalance15).
			Count(&openedCount).Error; err != nil {
			return err
		}

		var pity commerceschema.BalanceBlindBoxPityState
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where("user_id = ?", userID).First(&pity).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			pity = commerceschema.BalanceBlindBoxPityState{UserId: userID}
			if err := tx.Create(&pity).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		operationID := "balance-blind-box:" + requestID
		if err := billingapp.DebitWalletQuotaTx(tx, userID, int(priceQuota)*count, operationID+":debit"); err != nil {
			if errors.Is(err, billingdomain.ErrInsufficientBalance) {
				return errors.New("钱包余额不足，无法抽取余额盲盒")
			}
			return err
		}

		for index := 0; index < count; index++ {
			drawRequestID := requestID
			if index > 0 {
				drawRequestID = fmt.Sprintf("%s:%d", requestID, index)
			}
			record, drawErr := drawBalanceBlindBoxRewardTx(tx, userID, drawRequestID, &pity, setting, openedCount == 0 && index == 0 && pity.ConsecutiveUnder6USD == 0 && pity.ConsecutiveUnder35USD == 0)
			if drawErr != nil {
				return drawErr
			}
			result.Records = append(result.Records, *record)
		}
		result.Record = result.Records[0]
		return tx.Save(&pity).Error
	})
	if err != nil {
		return nil, err
	}
	_ = identitystore.InvalidateUserCache(userID)
	overview, err := GetBalanceBlindBoxOverview(userID)
	if err != nil {
		return nil, err
	}
	result.BalanceUSD = overview.BalanceUSD
	result.Overview = *overview
	return result, nil
}

func drawBalanceBlindBoxRewardTx(tx *gorm.DB, userID int, requestID string, pity *commerceschema.BalanceBlindBoxPityState, setting blindboxsettings.Setting, firstDraw bool) (*commerceschema.BlindBoxOpenRecord, error) {
	tier := pickBlindBoxTier(setting.BalanceBlindBoxTiers)
	rewardUSD := randomTierRewardUSD(tier)
	rewardType := blindboxsettings.NormalizeRewardType(tier.RewardType)
	walletType := normalizeBlindBoxRewardWalletType(tier.WalletType)
	firstDrawTriggered := firstDraw
	bigPityTriggered := !firstDrawTriggered && pity.ConsecutiveUnder35USD+1 >= setting.BalanceBlindBoxPityThreshold
	smallPityTriggered := !bigPityTriggered && pity.ConsecutiveUnder6USD+1 >= setting.BalanceBlindBoxSmallPityThreshold
	if firstDrawTriggered {
		tier = blindboxsettings.TierSetting{Name: "$10 余额盲盒首抽保底", MinUSD: setting.BalanceBlindBoxFirstDrawGuaranteeUSD, MaxUSD: setting.BalanceBlindBoxFirstDrawGuaranteeUSD, RewardType: commerceschema.BlindBoxRewardTypeQuota, WalletType: string(commerceschema.BlindBoxRewardWalletTypeDefault)}
		rewardUSD = setting.BalanceBlindBoxFirstDrawGuaranteeUSD
		rewardType = commerceschema.BlindBoxRewardTypeQuota
		walletType = commerceschema.BlindBoxRewardWalletTypeDefault
	} else if bigPityTriggered {
		tier = blindboxsettings.TierSetting{Name: "$35 余额盲盒保底", RewardType: commerceschema.BlindBoxRewardTypeQuota, WalletType: string(commerceschema.BlindBoxRewardWalletTypeDefault)}
		rewardUSD = setting.BalanceBlindBoxPityGuaranteeUSD
		rewardType = commerceschema.BlindBoxRewardTypeQuota
		walletType = commerceschema.BlindBoxRewardWalletTypeDefault
	} else if smallPityTriggered {
		tier = blindboxsettings.TierSetting{Name: "$10 余额盲盒小保底", MinUSD: setting.BalanceBlindBoxSmallPityGuaranteeUSD, MaxUSD: setting.BalanceBlindBoxSmallPityGuaranteeUSD, RewardType: commerceschema.BlindBoxRewardTypeQuota, WalletType: string(commerceschema.BlindBoxRewardWalletTypeDefault)}
		rewardUSD = setting.BalanceBlindBoxSmallPityGuaranteeUSD
		rewardType = commerceschema.BlindBoxRewardTypeQuota
		walletType = commerceschema.BlindBoxRewardWalletTypeDefault
	}
	record := &commerceschema.BlindBoxOpenRecord{
		UserId: userID, RequestId: &requestID, PoolType: commerceschema.BlindBoxPoolTypeBalance15,
		RewardType: rewardType, RewardTier: tier.Name, RewardUSD: rewardUSD,
		RewardWalletType: string(walletType), IsPity: firstDrawTriggered || bigPityTriggered || smallPityTriggered, CreateTime: platformruntime.GetTimestamp(),
	}
	if rewardType == commerceschema.BlindBoxRewardTypeProp {
		record.RewardTitle = tier.Name
		record.RewardUSD = 0
		record.RewardWalletType = ""
	} else {
		record.CreditAmount = quotaUnitsFromBlindBoxUSD(rewardUSD)
		if record.CreditAmount <= 0 {
			return nil, fmt.Errorf("invalid balance blind box reward: %.2f", rewardUSD)
		}
		if rewardType == commerceschema.BlindBoxRewardTypeClaudeQuota {
			record.RewardTitle = fmt.Sprintf("%.2f Claude 额度奖励", rewardUSD)
		} else {
			record.RewardTitle = fmt.Sprintf("%.2f 美元奖励", rewardUSD)
		}
	}
	// Direct creation intentionally bypasses createBlindBoxOpenRecordTx: balance draws never issue lucky numbers.
	if err := tx.Create(record).Error; err != nil {
		return nil, err
	}
	if rewardType == commerceschema.BlindBoxRewardTypeProp {
		prop, err := createBlindBoxPropTx(tx, userID, record.Id, record.RewardTitle)
		if err != nil {
			return nil, err
		}
		record.PropId, record.PropType, record.PropStatus, record.PropExpiresAt = prop.Id, prop.PropType, prop.Status, prop.ExpiresAt
	} else {
		if err := applyBlindBoxWalletRewardTx(tx, userID, record.Id, record.CreditAmount, walletType); err != nil {
			return nil, err
		}
		if err := recordBlindBoxRewardLogTx(tx, userID, record.CreditAmount, walletType, record); err != nil {
			return nil, err
		}
	}
	if rewardType != commerceschema.BlindBoxRewardTypeProp && (firstDrawTriggered || rewardUSD >= setting.BalanceBlindBoxPityGuaranteeUSD) {
		pity.ConsecutiveUnder6USD = 0
		pity.ConsecutiveUnder35USD = 0
	} else if rewardType != commerceschema.BlindBoxRewardTypeProp && rewardUSD >= setting.BalanceBlindBoxSmallPityGuaranteeUSD {
		pity.ConsecutiveUnder6USD = 0
		pity.ConsecutiveUnder35USD++
	} else {
		pity.ConsecutiveUnder6USD++
		pity.ConsecutiveUnder35USD++
	}
	return record, nil
}

func buildBalanceBlindBoxOverview(setting blindboxsettings.Setting, balance int, smallPityProgress int, pityProgress int, firstDrawEligible bool) *BalanceBlindBoxOverview {
	return &BalanceBlindBoxOverview{
		Enabled:               setting.Enabled && setting.BalanceBlindBoxEnabled,
		PriceUSD:              setting.BalanceBlindBoxPriceUSD,
		BalanceUSD:            float64(balance) / float64(platformruntime.QuotaPerUnit),
		Tiers:                 setting.BalanceBlindBoxTiers,
		PityProgress:          pityProgress,
		PityThreshold:         setting.BalanceBlindBoxPityThreshold,
		PityGuaranteeUSD:      setting.BalanceBlindBoxPityGuaranteeUSD,
		SmallPityProgress:     smallPityProgress,
		SmallPityThreshold:    setting.BalanceBlindBoxSmallPityThreshold,
		SmallPityGuaranteeUSD: setting.BalanceBlindBoxSmallPityGuaranteeUSD,
		FirstDrawGuaranteeUSD: setting.BalanceBlindBoxFirstDrawGuaranteeUSD,
		FirstDrawEligible:     firstDrawEligible,
	}
}
