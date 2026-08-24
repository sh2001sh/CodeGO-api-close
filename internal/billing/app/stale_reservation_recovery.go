package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	staleRelayReservationAge      = 24 * time.Hour
	staleReservationRecoveryBatch = 100
)

type StaleReservationRecoveryResult struct {
	Requests int
	Failed   int
	Released int
	Settled  int
	Capped   int
	Amount   int64
}

type staleReservationCandidate struct {
	AccountID string
	RequestID string
}

type recoveryEvidence struct {
	actualAmount int64
	evidenceID   string
	found        bool
}

type recoveryLogOther struct {
	SubscriptionConsumed    *int64 `json:"subscription_consumed"`
	SubscriptionPreConsumed *int64 `json:"subscription_pre_consumed"`
}

// RecoverStaleRelayReservations closes expired relay holds from durable usage
// evidence, or releases them when the request produced no billable usage.
func RecoverStaleRelayReservations(ctx context.Context, now time.Time, limit int) (StaleReservationRecoveryResult, error) {
	if platformdb.DB == nil {
		return StaleReservationRecoveryResult{}, fmt.Errorf("primary database is not initialized")
	}
	if limit <= 0 {
		limit = staleReservationRecoveryBatch
	}
	candidates, err := loadStaleReservationCandidates(ctx, now, limit)
	if err != nil {
		return StaleReservationRecoveryResult{}, err
	}
	result := StaleReservationRecoveryResult{}
	var recoveryErrors []error
	for _, candidate := range candidates {
		item, userID, err := recoverStaleReservation(ctx, candidate, now)
		if err != nil {
			result.Failed++
			recoveryErrors = append(recoveryErrors, fmt.Errorf("recover request %s: %w", candidate.RequestID, err))
			continue
		}
		result.Requests += item.Requests
		result.Released += item.Released
		result.Settled += item.Settled
		result.Capped += item.Capped
		result.Amount += item.Amount
		if userID > 0 {
			_ = platformcache.DeleteUserCache(userID)
		}
	}
	return result, errors.Join(recoveryErrors...)
}

func loadStaleReservationCandidates(ctx context.Context, now time.Time, limit int) ([]staleReservationCandidate, error) {
	legacyCutoff := now.Add(-staleRelayReservationAge)
	reservationTable := billingschema.BillingReservation{}.TableName()
	accountTable := billingschema.BillingAccount{}.TableName()
	var candidates []staleReservationCandidate
	err := platformdb.DB.WithContext(ctx).Table(reservationTable+" AS reservation").
		Select("reservation.account_id, reservation.request_id").
		Joins("JOIN "+accountTable+" AS account ON account.account_id = reservation.account_id").
		Where("reservation.status = ?", billingschema.BillingReservationStatusOpen).
		Where("reservation.request_id <> ?", "").
		Where("account.account_type IN ?", []string{billingAccountTypeWallet, billingAccountTypeClaudeWallet, "subscription"}).
		Where("(reservation.expires_at IS NOT NULL AND reservation.expires_at <= ?) OR (reservation.expires_at IS NULL AND reservation.created_at <= ?)", now, legacyCutoff).
		Group("reservation.account_id, reservation.request_id").
		Order("MIN(reservation.created_at) asc, reservation.account_id asc, reservation.request_id asc").
		Limit(limit).
		Scan(&candidates).Error
	return candidates, err
}

func recoverStaleReservation(ctx context.Context, candidate staleReservationCandidate, now time.Time) (StaleReservationRecoveryResult, int, error) {
	result := StaleReservationRecoveryResult{}
	ownerUserID := 0
	err := platformdb.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		account, reservations, eligible, err := lockRecoveryTarget(tx, candidate, now)
		if err != nil || !eligible {
			return err
		}
		evidence, err := loadRecoveryEvidence(tx, account.AccountType, candidate.RequestID)
		if err != nil {
			return err
		}
		if evidence.found {
			capped, settled, err := settleRecoveredReservations(tx, reservations, evidence)
			if err != nil {
				return err
			}
			result.Settled, result.Capped, result.Amount = len(reservations), capped, settled
		} else {
			if err := releaseRecoveredReservations(tx, reservations); err != nil {
				return err
			}
			result.Released = len(reservations)
			for _, reservation := range reservations {
				result.Amount += reservation.ReservedAmount
			}
		}
		result.Requests = 1
		ownerUserID, err = syncRecoveredProjection(tx, account, candidate.RequestID, evidence.found)
		return err
	})
	return result, ownerUserID, err
}

func lockRecoveryTarget(tx *gorm.DB, candidate staleReservationCandidate, now time.Time) (billingschema.BillingAccount, []billingschema.BillingReservation, bool, error) {
	var account billingschema.BillingAccount
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ?", candidate.AccountID).First(&account).Error; err != nil {
		return account, nil, false, err
	}
	var reservations []billingschema.BillingReservation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ? AND request_id = ? AND status = ?", candidate.AccountID, candidate.RequestID, billingschema.BillingReservationStatusOpen).
		Order("created_at asc, reservation_id asc").Find(&reservations).Error; err != nil {
		return account, nil, false, err
	}
	if len(reservations) == 0 {
		return account, nil, false, nil
	}
	legacyCutoff := now.Add(-staleRelayReservationAge)
	for _, reservation := range reservations {
		if reservation.ExpiresAt != nil && reservation.ExpiresAt.After(now) {
			return account, nil, false, nil
		}
		if reservation.ExpiresAt == nil && reservation.CreatedAt.After(legacyCutoff) {
			return account, nil, false, nil
		}
	}
	return account, reservations, true, nil
}

