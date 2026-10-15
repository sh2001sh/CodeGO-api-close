package app

import (
	"errors"
	"fmt"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

func migrateBlindBoxLegacyCreditsTx(tx *gorm.DB) error {
	if tx == nil {
		return errors.New("transaction is required")
	}

	now := platformruntime.GetTimestamp()
	var pending []commerceschema.BlindBoxCredit
	if err := tx.Set("gorm:query_option", "FOR UPDATE").
		Where("migrated_at = 0").
		Order("id asc").
		Find(&pending).Error; err != nil {
		return err
	}

	for i := range pending {
		credit := &pending[i]
		if credit.Id <= 0 {
			continue
		}
		if credit.RemainingAmount <= 0 || (credit.ExpiresAt > 0 && credit.ExpiresAt <= now) {
			if err := tx.Delete(&commerceschema.BlindBoxCredit{}, credit.Id).Error; err != nil {
				return err
			}
			continue
		}
		if credit.OpenRecordId <= 0 {
			if err := tx.Delete(&commerceschema.BlindBoxCredit{}, credit.Id).Error; err != nil {
				return err
			}
			continue
		}

		var record commerceschema.BlindBoxOpenRecord
		if err := tx.Where("id = ?", credit.OpenRecordId).First(&record).Error; err != nil {
			if err := tx.Delete(&commerceschema.BlindBoxCredit{}, credit.Id).Error; err != nil {
				return err
			}
			continue
		}

		walletType := normalizeBlindBoxRewardWalletType(record.RewardWalletType)
		if record.RewardType == commerceschema.BlindBoxRewardTypeClaudeQuota {
			walletType = commerceschema.BlindBoxRewardWalletTypeClaude
		}
		if err := creditBlindBoxRewardByWalletTx(
			tx,
			credit.UserId,
			credit.RemainingAmount,
			walletType,
			fmt.Sprintf("blind-box:legacy-credit:%d:%s", credit.Id, walletType),
			"blind_box_legacy_migration",
		); err != nil {
			return err
		}

		if err := tx.Delete(&commerceschema.BlindBoxCredit{}, credit.Id).Error; err != nil {
			return err
		}
		_ = identitystore.InvalidateUserCache(credit.UserId)
	}
	return nil
}

// MigrateBlindBoxLegacyCredits migrates legacy blind-box credit rows into billing-backed wallets.
func MigrateBlindBoxLegacyCredits() error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		return MigrateBlindBoxLegacyCreditsTx(tx)
	})
}

// MigrateBlindBoxLegacyCreditsTx transfers outstanding legacy reward credits
// into the ledger-backed wallets inside the caller's transaction.
func MigrateBlindBoxLegacyCreditsTx(tx *gorm.DB) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if !tx.Migrator().HasTable(&commerceschema.BlindBoxCredit{}) {
		return nil
	}
	return migrateBlindBoxLegacyCreditsTx(tx)
}
