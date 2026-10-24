package app

import (
	"errors"
	"github.com/sh2001sh/new-api/constant"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/shopspring/decimal"
	"math"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

const topUpQueryWindowSeconds int64 = 30 * 24 * 60 * 60

const searchTopUpCountHardLimit = 10000

const (
	defaultPendingTopUpExpiryMinutes = 3
	minimumPendingTopUpExpiryMinutes = 1
	maximumPendingTopUpExpiryMinutes = 24 * 60
)

// CreatePendingTopUpOrderWithBlindBoxDiscount creates a pending top-up order
// from its pre-discount price and applies at most one reserved card in the same
// transaction. Checkout creators must not apply the card before calling it.
func CreatePendingTopUpOrderWithBlindBoxDiscount(topUp *commerceschema.TopUp) (float64, error) {
	if topUp == nil {
		return 0, errors.New("topup is nil")
	}
	if topUp.UserId <= 0 || strings.TrimSpace(topUp.TradeNo) == "" {
		return 0, errors.New("invalid topup order")
	}
	if topUp.Amount <= 0 || topUp.Money <= 0 || math.IsNaN(topUp.Money) || math.IsInf(topUp.Money, 0) {
		return 0, errors.New("invalid topup amount")
	}
	baseMoney := decimal.NewFromFloat(topUp.Money)
	grantedQuota := int64(calculateTopUpQuotaToAdd(topUp))
	basePaidQuota := grantedQuota
	if topUp.PaymentProvider == commerceschema.PaymentProviderCreem {
		// Creem prices can use another currency or include a product bonus.
		// Only its checkout creator can supply a verified paid quota basis.
		basePaidQuota = topUp.PaidQuota
	}
	if basePaidQuota < 0 || grantedQuota <= 0 {
		return 0, errors.New("invalid paid topup quota")
	}

	appliedRate := 0.0
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		prop, err := ReserveBlindBoxTopupDiscountPropTx(tx, topUp.UserId, topUp.TradeNo)
		if err != nil {
			return err
		}
		if prop != nil {
			appliedRate = prop.DiscountRate
			effectiveRate := commercedomain.CappedDiscountRate(float64(topUp.Amount), prop.DiscountRate, blindBoxCheckoutMaximumDiscountCredits)
			topUp.Money = commercedomain.ApplyDiscountRateToMoney(topUp.Money, effectiveRate)
		}
		// Freeze the same-currency checkout discount before mutable provider
		// prices or card settings can change. Zero means no verifiable basis.
		discountedPaidQuota := decimal.NewFromInt(basePaidQuota).
			Mul(decimal.NewFromFloat(topUp.Money)).Div(baseMoney)
		topUp.PaidQuota = decimal.Min(discountedPaidQuota, decimal.NewFromInt(grantedQuota)).IntPart()
		if topUp.PaymentProvider == commerceschema.PaymentProviderEpay {
			propID := 0
			if prop != nil {
				propID = prop.Id
			}
			if err := snapshotEpayTopupCheckout(topUp, propID); err != nil {
				return err
			}
		}
		if topUp.PaymentProvider == commerceschema.PaymentProviderStripe {
			propID := 0
			if prop != nil {
				propID = prop.Id
			}
			if err := snapshotStripeCheckout(&topUp.ProviderPayload, stripeOrderCurrency(topUp.ProviderPayload), topUp.Money, propID); err != nil {
				return err
			}
		}
		return tx.Create(topUp).Error
	})
	return appliedRate, err
}

// GetTopUpByTradeNo loads a top-up order by trade number.
func GetTopUpByTradeNo(tradeNo string) *commerceschema.TopUp {
	if strings.TrimSpace(tradeNo) == "" {
		return nil
	}

	topUp := &commerceschema.TopUp{}
	if err := platformdb.DB.Where("trade_no = ?", tradeNo).First(topUp).Error; err != nil {
		return nil
	}
	return topUp
}

// UpdatePendingTopUpStatus updates a pending top-up order to the target status.
func UpdatePendingTopUpStatus(tradeNo string, expectedPaymentProvider string, targetStatus string) error {
	if strings.TrimSpace(tradeNo) == "" {
		return errors.New("未提供支付单号")
	}

	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		topUp := &commerceschema.TopUp{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(topUpTradeNoColumn()+" = ?", tradeNo).First(topUp).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commerceschema.ErrTopUpNotFound
			}
			return err
		}
		if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
			return commerceschema.ErrPaymentMethodMismatch
		}
		if topUp.Status != constant.TopUpStatusPending {
			return commerceschema.ErrTopUpStatusInvalid
		}

		topUp.Status = targetStatus
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}
		if targetStatus != constant.TopUpStatusSuccess {
			return ReleaseReservedBlindBoxPropByTradeNoTx(tx, tradeNo, commerceschema.BlindBoxPropOrderTypeTopup)
		}
		return nil
	})
}

