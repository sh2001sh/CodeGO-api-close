package store

import (
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"gorm.io/gorm"
)

func migrateMarketplacePresentation(tx *gorm.DB) error {
	// Existing groups only need additive columns; do not rewrite established
	// PostgreSQL column types while adding public presentation fields.
	group := &marketplaceschema.Group{}
	if !tx.Migrator().HasTable(group) {
		if err := tx.AutoMigrate(group); err != nil {
			return err
		}
	} else {
		for _, field := range []string{"CustomName", "Description", "Tags", "ModerationStatus", "ModerationReason", "ModerationCheckedAt"} {
			if !tx.Migrator().HasColumn(group, field) {
				if err := tx.Migrator().AddColumn(group, field); err != nil {
					return err
				}
			}
		}
	}
	return tx.AutoMigrate(&marketplaceschema.Shop{}, &marketplaceschema.ShopRating{}, &marketplaceschema.MarketReport{})
}
