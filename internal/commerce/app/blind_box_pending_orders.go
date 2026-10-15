package app

import (
	"errors"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/constant"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const (
	defaultPendingBlindBoxExpiryMinutes = 3
	minimumPendingBlindBoxExpiryMinutes = 1
	maximumPendingBlindBoxExpiryMinutes = 24 * 60
)

// PendingBlindBoxOrderExpiry returns how long an unpaid order reserves purchase capacity.
func PendingBlindBoxOrderExpiry() time.Duration {
	minutes := platformconfig.GetEnvOrDefaultInt("BLIND_BOX_PENDING_EXPIRY_MINUTES", defaultPendingBlindBoxExpiryMinutes)
	if minutes < minimumPendingBlindBoxExpiryMinutes || minutes > maximumPendingBlindBoxExpiryMinutes {
		minutes = defaultPendingBlindBoxExpiryMinutes
	}
	return time.Duration(minutes) * time.Minute
}

// CancelPendingBlindBoxOrder expires an unpaid order owned by the current user.
func CancelPendingBlindBoxOrder(userID int, tradeNo string) error {
	if userID <= 0 || strings.TrimSpace(tradeNo) == "" {
		return errors.New("invalid blind box order cancellation")
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var order commerceschema.BlindBoxOrder
		if err := tx.Set("gorm:query_option", "FOR UPDATE").
			Where(blindBoxTradeNoColumn()+" = ? AND user_id = ?", tradeNo, userID).
			First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commercedomain.ErrBlindBoxOrderNotFound
			}
			return err
		}
		if order.Status != constant.TopUpStatusPending {
			return nil
		}
		order.Status = constant.TopUpStatusExpired
		order.CompleteTime = platformruntime.GetTimestamp()
		return tx.Save(&order).Error
	})
}

// ExpireDueBlindBoxOrders releases capacity held by stale unpaid orders.
func ExpireDueBlindBoxOrders(limit int) (int, error) {
	if limit <= 0 {
		limit = 300
	}
	now := platformruntime.GetTimestamp()
	cutoff := pendingBlindBoxOrderCutoff(now)
	var ids []int
	if err := platformdb.DB.Model(&commerceschema.BlindBoxOrder{}).
		Where("status = ? AND create_time > 0 AND create_time <= ? AND money > 0", constant.TopUpStatusPending, cutoff).
		Order("create_time asc, id asc").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}

	expired := 0
	for _, id := range ids {
		result := platformdb.DB.Model(&commerceschema.BlindBoxOrder{}).
			Where("id = ? AND status = ? AND create_time <= ?", id, constant.TopUpStatusPending, cutoff).
			Updates(map[string]any{"status": constant.TopUpStatusExpired, "complete_time": now})
		if result.Error != nil {
			return expired, result.Error
		}
		expired += int(result.RowsAffected)
	}
	return expired, nil
}

func pendingBlindBoxOrderCutoff(now int64) int64 {
	return now - int64(PendingBlindBoxOrderExpiry().Seconds())
}