func loadRecoveryEvidence(tx *gorm.DB, accountType, requestID string) (recoveryEvidence, error) {
	var economics billingschema.RequestEconomics
	if err := tx.Where("request_id = ?", requestID).First(&economics).Error; err == nil {
		return recoveryEvidence{actualAmount: economics.ActualAmount, evidenceID: requestID, found: true}, nil
	} else if err != gorm.ErrRecordNotFound {
		return recoveryEvidence{}, err
	}
	var log auditschema.Log
	if err := tx.Where("request_id = ? AND type = ?", requestID, auditschema.LogTypeConsume).Order("id desc").First(&log).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return recoveryEvidence{}, nil
		}
		return recoveryEvidence{}, err
	}
	actual := int64(log.Quota)
	if accountType == "subscription" {
		var other recoveryLogOther
		if err := platformencoding.UnmarshalString(log.Other, &other); err == nil && other.SubscriptionConsumed != nil {
			actual = max(*other.SubscriptionConsumed, 0)
			var reservedTotal int64
			if err := tx.Model(&billingschema.BillingReservation{}).
				Where("request_id = ?", requestID).
				Select("COALESCE(SUM(reserved_amount), 0)").
				Scan(&reservedTotal).Error; err != nil {
				return recoveryEvidence{}, err
			}
			if other.SubscriptionPreConsumed != nil {
				duplicated := *other.SubscriptionPreConsumed - reservedTotal
				if duplicated > 0 {
					actual = max(actual-duplicated, 0)
				}
			}
		}
	}
	return recoveryEvidence{actualAmount: actual, evidenceID: strconv.Itoa(log.Id), found: true}, nil
}

func settleRecoveredReservations(tx *gorm.DB, reservations []billingschema.BillingReservation, evidence recoveryEvidence) (int, int64, error) {
	var snapshot billingschema.BillingBalanceSnapshot
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ?", reservations[0].AccountID).First(&snapshot).Error; err != nil {
		return 0, 0, err
	}
	reserved := int64(0)
	for _, reservation := range reservations {
		reserved += reservation.ReservedAmount
	}
	actual := evidence.actualAmount
	if actual < 0 {
		actual = 0
	}
	capacity := reserved + snapshot.AvailableBalance
	capped := 0
	if actual > capacity {
		actual, capped = capacity, 1
	}
	remaining := actual
	for index, reservation := range reservations {
		amount := min(remaining, reservation.ReservedAmount)
		if index == len(reservations)-1 && remaining > amount {
			amount = remaining
		}
		if _, err := billingdomain.SettleReservationTx(tx, billingdomain.SettleReservationParams{
			ReservationID: reservation.ReservationID, UsageEvidenceID: evidence.evidenceID, ActualAmount: amount,
			IdempotencyKey: "stale-recovery:" + reservation.ReservationID + ":settle",
		}); err != nil {
			return capped, 0, err
		}
		remaining -= amount
	}
	return capped, actual, nil
}

func releaseRecoveredReservations(tx *gorm.DB, reservations []billingschema.BillingReservation) error {
	for _, reservation := range reservations {
		if _, err := billingdomain.ReleaseReservationTx(tx, billingdomain.ReleaseReservationParams{
			ReservationID:  reservation.ReservationID,
			IdempotencyKey: "stale-recovery:" + reservation.ReservationID + ":release",
			ReasonCode:     "stale_relay_without_usage",
		}); err != nil {
			return err
		}
	}
	return nil
}

func syncRecoveredProjection(tx *gorm.DB, account billingschema.BillingAccount, requestID string, hasUsage bool) (int, error) {
	var snapshot billingschema.BillingBalanceSnapshot
	if err := tx.Where("account_id = ?", account.AccountID).First(&snapshot).Error; err != nil {
		return 0, err
	}
	switch account.AccountType {
	case billingAccountTypeClaudeWallet:
		return int(account.OwnerID), tx.Model(&identityschema.User{}).Where("id = ?", account.OwnerID).Update("claude_quota", snapshot.AvailableBalance).Error
	case billingAccountTypeWallet:
		return int(account.OwnerID), tx.Model(&identityschema.User{}).Where("id = ?", account.OwnerID).Update("quota", snapshot.AvailableBalance).Error
	case "subscription":
		var subscription commerceschema.UserSubscription
		if err := tx.Where("id = ?", account.OwnerID).First(&subscription).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return 0, nil
			}
			return 0, err
		}
		used := max(subscription.AmountTotal-snapshot.AvailableBalance, 0)
		if used > subscription.AmountTotal {
			used = subscription.AmountTotal
		}
		if err := tx.Model(&subscription).Update("amount_used", used).Error; err != nil {
			return 0, err
		}
		if !hasUsage {
			if err := tx.Model(&commerceschema.SubscriptionPreConsumeRecord{}).Where("request_id = ?", requestID).Update("status", "refunded").Error; err != nil {
				return 0, err
			}
		}
		return subscription.UserId, nil
	default:
		return 0, nil
	}
}
