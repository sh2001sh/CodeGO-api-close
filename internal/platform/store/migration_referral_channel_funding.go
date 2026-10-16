package store

import (
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"gorm.io/gorm"
)

func migrateReferralChannelFunding(tx *gorm.DB) error {
	// Historical settlements keep their original earnings and release state.
	// Only newly recorded credit-pack settlements wait for referral funding.
	for _, column := range []struct {
		model any
		name  string
	}{
		{&marketplaceschema.Settlement{}, "ReferralRebateAmount"},
		{&marketplaceschema.Settlement{}, "ReferralRebatePending"},
		{&billingschema.ReferralConsumptionRebate{}, "FundingSource"},
		{&billingschema.ReferralConsumptionRebate{}, "MarketplaceSettlementID"},
	} {
		if !tx.Migrator().HasColumn(column.model, column.name) {
			if err := tx.Migrator().AddColumn(column.model, column.name); err != nil {
				return err
			}
		}
	}
	return nil
}
