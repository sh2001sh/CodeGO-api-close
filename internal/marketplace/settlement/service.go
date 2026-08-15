package settlement

import (
	"context"
	"errors"
	"sync"
	"time"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	statusPending  = "pending"
	statusReleased = "released"
)

type RecordParams struct {
	RequestID      string
	GroupID        string
	OwnerUserID    int
	ConsumerUserID int
	Amount         int64
	Multiplier     float64
}

type ReleaseHook func(tx *gorm.DB, userID int, amount int, idempotencyKey string, reasonCode string) error

var (
	releaseHook ReleaseHook
	workerOnce  sync.Once
)

func RegisterReleaseHook(hook ReleaseHook) { releaseHook = hook }

func Record(params RecordParams) error {
	if params.RequestID == "" || params.GroupID == "" || params.OwnerUserID <= 0 || params.Amount <= 0 {
		return nil
	}
	commission := percentage(params.Amount, 5)
	fee := int64(0)
	ownerNet := params.Amount - commission
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
			ConsumerUserID: params.ConsumerUserID, ConsumerAmount: params.Amount,
			PlatformCommission: commission, TransactionFee: fee, OwnerNetAmount: ownerNet,
			Multiplier: params.Multiplier, Status: statusPending, PendingAccountID: account.AccountID,
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
