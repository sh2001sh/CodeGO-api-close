package app

import (
	"errors"
	"fmt"
	"github.com/sh2001sh/new-api/constant"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"math/rand"
	"strings"
	// OpenBlindBoxes opens the requested number of blind boxes for the user.
)

func OpenBlindBoxes(userID int, count int) ([]commerceschema.BlindBoxOpenRecord, error) {
	if userID <= 0 || count <= 0 {
		return nil, errors.New("invalid blind box open request")
	}
	setting := blindboxsettings.Get()
	if !setting.Enabled {
		return nil, commercedomain.ErrBlindBoxDisabled
	}

	var records []commerceschema.BlindBoxOpenRecord
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		records, err = openBlindBoxesTx(tx, userID, count, nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

// OpenBlindBoxOrderByTradeNo opens all remaining blind boxes for a paid order.
func OpenBlindBoxOrderByTradeNo(tradeNo string) ([]commerceschema.BlindBoxOpenRecord, error) {
	if strings.TrimSpace(tradeNo) == "" {
		return nil, errors.New("tradeNo is empty")
	}
	var records []commerceschema.BlindBoxOpenRecord
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		records, err = OpenBlindBoxOrderByTradeNoTx(tx, tradeNo)
		return err
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

// OpenBlindBoxOrderByTradeNoTx opens all remaining blind boxes for a paid order inside a transaction.
func OpenBlindBoxOrderByTradeNoTx(tx *gorm.DB, tradeNo string) ([]commerceschema.BlindBoxOpenRecord, error) {
	if strings.TrimSpace(tradeNo) == "" {
		return nil, errors.New("tradeNo is empty")
	}
	var order commerceschema.BlindBoxOrder
	if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return nil, commercedomain.ErrBlindBoxOrderNotFound
	}
	if order.Status != constant.TopUpStatusSuccess {
		return nil, commercedomain.ErrBlindBoxOrderStatusInvalid
	}
	remaining := order.Quantity - order.OpenedCount
	if remaining <= 0 {
		return []commerceschema.BlindBoxOpenRecord{}, nil
	}
	return openBlindBoxesTx(tx, order.UserId, remaining, &order.Id)
}

func openBlindBoxesTx(tx *gorm.DB, userID int, count int, orderID *int) ([]commerceschema.BlindBoxOpenRecord, error) {
	now := platformruntime.GetTimestamp()
	dayStart, dayEnd := getBlindBoxDayRange(now)
	setting := blindboxsettings.Get()
	records := make([]commerceschema.BlindBoxOpenRecord, 0, count)

	openCountToday, err := countBlindBoxOpensInRange(tx, dayStart, dayEnd)
	if err != nil {
		return nil, err
	}
	if int(openCountToday)+count > setting.DailyOpenLimit {
		return nil, commercedomain.ErrBlindBoxSiteOpenLimitReached
	}

	orders, available, err := getOpenableBlindBoxOrdersTx(tx, userID, orderID, now)
	if err != nil {
		return nil, err
	}
	if available < count {
		return nil, commercedomain.ErrBlindBoxInsufficientStock
	}

	pityState, err := getOrCreateBlindBoxPityStateTx(tx, userID)
	if err != nil {
		return nil, err
	}
	zeroHourState, err := getOrCreateZeroHourStateTx(tx, userID)
	if err != nil {
		return nil, err
	}
	effectivePityThreshold := setting.PityThreshold
	firstPurchaseStartUSD := setting.FirstPurchaseGuaranteeUSD

	firstPurchaseOrderID := 0
	if firstPurchaseStartUSD > 0 {
		for index := range orders {
			if orders[index].Money <= 0 || orders[index].OpenedCount != 0 {
				continue
			}
			isFirstOrder, err := isFirstSuccessfulBlindBoxOrderTx(tx, userID, orders[index].Id)
			if err != nil {
				return nil, err
			}
			if isFirstOrder {
				firstPurchaseOrderID = orders[index].Id
				break
			}
		}
	}

	orderIndex := 0
	currentOrderRemaining := orders[0].Quantity - orders[0].OpenedCount
	for i := 0; i < count; i++ {
		for currentOrderRemaining <= 0 && orderIndex < len(orders)-1 {
			orderIndex++
			currentOrderRemaining = orders[orderIndex].Quantity - orders[orderIndex].OpenedCount
		}
		if orderIndex >= len(orders) || currentOrderRemaining <= 0 {
			return nil, commercedomain.ErrBlindBoxInsufficientStock
		}

		currentOrder := &orders[orderIndex]
		currentOrder.OpenedCount++
		currentOrderRemaining--
		isFirstPurchaseOpen := currentOrder.Id == firstPurchaseOrderID && currentOrder.OpenedCount == 1

		record := commerceschema.BlindBoxOpenRecord{
			UserId:     userID,
			OrderId:    currentOrder.Id,
			PoolType:   commerceschema.BlindBoxPoolTypeStandard,
			CreateTime: platformruntime.GetTimestamp(),
		}
		pityTriggered := pityState.ConsecutiveLowRewards+1 >= effectivePityThreshold
		zeroHourHit := false
		if !isFirstPurchaseOpen && !pityTriggered {
			zeroHourHit, err = tryZeroHourRewardTx(tx, userID, zeroHourState)
			if err != nil {
				return nil, err
			}
		}
		if zeroHourHit {
			record.RewardType = commerceschema.BlindBoxRewardTypeProp
			record.RewardTitle = "1 小时 0 倍率卡"
			record.RewardTier = "zero_hour_hidden"
			record.RewardWalletType = ""
			if err := createBlindBoxOpenRecordTx(tx, &record); err != nil {
				return nil, err
			}
			prop, err := createBlindBoxPropTx(tx, userID, record.Id, record.RewardTitle)
			if err != nil {
				return nil, err
			}
			record.PropId = prop.Id
			record.PropType = prop.PropType
			record.PropStatus = prop.Status
			if err := resetZeroHourProgressTx(tx, zeroHourState); err != nil {
				return nil, err
			}
			pityState.ConsecutiveLowRewards = 0
			records = append(records, record)
			continue
		}
		if isPaidBlindBoxOrder(currentOrder) {
			if err := addZeroHourProgressTx(tx, zeroHourState, zeroHourProgressPerPaidOpen); err != nil {
				return nil, err
			}
		}

		subscriptionHit := rand.Float64() < setting.SubscriptionPrizeProbability
		if subscriptionHit {
			subscriptionPlan, err := getBlindBoxSubscriptionPlanTx(tx)
			if err != nil {
				return nil, err
			}
			sub, mergeQuota, err := prepareBlindBoxSubscriptionRewardTx(tx, userID, subscriptionPlan)
			if err != nil {
				return nil, err
			}
			record.RewardType = commerceschema.BlindBoxRewardTypeSubscription
			record.RewardTitle = subscriptionPlan.Title
			record.RewardTier = "subscription"
			record.UserSubscriptionId = sub.Id
			record.RewardUSD = 0
			pityState.ConsecutiveLowRewards = 0
			if err := createBlindBoxOpenRecordTx(tx, &record); err != nil {
				return nil, err
			}
			if mergeQuota {
				if err := creditBlindBoxSubscriptionQuotaTx(tx, sub, subscriptionPlan.TotalAmount, record.Id); err != nil {
					return nil, err
				}
			}
			if err := awardMonthlyPassPropTx(tx, userID, subscriptionPlan, fmt.Sprintf("blind-box-subscription:%d", record.Id)); err != nil {
				return nil, err
			}
			if prop, propErr := primaryMonthlyPassPropTx(tx, userID); propErr == nil {
				record.PropId = prop.Id
				record.PropType = prop.PropType
				record.PropStatus = prop.Status
				record.PropExpiresAt = prop.ExpiresAt
				if err := tx.Save(&record).Error; err != nil {
					return nil, err
				}
			}
			records = append(records, record)
			continue
		} else {
			rewardUSD := 0.0
			tierName := "pity"
			var tier blindboxsettings.TierSetting
			rewardType := commerceschema.BlindBoxRewardTypeQuota
			tierWalletType := commerceschema.BlindBoxRewardWalletTypeDefault

			if pityTriggered {
				rewardUSD = setting.PityGuaranteeUSD
			} else {
				if isFirstPurchaseOpen {
					tierName = "first_purchase"
				}
				tier = pickBlindBoxTier(setting.Tiers)
				if tierName != "first_purchase" {
					tierName = tier.Name
				}
				rewardUSD = randomTierRewardUSD(tier)
				rewardType = blindboxsettings.NormalizeRewardType(tier.RewardType)
				tierWalletType = normalizeBlindBoxRewardWalletType(tier.WalletType)
				applyFirstPurchaseMinimumGuarantee(
					isFirstPurchaseOpen,
					firstPurchaseStartUSD,
					firstPurchaseStartUSD/4,
					&rewardUSD,
					&rewardType,
					&tierWalletType,
				)
			}

			totalRewardUSD := rewardUSD

			switch rewardType {
			case commerceschema.BlindBoxRewardTypeProp:
				record.RewardType = commerceschema.BlindBoxRewardTypeProp
				record.RewardTitle = tier.Name
				if record.RewardTitle == "" {
					record.RewardTitle = "实用道具奖励"
				}
				record.RewardTier = tierName
				record.RewardUSD = 0
				record.CreditAmount = 0
				record.RewardWalletType = ""
				record.IsPity = false
			case commerceschema.BlindBoxRewardTypeClaudeQuota:
				creditAmount := quotaUnitsFromBlindBoxUSD(totalRewardUSD)
				if creditAmount <= 0 {
					return nil, fmt.Errorf("invalid blind box reward amount: %.2f", totalRewardUSD)
				}
				record.RewardType = commerceschema.BlindBoxRewardTypeClaudeQuota
				record.RewardWalletType = string(commerceschema.BlindBoxRewardWalletTypeClaude)
				record.RewardUSD = totalRewardUSD
				record.CreditAmount = creditAmount
				record.RewardTier = tierName
				record.IsPity = pityTriggered
				if tierName == "first_purchase" {
					record.RewardTitle = formatFirstPurchaseBlindBoxRewardTitle(totalRewardUSD)
				} else {
					record.RewardTitle = fmt.Sprintf("%.2f Claude 额度奖励", totalRewardUSD)
				}
				if err := createBlindBoxOpenRecordTx(tx, &record); err != nil {
					return nil, err
				}
				if err := applyBlindBoxWalletRewardTx(tx, userID, record.Id, creditAmount, commerceschema.BlindBoxRewardWalletTypeClaude); err != nil {
					return nil, err
				}
				if err := recordBlindBoxRewardLogTx(tx, userID, creditAmount, commerceschema.BlindBoxRewardWalletTypeClaude, &record); err != nil {
					return nil, err
				}
				if isBlindBoxHighValueReward(record.RewardType, rewardUSD, setting.LowRewardThresholdUSD) {
					pityState.ConsecutiveLowRewards = 0
				} else {
					pityState.ConsecutiveLowRewards++
				}
				records = append(records, record)
				continue
			default:
				creditAmount := quotaUnitsFromBlindBoxUSD(totalRewardUSD)
				if creditAmount <= 0 {
					return nil, fmt.Errorf("invalid blind box reward amount: %.2f", totalRewardUSD)
				}
				record.RewardType = commerceschema.BlindBoxRewardTypeQuota
				record.RewardWalletType = string(tierWalletType)
				record.RewardUSD = totalRewardUSD
				record.CreditAmount = creditAmount
				record.RewardTier = tierName
				record.IsPity = pityTriggered
				if tierName == "first_purchase" {
					record.RewardTitle = formatFirstPurchaseBlindBoxRewardTitle(totalRewardUSD)
				} else if tierWalletType == commerceschema.BlindBoxRewardWalletTypeClaude {
					record.RewardTitle = fmt.Sprintf("%.2f Claude 额度奖励", totalRewardUSD)
				} else {
					record.RewardTitle = fmt.Sprintf("%.2f 美元奖励", totalRewardUSD)
				}
				if err := createBlindBoxOpenRecordTx(tx, &record); err != nil {
					return nil, err
				}
				if err := applyBlindBoxWalletRewardTx(tx, userID, record.Id, creditAmount, tierWalletType); err != nil {
					return nil, err
				}
				if err := recordBlindBoxRewardLogTx(tx, userID, creditAmount, tierWalletType, &record); err != nil {
					return nil, err
				}
				if isBlindBoxHighValueReward(record.RewardType, rewardUSD, setting.LowRewardThresholdUSD) {
					pityState.ConsecutiveLowRewards = 0
				} else {
					pityState.ConsecutiveLowRewards++
				}
				records = append(records, record)
				continue
			}

			if err := createBlindBoxOpenRecordTx(tx, &record); err != nil {
				return nil, err
			}
			if record.RewardType == commerceschema.BlindBoxRewardTypeProp {
				prop, err := createBlindBoxPropTx(tx, userID, record.Id, record.RewardTitle)
				if err != nil {
					return nil, err
				}
				record.PropId = prop.Id
				record.PropType = prop.PropType
				record.PropStatus = prop.Status
				record.PropExpiresAt = prop.ExpiresAt
				pityState.ConsecutiveLowRewards++
			}
			records = append(records, record)
			continue
		}

		if err := createBlindBoxOpenRecordTx(tx, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	for i := range orders {
		if err := tx.Save(&orders[i]).Error; err != nil {
			return nil, err
		}
	}
	if err := tx.Save(pityState).Error; err != nil {
		return nil, err
	}
	return records, nil
}

func getBlindBoxSubscriptionPlanTx(tx *gorm.DB) (*commerceschema.SubscriptionPlan, error) {
	setting := blindboxsettings.Get()
	title := strings.TrimSpace(setting.SubscriptionPlanTitle)
	if title == "" {
		return nil, errors.New("blind box subscription plan title is empty")
	}
	var plan commerceschema.SubscriptionPlan
	if err := tx.Where("title = ?", title).First(&plan).Error; err != nil {
		return nil, err
	}
	return &plan, nil
}

func countBlindBoxOpensInRange(tx *gorm.DB, start, end int64) (int64, error) {
	var count int64
	err := tx.Model(&commerceschema.BlindBoxOpenRecord{}).
		Where("create_time >= ? AND create_time < ?", start, end).
		Count(&count).Error
	return count, err
}

func getOrCreateBlindBoxPityStateTx(tx *gorm.DB, userID int) (*commerceschema.BlindBoxPityState, error) {
	var state commerceschema.BlindBoxPityState
	err := tx.Set("gorm:query_option", "FOR UPDATE").Where("user_id = ?", userID).First(&state).Error
	if err == nil {
		return &state, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	state = commerceschema.BlindBoxPityState{UserId: userID, ConsecutiveLowRewards: 0}
	if err := tx.Create(&state).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

func getOpenableBlindBoxOrdersTx(tx *gorm.DB, userID int, orderID *int, now int64) ([]commerceschema.BlindBoxOrder, int, error) {
	query := tx.Set("gorm:query_option", "FOR UPDATE").
		Where("user_id = ? AND status = ? AND opened_count < quantity", userID, constant.TopUpStatusSuccess).
		Where("source <> ? OR expires_at = 0 OR expires_at > ?", commerceschema.BlindBoxOrderSourceSubscriptionBenefit, now)
	if orderID != nil {
		query = query.Where("id = ?", *orderID)
	}
	var orders []commerceschema.BlindBoxOrder
	if err := query.Order("id asc").Find(&orders).Error; err != nil {
		return nil, 0, err
	}
	total := 0
	for _, order := range orders {
		remaining := order.Quantity - order.OpenedCount
		if remaining > 0 {
			total += remaining
		}
	}
	return orders, total, nil
}
