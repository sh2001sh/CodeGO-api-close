package app

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	unifiedCreditSchemaVersionKey = "billing_schema_version"
	unifiedCreditDefaultRateNum   = int64(1)
	unifiedCreditDefaultRateDen   = int64(4)
	sunbornUnifiedCreditRateNum   = int64(11)
	sunbornUnifiedCreditRateDen   = int64(100)
)

type UnifiedCreditMigrationSummary struct {
	UsersPending              int64 `json:"users_pending"`
	SubscriptionsPending      int64 `json:"subscriptions_pending"`
	SubscriptionsNeedReview   int64 `json:"subscriptions_need_review"`
	LegacyGPTQuota            int64 `json:"legacy_gpt_quota"`
	ConvertedUnifiedQuota     int64 `json:"converted_unified_quota"`
	SubscriptionUnifiedQuota  int64 `json:"subscription_unified_quota"`
	SpecialRateUsers          int64 `json:"special_rate_users"`
	SpecialRateConvertedQuota int64 `json:"special_rate_converted_quota"`
}

type UnifiedCreditMigrationDetail struct {
	Migration   *commerceschema.UnifiedCreditUserMigration  `json:"migration,omitempty"`
	Settlements []commerceschema.SubscriptionTierSettlement `json:"settlements"`
}

// GetUnifiedCreditMigrationDetail returns the current user's auditable cutover records.
func GetUnifiedCreditMigrationDetail(userID int) (*UnifiedCreditMigrationDetail, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	detail := &UnifiedCreditMigrationDetail{Settlements: []commerceschema.SubscriptionTierSettlement{}}
	var migration commerceschema.UnifiedCreditUserMigration
	err := platformdb.DB.Where("user_id = ?", userID).First(&migration).Error
	if err == nil {
		detail.Migration = &migration
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := platformdb.DB.Where("user_id = ?", userID).Order("id asc").Find(&detail.Settlements).Error; err != nil {
		return nil, err
	}
	return detail, nil
}

// InspectUnifiedCreditMigration calculates a read-only wallet cutover summary.
func InspectUnifiedCreditMigration() (UnifiedCreditMigrationSummary, error) {
	var summary UnifiedCreditMigrationSummary
	var users []identityschema.User
	if err := platformdb.DB.Where("quota > ?", 0).Order("id asc").Find(&users).Error; err != nil {
		return summary, err
	}
	for _, user := range users {
		numerator, denominator := unifiedCreditConversionRate(user)
		converted := roundedRatio(int64(user.Quota), numerator, denominator)
		summary.LegacyGPTQuota += int64(user.Quota)
		summary.ConvertedUnifiedQuota += converted
		if denominator != unifiedCreditDefaultRateDen || numerator != unifiedCreditDefaultRateNum {
			summary.SpecialRateUsers++
			summary.SpecialRateConvertedQuota += converted
		}
	}
	summary.UsersPending = int64(len(users))
	return summary, nil
}

// ApplyUnifiedCreditMigration converts legacy wallet balances without changing monthly passes.
func ApplyUnifiedCreditMigration() (UnifiedCreditMigrationSummary, error) {
	summary, err := InspectUnifiedCreditMigration()
	if err != nil {
		return UnifiedCreditMigrationSummary{}, err
	}
	userIDs, err := unifiedCreditMigrationUserIDs()
	if err != nil {
		return UnifiedCreditMigrationSummary{}, err
	}
	for _, userID := range userIDs {
		if err := applyUnifiedCreditMigrationForUser(userID); err != nil {
			return UnifiedCreditMigrationSummary{}, fmt.Errorf("migrate user %d: %w", userID, err)
		}
		_ = identitystore.InvalidateUserCache(userID)
	}
	if err := normalizeLegacySealedBlindBoxRewards(); err != nil {
		return UnifiedCreditMigrationSummary{}, err
	}
	if err := migrateLegacyPaidBlindBoxOrdersToInventory(); err != nil {
		return UnifiedCreditMigrationSummary{}, err
	}
	if err := migrateUnifiedCreditGPTGroupRatios(); err != nil {
		return UnifiedCreditMigrationSummary{}, err
	}
	if err := platformstore.UpdateOption(unifiedCreditSchemaVersionKey, commerceschema.UnifiedCreditMigrationVersion); err != nil {
		return UnifiedCreditMigrationSummary{}, err
	}
	return summary, nil
}

// ValidateUnifiedCreditSchemaVersion prevents the new runtime from accepting
// traffic before the one-time business migration has completed.
func ValidateUnifiedCreditSchemaVersion() error {
	var value string
	if err := platformdb.DB.Table("options").Select("value").Where("key = ?", unifiedCreditSchemaVersionKey).Scan(&value).Error; err != nil {
		return err
	}
	if strings.TrimSpace(value) != commerceschema.UnifiedCreditMigrationVersion {
		return fmt.Errorf("%s must be %s before this runtime can start", unifiedCreditSchemaVersionKey, commerceschema.UnifiedCreditMigrationVersion)
	}
	return nil
}

func unifiedCreditMigrationUserIDs() ([]int, error) {
	var userIDs []int
	if err := platformdb.DB.Model(&identityschema.User{}).Where("quota > ?", 0).Order("id asc").Pluck("id", &userIDs).Error; err != nil {
		return nil, err
	}
	return userIDs, nil
}

func applyUnifiedCreditMigrationForUser(userID int) error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var existing commerceschema.UnifiedCreditUserMigration
		err := tx.Where("user_id = ?", userID).First(&existing).Error
		if err == nil && existing.Status == commerceschema.UnifiedCreditMigrationApplied {
			return nil
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var user identityschema.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		now := platformruntime.GetTimestamp()
		migration := existing
		migration.UserId = userID
		migration.Version = commerceschema.UnifiedCreditMigrationVersion
		migration.LegacyGPTQuota = int64(user.Quota)
		numerator, denominator := unifiedCreditConversionRate(user)
		migration.ConvertedUnifiedQuota = roundedRatio(int64(user.Quota), numerator, denominator)
		migration.Status = commerceschema.UnifiedCreditMigrationApplied
		migration.CreatedAt = now

		migration.SubscriptionUnifiedQuota = 0
		migration.ReviewReason = ""
		migration.CompletedAt = now

		if user.Quota > 0 {
			operationID := fmt.Sprintf("unified-credit-v1:user:%d:gpt", userID)
			if err := billingapp.DebitLegacyGPTQuotaTxWithReason(tx, userID, user.Quota, operationID+":debit", "unified_credit_gpt_conversion_debit"); err != nil {
				return err
			}
			if migration.ConvertedUnifiedQuota > 0 {
				if err := billingapp.CreditClaudeWalletQuotaTx(tx, userID, int(migration.ConvertedUnifiedQuota), operationID+":credit", "unified_credit_gpt_conversion_credit"); err != nil {
					return err
				}
			}
		}

		if migration.Id == 0 {
			return tx.Create(&migration).Error
		}
		return tx.Save(&migration).Error
	})
}

