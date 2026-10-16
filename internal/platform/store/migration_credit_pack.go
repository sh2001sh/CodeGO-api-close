package store

import (
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"gorm.io/gorm"
)

func migrateCreditPackFundingPolicy(tx *gorm.DB) error {
	for _, item := range []struct {
		model  any
		fields []string
	}{
		{&commerceschema.SubscriptionOrder{}, []string{"CreditPackPaidQuota", "CreditPackChannelShareBPS"}},
		{&commerceschema.UserSubscription{}, []string{"CreditPackGrantedQuota", "CreditPackPaidQuota", "CreditPackChannelShareBPS"}},
		{&commerceschema.SubscriptionClaudeConversion{}, []string{"TargetCreditPackSubscriptionId"}},
		{&gatewayschema.Channel{}, []string{"CreditPackSupported"}},
		{&marketplaceschema.Channel{}, []string{"CreditPackSupported"}},
		{&marketplaceschema.Settlement{}, []string{"CreditPackGrantedQuota", "CreditPackPaidQuota", "CreditPackChannelShareBPS", "CreditPackDiscountAmount", "CreditPackPlatformSubsidyAmount"}},
	} {
		for _, field := range item.fields {
			if !tx.Migrator().HasColumn(item.model, field) {
				if err := tx.Migrator().AddColumn(item.model, field); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
