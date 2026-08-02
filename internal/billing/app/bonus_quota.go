package app

import (
	"errors"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"

	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

func GrantBonusWalletQuotaTx(tx *gorm.DB, userID int, amount int64, sourceType string, sourceID string, key string) (bool, error) {
	if tx == nil {
		tx = platformdb.DB
	}
	if userID <= 0 || amount <= 0 || key == "" {
		return false, errors.New("invalid bonus quota credit")
	}

	var existing billingschema.BonusQuotaCredit
	if err := tx.Where("idempotency_key = ?", key).First(&existing).Error; err == nil {
		return false, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}

	credit := billingschema.BonusQuotaCredit{
		UserId:          userID,
		OriginalAmount:  amount,
		RemainingAmount: amount,
		SourceType:      sourceType,
		SourceId:        sourceID,
		IdempotencyKey:  key,
		Status:          billingschema.BonusQuotaStatusActive,
	}
	if err := tx.Create(&credit).Error; err != nil {
		return false, err
	}
	if err := CreditWalletQuotaTx(tx, userID, int(amount), key, "bonus_quota_credit"); err != nil {
		return false, err
	}
	_ = identitystore.InvalidateUserCache(userID)
	return true, nil
}

func ConsumeBonusWalletQuotaCredits(userID int, amount int64) {
	if err := consumeBonusWalletQuotaCredits(userID, amount); err != nil {
		platformobservability.SysLog("failed to consume bonus quota credits: " + err.Error())
	}
}

func SumAvailableBonusWalletQuota(userID int) (int64, error) {
	var total int64
	err := platformdb.DB.Model(&billingschema.BonusQuotaCredit{}).
		Where("user_id = ? AND remaining_amount > 0", userID).
		Select("COALESCE(SUM(remaining_amount), 0)").
		Scan(&total).
		Error
	return total, err
}

func consumeBonusWalletQuotaCredits(userID int, amount int64) error {
	if userID <= 0 || amount <= 0 {
		return nil
	}
	if platformdb.DB == nil || !platformdb.DB.Migrator().HasTable(&billingschema.BonusQuotaCredit{}) {
		return nil
	}

	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		return ConsumeBonusWalletQuotaCreditsTx(tx, userID, amount)
	})
}

// ConsumeBonusWalletQuotaCreditsTx consumes bonus provenance inside the caller's transaction.
func ConsumeBonusWalletQuotaCreditsTx(tx *gorm.DB, userID int, amount int64) error {
	if tx == nil || userID <= 0 || amount <= 0 {
		return nil
	}
	if !tx.Migrator().HasTable(&billingschema.BonusQuotaCredit{}) {
		return nil
	}

	remaining := amount
	var credits []billingschema.BonusQuotaCredit
	if err := tx.Set("gorm:query_option", "FOR UPDATE").
		Where("user_id = ? AND remaining_amount > 0", userID).
		Order("id asc").
		Find(&credits).Error; err != nil {
		return err
	}
	for _, credit := range credits {
		if remaining <= 0 {
			break
		}
		use := credit.RemainingAmount
		if use > remaining {
			use = remaining
		}
		credit.RemainingAmount -= use
		if credit.RemainingAmount <= 0 {
			credit.RemainingAmount = 0
			credit.Status = billingschema.BonusQuotaStatusExhausted
		}
		if err := tx.Save(&credit).Error; err != nil {
			return err
		}
		remaining -= use
	}
	return nil
}
