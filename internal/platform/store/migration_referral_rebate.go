package store

import (
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func migrateReferralConsumptionRebate(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&billingschema.ReferralRebateProgram{}, &billingschema.ReferralConsumptionRebate{}); err != nil {
		return err
	}
	for _, model := range []any{&billingschema.FundingLot{}, &billingschema.FundingAllocation{}, &commerceschema.TopUp{}} {
		if !tx.Migrator().HasColumn(model, "PaidQuota") {
			if err := tx.Migrator().AddColumn(model, "PaidQuota"); err != nil {
				return err
			}
		}
	}
	// Never reset rollout time on restart: earlier settlements are not eligible.
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&billingschema.ReferralRebateProgram{
		ID: 1, RateBPS: 300, EffectiveAt: time.Now().Unix(),
	}).Error
}
