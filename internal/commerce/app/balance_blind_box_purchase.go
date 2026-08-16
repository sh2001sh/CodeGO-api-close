package app

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/sh2001sh/new-api/constant"
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
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PurchaseBalanceBlindBoxes spends wallet balance and mints sealed inventory.
func PurchaseBalanceBlindBoxes(userID int, requestID string, count int) (*BalanceBlindBoxPurchaseResult, error) {
	requestID = strings.TrimSpace(requestID)
	if userID <= 0 || requestID == "" || len(requestID) > 64 || count <= 0 || count > balanceBlindBoxMaxBatch {
		return nil, errors.New("统一盲盒购买参数无效")
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
			return fmt.Errorf("统一盲盒每日最多购买 %d 个，今日还可购买 %d 个", setting.BalanceBlindBoxDailyPurchaseLimit, maxBalanceBlindBoxCount(0, int64(setting.BalanceBlindBoxDailyPurchaseLimit)-purchasedToday))
		}
		if err := billingapp.DebitClaudeWalletQuotaTxWithReason(tx, userID, int(priceQuota)*count, "unified-blind-box-purchase:"+requestID, "unified_blind_box_purchase"); err != nil {
			if errors.Is(err, billingdomain.ErrInsufficientBalance) {
				return errors.New("统一额度不足，无法购买盲盒")
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
		return true, errors.New("统一盲盒购买请求冲突")
	}
	return true, nil
}

func countBalanceBlindBoxPurchasesTx(tx *gorm.DB, userID int) (int64, error) {
	var purchasedCount int64
	err := tx.Model(&commerceschema.BalanceBlindBoxPurchase{}).
		Where("user_id = ? AND purchase_date = ?", userID, currentBalanceBlindBoxDate()).
		Select("COALESCE(SUM(quantity), 0)").Scan(&purchasedCount).Error
	if err != nil {
		return 0, err
	}
	pendingCount, err := countPendingPaidBlindBoxOrdersTx(tx, userID)
	return purchasedCount + pendingCount, err
}

func countPendingPaidBlindBoxOrdersTx(tx *gorm.DB, userID int) (int64, error) {
	dayStart, dayEnd := getBlindBoxDayRange(platformruntime.GetTimestamp())
	var count int64
	err := tx.Model(&commerceschema.BlindBoxOrder{}).
		Where("user_id = ? AND create_time >= ? AND create_time < ? AND status = ? AND money > 0", userID, dayStart, dayEnd, constant.TopUpStatusPending).
		Select("COALESCE(SUM(quantity), 0)").Scan(&count).Error
	return count, err
}

func issueBalanceBlindBoxBatchTx(tx *gorm.DB, purchase *commerceschema.BalanceBlindBoxPurchase, setting blindboxsettings.Setting) error {
	items := make([]commerceschema.BalanceBlindBoxItem, 0, purchase.Quantity)
	for range purchase.Quantity {
		items = append(items, newUnrevealedBalanceBlindBoxItem(purchase.Id, purchase.UserId, purchase.UserId))
	}
	if err := tx.Create(&items).Error; err != nil {
		return err
	}
	return auditapp.RecordLogTx(tx, purchase.UserId, auditschema.LogTypeManage, fmt.Sprintf("统一盲盒入库 %d 个，批次 %d", purchase.Quantity, purchase.Id))
}

func issuePaidBlindBoxOrderInventoryTx(tx *gorm.DB, order *commerceschema.BlindBoxOrder) error {
	if tx == nil || order == nil || order.Id <= 0 || order.Quantity <= 0 {
		return errors.New("invalid paid blind box order")
	}
	requestID := paidBlindBoxInventoryRequestID(order.TradeNo)
	var existing commerceschema.BalanceBlindBoxPurchase
	err := tx.Where("request_id = ?", requestID).First(&existing).Error
	if err == nil {
		if existing.UserId != order.UserId || existing.Quantity != order.Quantity {
			return errors.New("paid blind box inventory request conflict")
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	unitPrice := blindboxsettings.Get().UnitPrice
	if order.Quantity > 0 && order.Money > 0 {
		unitPrice = order.Money / float64(order.Quantity)
	}
	purchase := &commerceschema.BalanceBlindBoxPurchase{
		UserId: order.UserId, RequestId: requestID, Quantity: order.Quantity,
		UnitPriceUSD: unitPrice, TotalQuota: 0, PurchaseDate: currentBalanceBlindBoxDate(),
	}
	if err := tx.Create(purchase).Error; err != nil {
		return err
	}
	return issueBalanceBlindBoxBatchTx(tx, purchase, blindboxsettings.Get())
}

func paidBlindBoxInventoryRequestID(tradeNo string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(tradeNo)))
	return fmt.Sprintf("cash:%x", digest[:24])
}

func maxBalanceBlindBoxCount(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
