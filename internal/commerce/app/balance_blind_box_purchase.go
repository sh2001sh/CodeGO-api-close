package app

import (
	"errors"
	"fmt"
	"strings"

	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PurchaseBalanceBlindBoxes spends wallet balance and mints sealed inventory.
func PurchaseBalanceBlindBoxes(userID int, requestID string, count int) (*BalanceBlindBoxPurchaseResult, error) {
	requestID = strings.TrimSpace(requestID)
	if userID <= 0 || requestID == "" || len(requestID) > 64 || count <= 0 || count > balanceBlindBoxMaxBatch {
		return nil, errors.New("余额盲盒购买参数无效")
	}
	setting := blindboxsettings.Get()
	if !setting.Enabled || !setting.BalanceBlindBoxEnabled {
		return nil, commercedomain.ErrBlindBoxDisabled
	}
	priceQuota := quotaUnitsFromBlindBoxUSD(setting.BalanceBlindBoxPriceUSD)
	var purchase commerceschema.BalanceBlindBoxPurchase
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if found, err := loadExistingBalanceBlindBoxPurchase(tx, userID, requestID, count, &purchase); found || err != nil {
			return err
		}
		var user identityschema.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		purchasedToday, err := countBalanceBlindBoxPurchasesTx(tx, userID)
		if err != nil {
			return err
		}
		if purchasedToday+int64(count) > int64(setting.BalanceBlindBoxDailyPurchaseLimit) {
			return fmt.Errorf("余额盲盒每日最多购买 %d 个，今日还可购买 %d 个", setting.BalanceBlindBoxDailyPurchaseLimit, maxBalanceBlindBoxCount(0, int64(setting.BalanceBlindBoxDailyPurchaseLimit)-purchasedToday))
		}
		if err := billingapp.DebitWalletQuotaTxWithReason(tx, userID, int(priceQuota)*count, "balance-blind-box-purchase:"+requestID, "balance_blind_box_purchase"); err != nil {
			if errors.Is(err, billingdomain.ErrInsufficientBalance) {
				return errors.New("钱包余额不足，无法购买余额盲盒")
			}
			return err
		}
		purchase = commerceschema.BalanceBlindBoxPurchase{
			UserId: userID, RequestId: requestID, Quantity: count, UnitPriceUSD: setting.BalanceBlindBoxPriceUSD,
			TotalQuota: priceQuota * int64(count), PurchaseDate: currentBalanceBlindBoxDate(),
		}
		if err := tx.Create(&purchase).Error; err != nil {
			return err
		}
		return issueBalanceBlindBoxBatchTx(tx, &purchase, setting)
	})
	if err != nil {
		return nil, err
	}
	_ = identitystore.InvalidateUserCache(userID)
	overview, err := GetBalanceBlindBoxOverview(userID)
	if err != nil {
		return nil, err
	}
	return &BalanceBlindBoxPurchaseResult{Purchase: purchase, Overview: *overview}, nil
}

func loadExistingBalanceBlindBoxPurchase(tx *gorm.DB, userID int, requestID string, count int, purchase *commerceschema.BalanceBlindBoxPurchase) (bool, error) {
	err := tx.Where("request_id = ?", requestID).First(purchase).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if purchase.UserId != userID || purchase.Quantity != count {
		return true, errors.New("余额盲盒购买请求冲突")
	}
	return true, nil
}

func countBalanceBlindBoxPurchasesTx(tx *gorm.DB, userID int) (int64, error) {
	var count int64
	err := tx.Model(&commerceschema.BalanceBlindBoxPurchase{}).
		Where("user_id = ? AND purchase_date = ?", userID, currentBalanceBlindBoxDate()).
		Select("COALESCE(SUM(quantity), 0)").Scan(&count).Error
	return count, err
}

func issueBalanceBlindBoxBatchTx(tx *gorm.DB, purchase *commerceschema.BalanceBlindBoxPurchase, setting blindboxsettings.Setting) error {
	var pity commerceschema.BalanceBlindBoxPityState
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", purchase.UserId).First(&pity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		pity = commerceschema.BalanceBlindBoxPityState{UserId: purchase.UserId}
		if err := tx.Create(&pity).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	var issuedCount int64
	if err := tx.Model(&commerceschema.BalanceBlindBoxItem{}).Where("purchase_user_id = ?", purchase.UserId).Count(&issuedCount).Error; err != nil {
		return err
	}
	items := make([]commerceschema.BalanceBlindBoxItem, 0, purchase.Quantity)
	for index := 0; index < purchase.Quantity; index++ {
		items = append(items, issueSealedBalanceBlindBox(purchase.Id, purchase.UserId, setting, &pity, issuedCount == 0 && index == 0))
	}
	if err := tx.Create(&items).Error; err != nil {
		return err
	}
	if err := tx.Save(&pity).Error; err != nil {
		return err
	}
	return auditapp.RecordLogTx(tx, purchase.UserId, auditschema.LogTypeManage, fmt.Sprintf("购买余额盲盒 %d 个，批次 %d", purchase.Quantity, purchase.Id))
}

func maxBalanceBlindBoxCount(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