// PendingTopUpExpiry returns the maximum time an unpaid top-up can reserve a prop.
func PendingTopUpExpiry() time.Duration {
	minutes := platformconfig.GetEnvOrDefaultInt("TOPUP_PENDING_EXPIRY_MINUTES", defaultPendingTopUpExpiryMinutes)
	if minutes < minimumPendingTopUpExpiryMinutes || minutes > maximumPendingTopUpExpiryMinutes {
		minutes = defaultPendingTopUpExpiryMinutes
	}
	return time.Duration(minutes) * time.Minute
}

// ExpireDueTopUps cancels stale pending top-ups and releases their reserved props.
func ExpireDueTopUps(limit int) (int, error) {
	if limit <= 0 {
		limit = 300
	}

	now := platformruntime.GetTimestamp()
	cutoff := now - int64(PendingTopUpExpiry().Seconds())
	var ids []int
	if err := platformdb.DB.Model(&commerceschema.TopUp{}).
		Where("status = ? AND create_time > 0 AND create_time <= ?", constant.TopUpStatusPending, cutoff).
		Where("payment_provider <> ?", commerceschema.PaymentProviderStripe).
		Order("create_time asc, id asc").
		Limit(limit).
		Pluck("id", &ids).Error; err != nil {
		return 0, err
	}

	expired := 0
	for _, id := range ids {
		err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
			topUp := &commerceschema.TopUp{}
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ? AND status = ? AND create_time <= ?", id, constant.TopUpStatusPending, cutoff).
				First(topUp).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}

			topUp.Status = constant.TopUpStatusExpired
			topUp.CompleteTime = now
			if err := tx.Save(topUp).Error; err != nil {
				return err
			}
			if err := ReleaseReservedBlindBoxPropByTradeNoTx(tx, topUp.TradeNo, commerceschema.BlindBoxPropOrderTypeTopup); err != nil {
				return err
			}
			expired++
			return nil
		})
		if err != nil {
			return expired, err
		}
	}
	return expired, nil
}

// GetUserTopUps loads recent top-up records for a specific user.
func GetUserTopUps(userID int, pageInfo *platformpagination.PageInfo) (topUps []*commerceschema.TopUp, total int64, err error) {
	tx := platformdb.DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if recover() != nil {
			tx.Rollback()
		}
	}()

	cutoff := topUpQueryCutoff()
	if err = tx.Model(&commerceschema.TopUp{}).Where("user_id = ? AND create_time >= ?", userID, cutoff).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}
	if err = tx.Where("user_id = ? AND create_time >= ?", userID, cutoff).Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topUps).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topUps, total, nil
}

// GetAllTopUps loads top-up records for administrators without a time-window restriction.
func GetAllTopUps(pageInfo *platformpagination.PageInfo) (topUps []*commerceschema.TopUp, total int64, err error) {
	tx := platformdb.DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if recover() != nil {
			tx.Rollback()
		}
	}()

	if err = tx.Model(&commerceschema.TopUp{}).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}
	if err = tx.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topUps).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topUps, total, nil
}

// SearchUserTopUps searches a user's recent top-up records by trade number.
func SearchUserTopUps(userID int, keyword string, pageInfo *platformpagination.PageInfo) (topUps []*commerceschema.TopUp, total int64, err error) {
	tx := platformdb.DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if recover() != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&commerceschema.TopUp{}).Where("user_id = ? AND create_time >= ?", userID, topUpQueryCutoff())
	if keyword != "" {
		pattern, perr := sanitizeTopUpLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		platformobservability.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}
	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topUps).Error; err != nil {
		tx.Rollback()
		platformobservability.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topUps, total, nil
}

// SearchAllTopUps searches all top-up records by trade number.
func SearchAllTopUps(keyword string, pageInfo *platformpagination.PageInfo) (topUps []*commerceschema.TopUp, total int64, err error) {
	tx := platformdb.DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if recover() != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&commerceschema.TopUp{})
	if keyword != "" {
		pattern, perr := sanitizeTopUpLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		platformobservability.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}
	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topUps).Error; err != nil {
		tx.Rollback()
		platformobservability.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topUps, total, nil
}

func topUpQueryCutoff() int64 {
	return platformruntime.GetTimestamp() - topUpQueryWindowSeconds
}

func topUpTradeNoColumn() string {
	if platformdb.UsingPostgreSQL {
		return `"trade_no"`
	}
	return "`trade_no`"
}

func sanitizeTopUpLikePattern(input string) (string, error) {
	input = strings.ReplaceAll(input, "!", "!!")
	input = strings.ReplaceAll(input, `_`, `!_`)

	if strings.Contains(input, "%%") {
		return "", errors.New("搜索模式中不允许包含连续的 % 通配符")
	}

	count := strings.Count(input, "%")
	if count > 2 {
		return "", errors.New("搜索模式中最多允许包含 2 个 % 通配符")
	}
	if count > 0 {
		stripped := strings.ReplaceAll(input, "%", "")
		if len(stripped) < 2 {
			return "", errors.New("使用模糊搜索时，关键词长度至少为 2 个字符")
		}
	}
	return input, nil
}
