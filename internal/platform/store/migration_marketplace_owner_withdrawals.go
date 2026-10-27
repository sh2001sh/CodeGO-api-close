package store

import (
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"gorm.io/gorm"
)

func migrateMarketplaceOwnerWithdrawals(tx *gorm.DB) error {
	return tx.AutoMigrate(&marketplaceschema.OwnerWithdrawal{})
}
