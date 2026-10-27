package store

import (
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"gorm.io/gorm"
)

func migrateMarketplaceSecurityAlertEmail(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&marketplaceschema.Channel{}) ||
		tx.Migrator().HasColumn(&marketplaceschema.Channel{}, "SecurityAlertEmailEnabled") {
		return nil
	}
	return tx.Migrator().AddColumn(&marketplaceschema.Channel{}, "SecurityAlertEmailEnabled")
}
