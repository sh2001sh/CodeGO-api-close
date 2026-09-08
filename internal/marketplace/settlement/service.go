package settlement

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	statusPending   = "pending"
	statusReleased  = "released"
	statusReclaimed = "reclaimed"
	statusForfeited = "forfeited"
)

type RecordParams struct {
	RequestID              string
	GroupID                string
	OwnerUserID            int
	ConsumerUserID         int
	BillingSource          string
	ConsumerDebitAmount    int64
	SettlementGrossAmount  int64
	WalletMultiplier       float64
	SubscriptionMultiplier float64
}

type ReleaseHook func(tx *gorm.DB, userID int, amount int, idempotencyKey string, reasonCode string) error
type ReclaimHook func(tx *gorm.DB, ownerUserID int, adminUserID int, amount int, idempotencyKey string) error
type ForfeitHook func(tx *gorm.DB, pendingAccountID string, adminUserID int, amount int, idempotencyKey string) error

type ReleaseFilter struct {
	OwnerUserIDs   []int
	StartTimestamp int64
	EndTimestamp   int64
	Limit          int
	MaxAmount      int64 // Exact amount to reclaim; 0 means all matching released earnings.
	OperationID    string
}

type ReleaseResult struct {
	Count  int
	Amount int64
}

type ReclaimResult struct {
	Count  int
	Amount int64
}

var (
	releaseHook ReleaseHook
	reclaimHook ReclaimHook
	forfeitHook ForfeitHook
	workerOnce  sync.Once
)

func RegisterReleaseHook(hook ReleaseHook) { releaseHook = hook }
func RegisterReclaimHook(hook ReclaimHook) { reclaimHook = hook }
func RegisterForfeitHook(hook ForfeitHook) { forfeitHook = hook }

func Record(params RecordParams) error {
	if params.RequestID == "" || params.GroupID == "" || params.OwnerUserID <= 0 || params.SettlementGrossAmount <= 0 {
		return nil
	}
	commission := percentage(params.SettlementGrossAmount, 5)
	fee := int64(0)
	ownerNet := params.SettlementGrossAmount - commission
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
			AccountType: "marketplace_owner_pending", OwnerType: "user", OwnerID: int64(params.OwnerUserID), QuotaUnit: "quota",
		})
		if err != nil {
			return err
		}
		platformAccount, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
			AccountType: "marketplace_platform_revenue", OwnerType: "system", OwnerID: 1, QuotaUnit: "quota",
		})
		if err != nil {
			return err
		}
		settlement := marketplaceschema.Settlement{
			RequestID: params.RequestID, GroupID: params.GroupID, OwnerUserID: params.OwnerUserID,
			ConsumerUserID: params.ConsumerUserID, BillingSource: params.BillingSource,
			ConsumerAmount: params.ConsumerDebitAmount, SettlementGrossAmount: params.SettlementGrossAmount,
			PlatformCommission: commission, TransactionFee: fee, OwnerNetAmount: ownerNet,
			Multiplier: params.WalletMultiplier, SubscriptionMultiplier: params.SubscriptionMultiplier,
			Status: statusPending, PendingAccountID: account.AccountID,
			AvailableAt: time.Now().UTC().Add(24 * time.Hour),
		}
		result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "request_id"}}, DoNothing: true}).Create(&settlement)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		_, err = billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
			AccountID: account.AccountID, Amount: ownerNet, IdempotencyKey: "marketplace-pending:" + params.RequestID,
			ReasonCode: "marketplace_owner_pending", ReferenceType: "marketplace_settlement", ReferenceID: settlement.ID,
			OperatorType: "system", OperatorID: "marketplace",
		})
		if err != nil {
			return err
		}
		if commission <= 0 {
			return nil
		}
		_, err = billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
			AccountID: platformAccount.AccountID, Amount: commission,
			IdempotencyKey: "marketplace-platform:" + params.RequestID,
			ReasonCode:     "marketplace_platform_revenue", ReferenceType: "marketplace_settlement", ReferenceID: settlement.ID,
			OperatorType: "system", OperatorID: "marketplace",
		})
		return err
	})
}

