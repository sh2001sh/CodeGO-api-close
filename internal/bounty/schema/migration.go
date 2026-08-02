package schema

import "gorm.io/gorm"

// AutoMigrateModels returns every persistent model owned by the bounty domain.
func AutoMigrateModels(tx *gorm.DB) error {
	return tx.AutoMigrate(
		&BountyTask{},
		&BountyApplication{},
		&BountyMaterialRequest{},
		&BountyMaterialReply{},
		&BountySubmission{},
		&BountyDispute{},
		&BountyEvent{},
		&BountyNotification{},
		&BountyReport{},
	)
}
