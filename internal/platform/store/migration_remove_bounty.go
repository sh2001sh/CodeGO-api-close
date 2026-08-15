package store

import "gorm.io/gorm"

// migrateRemoveBountyMarket permanently removes the retired bounty feature tables.
func migrateRemoveBountyMarket(tx *gorm.DB) error {
	tables := []string{
		"bounty_material_replies",
		"bounty_material_requests",
		"bounty_applications",
		"bounty_submissions",
		"bounty_disputes",
		"bounty_notifications",
		"bounty_events",
		"bounty_reports",
		"bounty_tasks",
	}
	for _, table := range tables {
		if tx.Migrator().HasTable(table) {
			if err := tx.Migrator().DropTable(table); err != nil {
				return err
			}
		}
	}
	return nil
}