func StartReleaseWorker(ctx context.Context) {
	workerOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				if err := ReleaseDue(200); err != nil {
					platformobservability.SysError("release marketplace settlement: " + err.Error())
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}

func ReleaseDue(limit int) error {
	if releaseHook == nil {
		return errors.New("marketplace settlement release hook is not registered")
	}
	if limit <= 0 {
		limit = 100
	}
	var settlements []marketplaceschema.Settlement
	if err := platformdb.DB.Where("status = ? AND available_at <= ?", statusPending, time.Now().UTC()).Order("available_at asc").Limit(limit).Find(&settlements).Error; err != nil {
		return err
	}
	for index := range settlements {
		if err := releaseOne(settlements[index].ID); err != nil {
			return err
		}
	}
	return nil
}

// ReleasePending releases pending owner earnings selected by an administrator.
// The normal worker only releases records after AvailableAt; this explicit path
// intentionally allows a reviewed time range or owner selection to be released
// immediately while keeping the same idempotent ledger transaction.
func ReleasePending(filter ReleaseFilter) (ReleaseResult, error) {
	if releaseHook == nil {
		return ReleaseResult{}, errors.New("marketplace settlement release hook is not registered")
	}
	if filter.Limit <= 0 {
		filter.Limit = 5000
	}
	query := platformdb.DB.Where("status = ?", statusPending).Order("created_at asc").Limit(filter.Limit)
	if len(filter.OwnerUserIDs) > 0 {
		query = query.Where("owner_user_id IN ?", filter.OwnerUserIDs)
	}
	if filter.StartTimestamp > 0 {
		query = query.Where("created_at >= ?", time.Unix(filter.StartTimestamp, 0))
	}
	if filter.EndTimestamp > 0 {
		query = query.Where("created_at < ?", time.Unix(filter.EndTimestamp+1, 0))
	}
	var settlements []marketplaceschema.Settlement
	if err := query.Find(&settlements).Error; err != nil {
		return ReleaseResult{}, err
	}
	result := ReleaseResult{}
	for index := range settlements {
		if err := releaseOne(settlements[index].ID); err != nil {
			return result, err
		}
		result.Count++
		result.Amount += settlements[index].OwnerNetAmount
	}
	return result, nil
}

// ReclaimPending transfers released earnings atomically, including partial records.
// OperationID must be reused when retrying the same administrator action.
func ReclaimPending(filter ReleaseFilter) (ReclaimResult, error) {
	if filter.MaxAmount < 0 || len(filter.OperationID) > 64 {
		return ReclaimResult{}, errors.New("invalid income reclaim amount or operation ID")
	}
	if reclaimHook == nil {
		return ReclaimResult{}, errors.New("marketplace settlement reclaim hook is not registered")
	}
	// Legacy callers can still reclaim all without an operation ID. New clients
	// supply one to make retries safe after a lost response, including partials.
	operationID := filter.OperationID
	if operationID == "" {
		operationID = platformruntime.GetUUID()
	}
	filter.OwnerUserIDs = slices.Clone(filter.OwnerUserIDs)
	slices.Sort(filter.OwnerUserIDs)
	filter.OwnerUserIDs = slices.Compact(filter.OwnerUserIDs)
	filter.OperationID = ""
	filter.Limit = 0
	payload, err := json.Marshal(filter)
	if err != nil {
		return ReclaimResult{}, err
	}
	operation := marketplaceschema.IncomeReclaim{ID: operationID, Fingerprint: fmt.Sprintf("%x", sha256.Sum256(payload))}
	result := ReclaimResult{}
	err = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		inserted := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&operation)
		if inserted.Error != nil {
			return inserted.Error
		}
		if inserted.RowsAffected == 0 {
			var existing marketplaceschema.IncomeReclaim
			if err := tx.First(&existing, "id = ?", operationID).Error; err != nil {
				return err
			}
			if existing.Fingerprint != operation.Fingerprint {
				return errors.New("回收操作标识已用于其他筛选条件或金额")
			}
			result = ReclaimResult{Count: existing.Count, Amount: existing.Amount}
			return nil
		}
		query := tx.Model(&marketplaceschema.Settlement{}).
			Where("status = ? AND owner_net_amount > reclaimed_amount", statusReleased)
		if len(filter.OwnerUserIDs) > 0 {
			query = query.Where("owner_user_id IN ?", filter.OwnerUserIDs)
		}
		if filter.StartTimestamp > 0 {
			query = query.Where("created_at >= ?", time.Unix(filter.StartTimestamp, 0))
		}
		if filter.EndTimestamp > 0 {
			query = query.Where("created_at < ?", time.Unix(filter.EndTimestamp+1, 0))
		}
		ownerAmounts := make(map[int]int64)
		var cursor *marketplaceschema.Settlement
		for {
			batchQuery := query.Clauses(clause.Locking{Strength: "UPDATE"}).Order("created_at ASC, id ASC").Limit(500)
			if cursor != nil {
				batchQuery = batchQuery.Where("created_at > ? OR (created_at = ? AND id > ?)", cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
			}
			var items []marketplaceschema.Settlement
			if err := batchQuery.Find(&items).Error; err != nil {
				return err
			}
			if len(items) == 0 {
				break
			}
			fullIDs := make([]string, 0, len(items))
			for _, item := range items {
				amount := item.OwnerNetAmount - item.ReclaimedAmount
				if filter.MaxAmount > 0 {
					amount = min(amount, filter.MaxAmount-result.Amount)
				}
				if amount <= 0 {
					continue
				}
				ownerAmounts[item.OwnerUserID] += amount
				updates := map[string]any{"reclaimed_amount": item.ReclaimedAmount + amount, "reclaimed_at": time.Now().UTC()}
				if item.ReclaimedAmount+amount == item.OwnerNetAmount {
					fullIDs = append(fullIDs, item.ID)
				} else if err := tx.Model(&item).Updates(updates).Error; err != nil {
					return err
				}
				result.Count++
				result.Amount += amount
				if filter.MaxAmount > 0 && result.Amount == filter.MaxAmount {
					break
				}
			}
			if len(fullIDs) > 0 {
				if err := tx.Model(&marketplaceschema.Settlement{}).Where("id IN ?", fullIDs).Updates(map[string]any{
					"reclaimed_amount": gorm.Expr("owner_net_amount"), "reclaimed_at": time.Now().UTC(), "status": statusReclaimed,
				}).Error; err != nil {
					return err
				}
			}
			if filter.MaxAmount > 0 && result.Amount == filter.MaxAmount {
				break
			}
			cursor = &items[len(items)-1]
		}
		if filter.MaxAmount > 0 && result.Amount < filter.MaxAmount {
			return errors.New("所选范围的可回收收益不足，未扣除额度，请刷新后重试")
		}
		owners := make([]int, 0, len(ownerAmounts))
		for owner := range ownerAmounts {
			owners = append(owners, owner)
		}
		slices.Sort(owners)
		for _, owner := range owners {
			if err := reclaimHook(tx, owner, 1, int(ownerAmounts[owner]), fmt.Sprintf("marketplace-reclaim:%s:owner:%d", operationID, owner)); err != nil {
				return err
			}
		}
		return tx.Model(&operation).Updates(map[string]any{"count": result.Count, "amount": result.Amount}).Error
	})
	if err != nil {
		return ReclaimResult{}, err
	}
	return result, nil
}

