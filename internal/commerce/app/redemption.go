package app

import (
	"errors"
	"fmt"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/i18n"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"

	"gorm.io/gorm"
	"sync"
)

var (
	ErrPaymentComplianceRequired = errors.New(i18n.MsgPaymentComplianceRequired)
	ErrTopUpProcessing           = errors.New(i18n.MsgUserTopUpProcessing)
)

type RedemptionRequest struct {
	Key string `json:"key"`
}

type tryLock struct {
	ch chan struct{}
}

var (
	redemptionLocks     sync.Map
	redemptionCreateMux sync.Mutex
)

func newTryLock() *tryLock {
	return &tryLock{ch: make(chan struct{}, 1)}
}

func (l *tryLock) TryLock() bool {
	select {
	case l.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l *tryLock) Unlock() {
	select {
	case <-l.ch:
	default:
	}
}

func getRedemptionLock(userID int) *tryLock {
	if v, ok := redemptionLocks.Load(userID); ok {
		return v.(*tryLock)
	}
	redemptionCreateMux.Lock()
	defer redemptionCreateMux.Unlock()
	if v, ok := redemptionLocks.Load(userID); ok {
		return v.(*tryLock)
	}
	lock := newTryLock()
	redemptionLocks.Store(userID, lock)
	return lock
}

// RedeemTopUpCode applies a redemption code for the authenticated user.
func RedeemTopUpCode(userID int, key string) (*RedemptionResult, error) {
	if !commercestore.IsPaymentComplianceConfirmed() {
		return nil, ErrPaymentComplianceRequired
	}

	lock := getRedemptionLock(userID)
	if !lock.TryLock() {
		return nil, ErrTopUpProcessing
	}
	defer lock.Unlock()

	return RedeemCode(userID, key)
}

// RedeemCode executes the redemption workflow without transport-level compliance checks.
func RedeemCode(userID int, key string) (*RedemptionResult, error) {
	if key == "" {
		return nil, errors.New("redemption key is empty")
	}
	if userID == 0 {
		return nil, errors.New("invalid user id")
	}

	var redemption commerceschema.Redemption
	result := &RedemptionResult{}

	platformruntime.RandomSleep()
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(redemptionKeyColumn()+" = ?", key).First(&redemption).Error; err != nil {
			return commercedomain.ErrRedemptionInvalid
		}
		if redemption.Status == constant.RedemptionCodeStatusUsed {
			return commercedomain.ErrRedemptionUsed
		}
		if redemption.ExpiredTime != 0 && redemption.ExpiredTime < platformruntime.GetTimestamp() {
			return commercedomain.ErrRedemptionExpired
		}
		if redemption.Status != constant.RedemptionCodeStatusEnabled {
			return commercedomain.ErrRedemptionBusy
		}

		redeemType := commercedomain.NormalizeRedemptionType(redemption.RedeemType)
		result.RedeemType = redeemType

		switch redeemType {
		case commerceschema.RedemptionTypeSubscription:
			if redemption.PlanId <= 0 {
				return errors.New("subscription redemption plan is invalid")
			}
			plan, err := getSubscriptionPlanRecordTx(tx, redemption.PlanId)
			if err != nil {
				return err
			}
			sub, err := CreateUserSubscriptionFromPlanTx(tx, userID, plan, "redemption")
			if err != nil {
				return err
			}
			result.PlanId = plan.Id
			result.PlanTitle = plan.Title
			result.UserSubscriptionId = sub.Id
		case commerceschema.RedemptionTypeBlindBox:
			if redemption.BlindBoxQuantity <= 0 {
				return errors.New("blind box redemption quantity is invalid")
			}
			order, err := createBlindBoxRedemptionOrderTx(tx, userID, redemption.BlindBoxQuantity, redemption.Id)
			if err != nil {
				return err
			}
			result.BlindBoxQuantity = redemption.BlindBoxQuantity
			result.BlindBoxOrderId = order.Id
		default:
			walletType := commercedomain.NormalizeWalletType(redemption.WalletType)
			idempotencyKey := fmt.Sprintf("redemption:%d:%s", redemption.Id, walletType)
			if walletType == commerceschema.WalletTypeClaude {
				if err := billingapp.CreditClaudeWalletQuotaTx(tx, userID, redemption.Quota, idempotencyKey, "redemption_credit"); err != nil {
					return err
				}
			} else if err := billingapp.CreditWalletQuotaTx(tx, userID, redemption.Quota, idempotencyKey, "redemption_credit"); err != nil {
				return err
			}
			result.Quota = redemption.Quota
			result.WalletType = walletType
		}

		redemption.RedeemType = redeemType
		redemption.RedeemedTime = platformruntime.GetTimestamp()
		redemption.Status = constant.RedemptionCodeStatusUsed
		redemption.UsedUserId = userID
		if err := tx.Save(&redemption).Error; err != nil {
			return commercedomain.ErrRedemptionBusy
		}
		return nil
	})
	if err != nil {
		platformobservability.SysError("redemption failed: " + err.Error())
		switch {
		case errors.Is(err, commercedomain.ErrRedemptionInvalid):
			return nil, commercedomain.ErrRedemptionInvalid
		case errors.Is(err, commercedomain.ErrRedemptionUsed):
			return nil, commercedomain.ErrRedemptionUsed
		case errors.Is(err, commercedomain.ErrRedemptionExpired):
			return nil, commercedomain.ErrRedemptionExpired
		default:
			return nil, commercedomain.ErrRedemptionBusy
		}
	}

	switch result.RedeemType {
	case commerceschema.RedemptionTypeSubscription:
		planTitle := result.PlanTitle
		if planTitle == "" {
			planTitle = fmt.Sprintf("#%d", result.PlanId)
		}
		auditapp.RecordLog(userID, auditschema.LogTypeTopup, fmt.Sprintf("Redeemed subscription code for %s, redemption ID %d", planTitle, redemption.Id))
	case commerceschema.RedemptionTypeBlindBox:
		auditapp.RecordLog(userID, auditschema.LogTypeTopup, fmt.Sprintf("Redeemed blind box code for %d blind box(es), redemption ID %d", result.BlindBoxQuantity, redemption.Id))
	default:
		if result.WalletType == commerceschema.WalletTypeClaude {
			auditapp.RecordLog(userID, auditschema.LogTypeTopup, fmt.Sprintf("Redeemed Claude quota code for %s, redemption ID %d", logger.LogQuota(redemption.Quota), redemption.Id))
		} else {
			auditapp.RecordLog(userID, auditschema.LogTypeTopup, fmt.Sprintf("Redeemed quota code for %s, redemption ID %d", logger.LogQuota(redemption.Quota), redemption.Id))
		}
	}

	return result, nil
}

func redemptionKeyColumn() string {
	if platformdb.UsingPostgreSQL {
		return `"key"`
	}
	return "`key`"
}

func createBlindBoxRedemptionOrderTx(tx *gorm.DB, userID int, quantity int, redemptionID int) (*commerceschema.BlindBoxOrder, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	if quantity <= 0 {
		return nil, errors.New("invalid blind box quantity")
	}

	tradeNo := fmt.Sprintf("RDBBUSR%dRID%dNO%s", userID, redemptionID, platformruntime.GetUUID())
	order := &commerceschema.BlindBoxOrder{
		UserId:          userID,
		Quantity:        quantity,
		Money:           0,
		TradeNo:         tradeNo,
		PaymentMethod:   "redemption",
		PaymentProvider: "redemption",
		Source:          commerceschema.BlindBoxOrderSourcePurchase,
		Status:          constant.TopUpStatusSuccess,
		CreateTime:      platformruntime.GetTimestamp(),
		CompleteTime:    platformruntime.GetTimestamp(),
		ProviderPayload: fmt.Sprintf(`{"source":"redemption","redemption_id":%d}`, redemptionID),
	}
	if err := tx.Create(order).Error; err != nil {
		return nil, err
	}
	return order, nil
}