func unifiedCreditConversionRate(user identityschema.User) (int64, int64) {
	if strings.EqualFold(strings.TrimSpace(user.Username), "sunborn") {
		return sunbornUnifiedCreditRateNum, sunbornUnifiedCreditRateDen
	}
	return unifiedCreditDefaultRateNum, unifiedCreditDefaultRateDen
}

func roundedRatio(value, numerator, denominator int64) int64 {
	if value <= 0 || numerator <= 0 || denominator <= 0 {
		return 0
	}
	product := new(big.Int).Mul(big.NewInt(value), big.NewInt(numerator))
	divisor := big.NewInt(denominator)
	product.Add(product, new(big.Int).Div(divisor, big.NewInt(2)))
	return product.Div(product, divisor).Int64()
}

func normalizeLegacySealedBlindBoxRewards() error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var items []commerceschema.BalanceBlindBoxItem
		if err := tx.Where("status = ? AND reward_wallet_type = ?", commerceschema.BalanceBlindBoxItemStatusAvailable, string(commerceschema.BlindBoxRewardWalletTypeDefault)).Find(&items).Error; err != nil {
			return err
		}
		for index := range items {
			item := &items[index]
			item.RewardWalletType = string(commerceschema.BlindBoxRewardWalletTypeClaude)
			item.RewardType = commerceschema.BlindBoxRewardTypeClaudeQuota
			item.CreditAmount = roundedRatio(item.CreditAmount, 1, 4)
			item.RewardUSD = float64(item.CreditAmount) / float64(platformruntime.QuotaPerUnit)
			item.RewardTitle = fmt.Sprintf("%.2f 统一额度奖励", item.RewardUSD)
			if err := tx.Save(item).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func migrateLegacyPaidBlindBoxOrdersToInventory() error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var orders []commerceschema.BlindBoxOrder
		if err := tx.Set("gorm:query_option", "FOR UPDATE").
			Where("status = ? AND opened_count < quantity", "success").
			Order("id asc").Find(&orders).Error; err != nil {
			return err
		}
		for index := range orders {
			order := &orders[index]
			remaining := order.Quantity - order.OpenedCount
			if remaining <= 0 {
				continue
			}
			inventoryOrder := *order
			inventoryOrder.Quantity = remaining
			if err := issuePaidBlindBoxOrderInventoryTx(tx, &inventoryOrder); err != nil {
				return fmt.Errorf("migrate blind box order %d: %w", order.Id, err)
			}
			if err := tx.Model(order).Update("opened_count", order.Quantity).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