// ForfeitChannelPending clears frozen pending earnings when a channel is shut down.
func ForfeitChannelPending(channelID string) (ReclaimResult, error) {
	var groupID string
	if err := platformdb.DB.Model(&marketplaceschema.Group{}).Where("channel_id = ?", channelID).Pluck("id", &groupID).Error; err != nil {
		return ReclaimResult{}, err
	}
	var settlements []marketplaceschema.Settlement
	if err := platformdb.DB.Where("group_id = ? AND status = ?", groupID, statusPending).Find(&settlements).Error; err != nil {
		if isMissingSettlementTable(err) {
			return ReclaimResult{}, nil
		}
		return ReclaimResult{}, err
	}
	if len(settlements) == 0 {
		return ReclaimResult{}, nil
	}
	if forfeitHook == nil {
		return ReclaimResult{}, errors.New("marketplace settlement forfeit hook is not registered")
	}
	result := ReclaimResult{}
	for _, item := range settlements {
		if err := forfeitOne(item.ID); err != nil {
			return result, err
		}
		result.Count++
		result.Amount += item.OwnerNetAmount
	}
	return result, nil
}

func ForfeitChannelPendingTx(tx *gorm.DB, channelID string) (ReclaimResult, error) {
	var groupID string
	if err := tx.Model(&marketplaceschema.Group{}).Where("channel_id = ?", channelID).Pluck("id", &groupID).Error; err != nil {
		return ReclaimResult{}, err
	}
	var settlements []marketplaceschema.Settlement
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("group_id = ? AND status = ?", groupID, statusPending).Find(&settlements).Error; err != nil {
		if isMissingSettlementTable(err) {
			return ReclaimResult{}, nil
		}
		return ReclaimResult{}, err
	}
	if len(settlements) == 0 {
		return ReclaimResult{}, nil
	}
	if forfeitHook == nil {
		return ReclaimResult{}, errors.New("marketplace settlement forfeit hook is not registered")
	}
	result := ReclaimResult{}
	for _, item := range settlements {
		if err := forfeitOneTx(tx, &item); err != nil {
			return result, err
		}
		result.Count++
		result.Amount += item.OwnerNetAmount
	}
	return result, nil
}

