package app

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SubscriptionResetRedemptionPreview struct {
	Eligible         bool    `json:"eligible"`
	AvailableCount   int     `json:"available_count"`
	PlanID           int     `json:"plan_id"`
	PlanTitle        string  `json:"plan_title"`
	GrantedQuota     int64   `json:"granted_quota"`
	BaseQuota        int64   `json:"base_quota"`
	BonusQuota       int64   `json:"bonus_quota"`
	PriceAmount      float64 `json:"price_amount"`
	Currency         string  `json:"currency"`
	IneligibleReason string  `json:"ineligible_reason,omitempty"`
}

type RedeemSubscriptionResetOpportunityRequest struct {
	RequestID string `json:"request_id"`
}

type SubscriptionResetRedemptionResult struct {
	RequestID        string                                             `json:"request_id"`
	SubscriptionID   int                                                `json:"subscription_id"`
	PlanID           int                                                `json:"plan_id"`
	GrantedQuota     int64                                              `json:"granted_quota"`
	ResetOpportunity commerceschema.SubscriptionResetOpportunitySummary `json:"reset_opportunity"`
	Replayed         bool                                               `json:"replayed"`
}

func lowestEnabledCreditPackTx(tx *gorm.DB, lock bool) (*commerceschema.SubscriptionPlan, error) {
	query := tx.Where("enabled = ? AND internal_only = ? AND plan_type = ? AND price_amount > 0 AND total_amount > 0", true, false, commerceschema.SubscriptionPlanTypeCreditPack)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "SHARE"})
	}
	var plan commerceschema.SubscriptionPlan
	if err := query.Order("price_amount asc, id asc").First(&plan).Error; err != nil {
		return nil, err
	}
	return &plan, nil
}

func BuildSubscriptionResetRedemptionPreview(userID int) (*SubscriptionResetRedemptionPreview, error) {
	preview := &SubscriptionResetRedemptionPreview{}
	var account commerceschema.SubscriptionResetOpportunityAccount
	if err := platformdb.DB.Where("user_id = ?", userID).First(&account).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	preview.AvailableCount = account.AvailableTotal
	plan, err := lowestEnabledCreditPackTx(platformdb.DB, false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		preview.IneligibleReason = "当前没有可兑换的新套餐"
		return preview, nil
	}
	if err != nil {
		return nil, err
	}
	preview.PlanID, preview.PlanTitle, preview.GrantedQuota = plan.Id, plan.Title, plan.TotalAmount
	preview.BaseQuota, preview.PriceAmount, preview.Currency = plan.TotalAmount, plan.PriceAmount, plan.Currency
	preview.Eligible = account.AvailableTotal > 0
	if !preview.Eligible {
		preview.IneligibleReason = commerceschema.ErrSubscriptionResetOpportunityUnavailable.Error()
	}
	return preview, nil
}

// RedeemSubscriptionResetOpportunity exchanges one existing refresh credit for
// one permanent lowest-tier package. It creates no cash order or paid receipt.
func RedeemSubscriptionResetOpportunity(userID int, req RedeemSubscriptionResetOpportunityRequest) (*SubscriptionResetRedemptionResult, error) {
	requestID := strings.TrimSpace(req.RequestID)
	if userID <= 0 || requestID == "" || len(requestID) > 128 {
		return nil, errors.New("invalid reset redemption request")
	}
	key := fmt.Sprintf("reset-redeem:%d:%x", userID, sha256.Sum256([]byte(requestID)))
	result := &SubscriptionResetRedemptionResult{RequestID: requestID}
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		account, err := getOrCreateSubscriptionResetOpportunityAccountTx(tx, userID)
		if err != nil {
			return err
		}
		var existing commerceschema.SubscriptionResetOpportunityLedger
		if err := tx.Where("event_key = ? AND user_id = ?", key, userID).First(&existing).Error; err == nil {
			if existing.ChangeType != "redeem" || existing.Delta != -1 || existing.SourceType != "credit_pack_redemption" {
				return errors.New("reset redemption receipt is inconsistent")
			}
			id, err := strconv.Atoi(existing.SourceRef)
			if err != nil {
				return err
			}
			var sub commerceschema.UserSubscription
			if err := tx.Where("id = ? AND user_id = ? AND source = ?", id, userID, "reset_redemption").First(&sub).Error; err != nil {
				return err
			}
			result.SubscriptionID, result.PlanID, result.GrantedQuota = sub.Id, sub.PlanId, sub.CreditPackGrantedQuota
			result.ResetOpportunity, result.Replayed = buildSubscriptionResetOpportunitySummary(account), true
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if account.AvailableTotal <= 0 {
			return commerceschema.ErrSubscriptionResetOpportunityUnavailable
		}
		plan, err := lowestEnabledCreditPackTx(tx, true)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("当前没有可兑换的新套餐")
		}
		if err != nil {
			return err
		}
		// Equal granted/paid snapshot makes this platform-funded gift exempt from
		// channel promotional charges. No successful cash order exists, so neither
		// refunds nor referral-paid consumption can treat it as cash principal.
		sub, err := createCreditPackBalanceWithReasonTx(tx, userID, plan.Id, plan.TotalAmount, plan.TotalAmount, creditPackChannelShareBPS, key, "reset_opportunity_redemption")
		if err != nil {
			return err
		}
		if err := tx.Model(sub).Update("source", "reset_redemption").Error; err != nil {
			return err
		}
		account.AvailableTotal--
		account.UsedTotal++
		if err := tx.Save(account).Error; err != nil {
			return err
		}
		ledger := &commerceschema.SubscriptionResetOpportunityLedger{UserId: userID, ChangeType: "redeem", Delta: -1, BalanceAfter: account.AvailableTotal, SourceType: "credit_pack_redemption", SourceRef: strconv.Itoa(sub.Id), EventKey: key, Note: "刷新次数兑换最低档新套餐"}
		if err := tx.Create(ledger).Error; err != nil {
			return err
		}
		result.SubscriptionID, result.PlanID, result.GrantedQuota = sub.Id, sub.PlanId, sub.CreditPackGrantedQuota
		result.ResetOpportunity = buildSubscriptionResetOpportunitySummary(account)
		return nil
	})
	return result, err
}
