package store

import (
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"gorm.io/gorm"
)

func migrateRoutePoolCreditPackFilter(tx *gorm.DB) error {
	if tx.Migrator().HasColumn(&marketplaceschema.RoutePool{}, "AutoBuildCreditPackOnly") {
		return nil
	}
	return tx.Migrator().AddColumn(&marketplaceschema.RoutePool{}, "AutoBuildCreditPackOnly")
}