func isMissingSettlementTable(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such table") || strings.Contains(message, "does not exist")
}

func forfeitOne(settlementID string) error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var item marketplaceschema.Settlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", settlementID).Error; err != nil {
			return err
		}
		if item.Status != statusPending {
			return nil
		}
		if err := forfeitHook(tx, item.PendingAccountID, 1, int(item.OwnerNetAmount), "marketplace-forfeit:"+item.ID); err != nil {
			return err
		}
		now := time.Now().UTC()
		return tx.Model(&item).Updates(map[string]any{"status": statusForfeited, "forfeited_at": now}).Error
	})
}

func forfeitOneTx(tx *gorm.DB, item *marketplaceschema.Settlement) error {
	if item.Status != statusPending {
		return nil
	}
	if err := forfeitHook(tx, item.PendingAccountID, 1, int(item.OwnerNetAmount), "marketplace-forfeit:"+item.ID); err != nil {
		return err
	}
	now := time.Now().UTC()
	return tx.Model(item).Updates(map[string]any{"status": statusForfeited, "forfeited_at": now}).Error
}

func releaseOne(settlementID string) error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var item marketplaceschema.Settlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", settlementID).Error; err != nil {
			return err
		}
		if item.Status == statusReleased {
			return nil
		}
		reservation, err := billingdomain.CreateReservationTx(tx, billingdomain.CreateReservationParams{
			AccountID: item.PendingAccountID, RequestID: "marketplace-release:" + item.ID,
			ReservedAmount: item.OwnerNetAmount, IdempotencyKey: "marketplace-release-reserve:" + item.ID,
		})
		if err != nil {
			return err
		}
		if _, err := billingdomain.SettleReservationTx(tx, billingdomain.SettleReservationParams{
			ReservationID: reservation.ReservationID, ActualAmount: item.OwnerNetAmount,
			IdempotencyKey: "marketplace-release-settle:" + item.ID,
		}); err != nil {
			return err
		}
		if err := releaseHook(tx, item.OwnerUserID, int(item.OwnerNetAmount), "marketplace-release-credit:"+item.ID, "marketplace_owner_release"); err != nil {
			return err
		}
		now := time.Now().UTC()
		return tx.Model(&item).Updates(map[string]any{"status": statusReleased, "released_at": now}).Error
	})
}

func percentage(amount int64, percent int64) int64 {
	return (amount*percent + 50) / 100
}
