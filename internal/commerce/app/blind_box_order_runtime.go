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

	"strings"

	// ValidateBlindBoxPurchase validates quantity limits and returns the payable amount.
	"gorm.io/gorm"
)

func ValidateBlindBoxPurchase(userID int, quantity int) (float64, error) {
	setting := blindboxsettings.Get()
	if !setting.Enabled {
		return 0, commercedomain.ErrBlindBoxDisabled
	}
	if userID <= 0 || quantity <= 0 {
		return 0, errors.New("invalid blind box request")
	}

	now := platformruntime.GetTimestamp()
	dayStart, dayEnd := getBlindBoxDayRange(now)
	monthStart, monthEnd := getBlindBoxMonthRange(now)
	todayCount, err := sumBlindBoxOrderQuantity(userID, dayStart, dayEnd)
	if err != nil {
		return 0, err
	}
	var walletPurchaseCount int64
	if err := platformdb.DB.Model(&commerceschema.BalanceBlindBoxPurchase{}).
		Where("user_id = ? AND purchase_date = ? AND total_quota > 0", userID, currentBalanceBlindBoxDate()).
		Select("COALESCE(SUM(quantity), 0)").Scan(&walletPurchaseCount).Error; err != nil && !isBalanceBlindBoxSchemaMissing(err) {
		return 0, err
	}
	todayCount += int(walletPurchaseCount)
	if todayCount+quantity > setting.DailyLimit {
		return 0, fmt.Errorf("daily blind box limit reached: %d", setting.DailyLimit)
	}
	monthCount, err := sumBlindBoxOrderQuantity(userID, monthStart, monthEnd)
	if err != nil {
		return 0, err
	}
	if monthCount+quantity > setting.MonthlyLimit {
		return 0, fmt.Errorf("monthly blind box limit reached: %d", setting.MonthlyLimit)
	}
	return setting.UnitPrice * float64(quantity), nil
}

// CompleteBlindBoxOrder completes a pending payment. Boxes remain unopened so
// the user can choose whether to reveal them one at a time or all together.
func CompleteBlindBoxOrder(tradeNo string, providerPayload string, expectedPaymentProvider string, actualPaymentMethod string) error {
	if strings.TrimSpace(tradeNo) == "" {
		return errors.New("tradeNo is empty")
	}

	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var order commerceschema.BlindBoxOrder
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(blindBoxTradeNoColumn()+" = ?", tradeNo).First(&order).Error; err != nil {
			return commercedomain.ErrBlindBoxOrderNotFound
		}
		if expectedPaymentProvider != "" && order.PaymentProvider != expectedPaymentProvider {
			return commerceschema.ErrPaymentMethodMismatch
		}
		if order.Status == constant.TopUpStatusSuccess {
			return nil
		}
		if order.Status != constant.TopUpStatusPending {
			return commercedomain.ErrBlindBoxOrderStatusInvalid
		}

		order.Status = constant.TopUpStatusSuccess
		order.CompleteTime = platformruntime.GetTimestamp()
		if providerPayload != "" {
			order.ProviderPayload = providerPayload
		}
		if actualPaymentMethod != "" && order.PaymentMethod != actualPaymentMethod {
			order.PaymentMethod = actualPaymentMethod
		}
		if err := awardReferralFirstPurchaseBonusTx(tx, order.UserId, commercedomain.ReferralPurchaseTypeBlindBox, "blind_box_order", order.TradeNo); err != nil {
			return err
		}
		if err := issuePaidBlindBoxOrderInventoryTx(tx, &order); err != nil {
			return err
		}
		order.OpenedCount = order.Quantity
		return tx.Save(&order).Error
	})
}

// ExpireBlindBoxOrder marks a pending blind-box order as expired.
func ExpireBlindBoxOrder(tradeNo string, expectedPaymentProvider string) error {
	if strings.TrimSpace(tradeNo) == "" {
		return errors.New("tradeNo is empty")
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var order commerceschema.BlindBoxOrder
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(blindBoxTradeNoColumn()+" = ?", tradeNo).First(&order).Error; err != nil {
			return commercedomain.ErrBlindBoxOrderNotFound
		}
		if expectedPaymentProvider != "" && order.PaymentProvider != expectedPaymentProvider {
			return commerceschema.ErrPaymentMethodMismatch
		}
		if order.Status != constant.TopUpStatusPending {
			return nil
		}
		order.Status = constant.TopUpStatusExpired
		order.CompleteTime = platformruntime.GetTimestamp()
		return tx.Save(&order).Error
	})
}

func blindBoxTradeNoColumn() string {
	if platformdb.UsingPostgreSQL {
		return `"trade_no"`
	}
	return "`trade_no`"
}
