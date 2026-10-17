package app

import (
	"errors"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

type SubscriptionFundingPreConsumeResult struct {
	CreditPackGrantedQuota    int64
	CreditPackPaidQuota       int64
	CreditPackChannelShareBPS int64
	UserSubscriptionID        int
	PreConsumed               int64
	AmountTotal               int64
	AmountUsedAfter           int64
	ReservationID             string
	AccountID                 string
	PlanID                    int
	PlanTitle                 string
}

type SubscriptionFundingPlanInfo struct {
	PlanID    int
	PlanTitle string
}

// MonthlyPassEntitlement identifies the exact package multiplier card bound
// to one billing session.
type MonthlyPassEntitlement struct {
	PropID     int
	Multiplier float64
	ExpiresAt  int64
}

type BlindBoxConsumptionDiscountRequest struct {
	RequestID                 string
	UserID                    int
	ChannelID                 int
	ChannelScope              string
	MultiplierCardSupported   bool
	MultiplierCardUserEnabled bool
	ModelName                 string
	UsingGroup                string
	Quota                     int
}

type BlindBoxConsumptionDiscountResult struct {
	PropID                 int
	Title                  string
	QuotaBeforeDiscount    int
	QuotaAfterDiscount     int
	DiscountQuota          int
	DiscountRate           float64
	Multiplier             float64
	NominalMultiplier      float64
	EffectiveMultiplier    float64
	RemainingDiscountQuota int64
}

type SubscriptionFundingHooks struct {
	PreConsumeCreditPack             func(requestID string, userID int, modelName string, amount int64) (*SubscriptionFundingPreConsumeResult, error)
	PreConsume                       func(requestID string, userID int, modelName string, amount int64) (*SubscriptionFundingPreConsumeResult, error)
	ReserveAdditional                func(requestID string, subscriptionID int, modelName string, amount int64) error
	ReserveUnlimitedTarget           func(requestID string, subscriptionID int, modelName string, target int64) error
	GetPlanInfo                      func(userSubscriptionID int) (*SubscriptionFundingPlanInfo, error)
	PostConsumeDelta                 func(subscriptionID int, modelName string, delta int64) error
	AdjustUsageDelta                 func(subscriptionID int, modelName string, delta int64, operationKey string) (bool, error)
	RefundPreConsume                 func(requestID string) error
	SettleReservation                func(requestID string, subscriptionID int, modelName string, actualAmount int64) error
	SettleReservationTx              func(tx *gorm.DB, requestID string, subscriptionID int, modelName string, actualAmount int64) error
	GetMonthlyPassEntitlement        func(userID int) (*MonthlyPassEntitlement, error)
	ValidateMonthlyPassEntitlement   func(userID int, entitlement MonthlyPassEntitlement) (bool, error)
	ApplyBlindBoxConsumptionDiscount func(request BlindBoxConsumptionDiscountRequest) (BlindBoxConsumptionDiscountResult, error)
}

var subscriptionFundingHooks SubscriptionFundingHooks

func RegisterSubscriptionFundingHooks(hooks SubscriptionFundingHooks) {
	subscriptionFundingHooks = hooks
}

func postSubscriptionUsageDelta(subscriptionID int, modelName string, delta int64) error {
	if delta == 0 {
		return nil
	}
	if subscriptionFundingHooks.PostConsumeDelta == nil {
		return errors.New("subscription funding settle hook is not registered")
	}
	return subscriptionFundingHooks.PostConsumeDelta(subscriptionID, modelName, delta)
}

func adjustSubscriptionUsageDelta(subscriptionID int, modelName string, delta int64, operationKey string) error {
	if subscriptionFundingHooks.AdjustUsageDelta != nil {
		_, err := subscriptionFundingHooks.AdjustUsageDelta(subscriptionID, modelName, delta, operationKey)
		return err
	}
	return postSubscriptionUsageDelta(subscriptionID, modelName, delta)
}

func applyBlindBoxConsumptionDiscount(request BlindBoxConsumptionDiscountRequest) (BlindBoxConsumptionDiscountResult, error) {
	if request.UserID <= 0 || request.Quota <= 0 || subscriptionFundingHooks.ApplyBlindBoxConsumptionDiscount == nil {
		return BlindBoxConsumptionDiscountResult{
			QuotaBeforeDiscount: request.Quota, QuotaAfterDiscount: request.Quota,
			Multiplier: 1, NominalMultiplier: 1, EffectiveMultiplier: 1,
		}, nil
	}
	return subscriptionFundingHooks.ApplyBlindBoxConsumptionDiscount(request)
}

func getMonthlyPassEntitlement(userID int) (*MonthlyPassEntitlement, error) {
	if userID <= 0 || subscriptionFundingHooks.GetMonthlyPassEntitlement == nil {
		return nil, nil
	}
	entitlement, err := subscriptionFundingHooks.GetMonthlyPassEntitlement(userID)
	if err != nil || entitlement == nil {
		return entitlement, err
	}
	if entitlement.PropID <= 0 || entitlement.ExpiresAt <= 0 || entitlement.Multiplier <= 0 || entitlement.Multiplier >= 1 {
		return nil, nil
	}
	return entitlement, nil
}

type FundingSource interface {
	Source() string
	PreConsume(amount int) error
	Settle(delta int) error
	Refund() error
}

type ReservableFundingSource interface {
	ReserveAdditional(amount int64) error
}

type fundingAvailableBalance interface {
	AvailableBalance() (int64, error)
}

type SubscriptionFunding struct {
	creditPack                bool
	CreditPackGrantedQuota    int64
	CreditPackPaidQuota       int64
	CreditPackChannelShareBPS int64
	requestID                 string
	userID                    int
	modelName                 string
	amount                    int64
	subscriptionID            int
	preConsumed               int64
	AmountTotal               int64
	AmountUsedAfter           int64
	PlanId                    int
	PlanTitle                 string
	reservationID             string
	accountID                 string
	settlementID              string
}

func (s *SubscriptionFunding) Source() string { return BillingSourceSubscription }

func (s *SubscriptionFunding) PreConsume(amount int) error {
	preConsume := subscriptionFundingHooks.PreConsume
	if s.creditPack {
		preConsume = subscriptionFundingHooks.PreConsumeCreditPack
	}
	if preConsume == nil {
		return errors.New("subscription funding pre-consume hook is not registered")
	}
	s.amount = int64(amount)
	res, err := preConsume(s.requestID, s.userID, s.modelName, s.amount)
	if err != nil {
		return err
	}
	s.subscriptionID = res.UserSubscriptionID
	s.CreditPackGrantedQuota = res.CreditPackGrantedQuota
	s.CreditPackPaidQuota = res.CreditPackPaidQuota
	s.CreditPackChannelShareBPS = res.CreditPackChannelShareBPS
	s.preConsumed = res.PreConsumed
	s.AmountTotal = res.AmountTotal
	s.AmountUsedAfter = res.AmountUsedAfter
	s.reservationID = res.ReservationID
	s.accountID = res.AccountID
	if s.reservationID == "" {
		if reservation, err := findSubscriptionReservation(s.requestID); err != nil {
			return err
		} else if reservation != nil {
			s.reservationID = reservation.ReservationID
			s.accountID = reservation.AccountID
		}
	}
	s.PlanId = res.PlanID
	s.PlanTitle = res.PlanTitle
	if s.PlanId == 0 && subscriptionFundingHooks.GetPlanInfo != nil {
		if planInfo, err := subscriptionFundingHooks.GetPlanInfo(res.UserSubscriptionID); err == nil && planInfo != nil {
			s.PlanId = planInfo.PlanID
			s.PlanTitle = planInfo.PlanTitle
		}
	}
	if s.creditPack && (s.CreditPackGrantedQuota <= 0 || s.CreditPackPaidQuota <= 0 ||
		s.CreditPackPaidQuota > s.CreditPackGrantedQuota || s.CreditPackChannelShareBPS < 0 || s.CreditPackChannelShareBPS > 10000) {
		return errors.Join(errors.New("invalid credit pack funding snapshot"), s.Refund())
	}
	if !s.creditPack && s.CreditPackGrantedQuota > 0 {
		return errors.Join(errors.New("credit pack cannot use legacy subscription pricing"), s.Refund())
	}
	return nil
}

func (s *SubscriptionFunding) Settle(delta int) error {
	actual := s.preConsumed + int64(delta)
	if actual < 0 {
		actual = 0
	}
	if s.reservationID != "" && subscriptionFundingHooks.SettleReservation != nil {
		if err := subscriptionFundingHooks.SettleReservation(s.requestID, s.subscriptionID, s.modelName, actual); err != nil {
			return err
		}
		return s.loadSettlement()
	}
	if s.AmountTotal == 0 && subscriptionFundingHooks.SettleReservation != nil {
		return subscriptionFundingHooks.SettleReservation(s.requestID, s.subscriptionID, s.modelName, actual)
	}
	return adjustSubscriptionUsageDelta(s.subscriptionID, s.modelName, int64(delta), "relay-settle:"+s.requestID)
}

func (s *SubscriptionFunding) ReservationID() string { return s.reservationID }

func (s *SubscriptionFunding) RequestID() string { return s.requestID }

func (s *SubscriptionFunding) AccountID() string { return s.accountID }

func (s *SubscriptionFunding) SettlementID() string { return s.settlementID }

func (s *SubscriptionFunding) AvailableBalance() (int64, error) {
	if s.accountID == "" {
		return 0, errors.New("subscription ledger account is missing")
	}
	var snapshot billingschema.BillingBalanceSnapshot
	if err := platformdb.DB.Where("account_id = ?", s.accountID).First(&snapshot).Error; err != nil {
		return 0, err
	}
	return snapshot.AvailableBalance, nil
}

func (s *SubscriptionFunding) loadSettlement() error {
	if s.reservationID == "" {
		return errors.New("subscription ledger reservation is missing")
	}
	var settlement billingschema.BillingSettlement
	if err := platformdb.DB.Where("reservation_id = ?", s.reservationID).First(&settlement).Error; err != nil {
		return err
	}
	s.settlementID = settlement.SettlementID
	return nil
}

func (s *SubscriptionFunding) ReserveAdditional(amount int64) error {
	if amount <= 0 {
		return nil
	}
	if s.AmountTotal == 0 && subscriptionFundingHooks.ReserveUnlimitedTarget != nil {
		if err := subscriptionFundingHooks.ReserveUnlimitedTarget(s.requestID, s.subscriptionID, s.modelName, s.preConsumed+amount); err != nil {
			return err
		}
	} else if subscriptionFundingHooks.ReserveAdditional == nil {
		return errors.New("subscription additional reservation hook is not registered")
	} else if err := subscriptionFundingHooks.ReserveAdditional(s.requestID, s.subscriptionID, s.modelName, amount); err != nil {
		return err
	}
	s.preConsumed += amount
	s.AmountUsedAfter += amount
	return nil
}

func (s *SubscriptionFunding) Refund() error {
	if s.preConsumed <= 0 {
		return nil
	}
	if subscriptionFundingHooks.RefundPreConsume == nil {
		return errors.New("subscription funding refund hook is not registered")
	}
	return refundWithRetry(func() error {
		return subscriptionFundingHooks.RefundPreConsume(s.requestID)
	})
}

func findSubscriptionReservation(requestID string) (*billingschema.BillingReservation, error) {
	var reservation billingschema.BillingReservation
	err := platformdb.DB.Where("idempotency_key = ?", "subscription:"+requestID+":reserve").First(&reservation).Error
	if err == nil {
		return &reservation, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return nil, err
}

func refundWithRetry(fn func() error) error {
	if fn == nil {
		return nil
	}
	const maxAttempts = 3
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		if err := fn(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if i < maxAttempts-1 {
			time.Sleep(time.Duration(200*(i+1)) * time.Millisecond)
		}
	}
	return lastErr
}
