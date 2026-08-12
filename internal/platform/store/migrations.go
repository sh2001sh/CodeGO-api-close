package store

import (
	"context"
	"fmt"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	bountyschema "github.com/sh2001sh/new-api/internal/bounty/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	communityschema "github.com/sh2001sh/new-api/internal/community/schema"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	workflowschema "github.com/sh2001sh/new-api/internal/workflow/schema"
	"gorm.io/gorm"
)

type schemaMigration struct {
	ID        string `gorm:"primaryKey;size:128"`
	AppliedAt time.Time
}

func (schemaMigration) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "platform.schema_migrations"
	}
	return "platform_schema_migrations"
}

type schemaMigrationStep struct {
	ID           string
	Run          func(*gorm.DB) error
	RunOutsideTx func(*gorm.DB) error
}

// V2MigrationIDs returns the ordered migration contract required by CodeGo v2.
// Deployment verification uses this list without changing database state.
func V2MigrationIDs() []string {
	return []string{
		"20260710_billing_core",
		"20260710_workflow_core",
		"20260711_subscription_core",
		"20260711_subscription_order_fulfillment",
		"20260711_gateway_execution_core",
		"20260711_gateway_execution_trace",
		"20260712_remove_pet_gamification",
		"20260713_bounty_market",
		"20260713_bounty_market_followups",
		"20260713_bounty_delivery_summary",
		"20260713_bounty_submission_version_index",
		"20260714_user_external_id",
		"20260715_blind_box_admin_grants",
		"20260718_first_purchase_discount",
		"20260718_community_resources",
		"20260719_subscription_first_purchase_discount",
		"20260720_wallet_quota_conversion",
		"20260721_blind_box_zero_hour",
		"20260724_gateway_route_pools",
		"20260724_gateway_route_pool_auto_discovery",
		"20260724_billing_funding_attribution",
		"20260801_daily_lucky_number",
		"20260802_gateway_route_pool_fault_domains",
		"20260804_daily_lucky_reward_notifications",
		"20260805_billing_outbox_pending_lookup",
		"20260808_commerce_invoice_requests",
	}
}

// ApplyV2Migrations applies only v2-owned tables and records every completed step.
// It deliberately excludes legacy table AutoMigrate calls that are unsafe on old SQLite DDL.
func ApplyV2Migrations(ctx context.Context, dryRun bool) error {
	if platformdb.DB == nil {
		return fmt.Errorf("primary database is not initialized")
	}
	if err := ensureCodeGoSchemas(); err != nil {
		return err
	}
	db := platformdb.DB.WithContext(ctx)
	if err := db.AutoMigrate(&schemaMigration{}); err != nil {
		return err
	}

	steps := []schemaMigrationStep{
		{ID: "20260710_billing_core", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{})
		}},
		{ID: "20260710_workflow_core", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&workflowschema.WorkflowTaskWorkflow{}, &workflowschema.WorkflowTaskSnapshot{}, &workflowschema.WorkflowTaskTerminalResult{})
		}},
		{ID: "20260711_subscription_core", Run: func(tx *gorm.DB) error {
			return migrateSubscriptionCore(tx)
		}},
		{ID: "20260711_subscription_order_fulfillment", Run: func(tx *gorm.DB) error {
			if err := migrateSubscriptionOrder(tx); err != nil {
				return err
			}
			return tx.Model(&commerceschema.SubscriptionOrder{}).
				Where("status = ? AND (fulfillment_status = '' OR fulfillment_status IS NULL)", "success").
				Update("fulfillment_status", commerceschema.SubscriptionOrderFulfillmentCompleted).Error
		}},
		{ID: "20260711_gateway_execution_core", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(
				&gatewayschema.RequestExecution{},
				&gatewayschema.GatewayRoutePlan{},
				&gatewayschema.ExecutionAttempt{},
				&gatewayschema.UsageEvidence{},
			)
		}},
		{ID: "20260711_gateway_execution_trace", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(
				&gatewayschema.RequestExecution{},
				&gatewayschema.GatewayRoutePlan{},
				&gatewayschema.ExecutionAttempt{},
				&gatewayschema.UsageEvidence{},
			)
		}},
		{ID: "20260712_remove_pet_gamification", Run: func(tx *gorm.DB) error {
			for _, tableName := range []string{
				"user_companion_pets",
				"daily_mission_rewards",
				"achievement_unlocks",
			} {
				if tx.Migrator().HasTable(tableName) {
					if err := tx.Migrator().DropTable(tableName); err != nil {
						return err
					}
				}
			}
			return nil
		}},
		{ID: "20260713_bounty_market", Run: func(tx *gorm.DB) error {
			return bountyschema.AutoMigrateModels(tx)
		}},
		{ID: "20260713_bounty_market_followups", Run: func(tx *gorm.DB) error {
			return bountyschema.AutoMigrateModels(tx)
		}},
		{ID: "20260713_bounty_delivery_summary", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&bountyschema.BountySubmission{})
		}},
		{ID: "20260713_bounty_submission_version_index", Run: func(tx *gorm.DB) error {
			indexName := "uq_bounty_submissions_task_version"
			if tx.Migrator().HasIndex(&bountyschema.BountySubmission{}, indexName) {
				if err := tx.Migrator().DropIndex(&bountyschema.BountySubmission{}, indexName); err != nil {
					return err
				}
			}
			return tx.Migrator().CreateIndex(&bountyschema.BountySubmission{}, indexName)
		}},
		{ID: "20260714_user_external_id", Run: migrateUserExternalIDs},
		{ID: "20260715_blind_box_admin_grants", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&commerceschema.BlindBoxOrder{}, &commerceschema.BlindBoxGrant{})
		}},
		{ID: "20260718_first_purchase_discount", Run: func(tx *gorm.DB) error {
			return migrateFirstPurchaseDiscount(tx)
		}},
		{ID: "20260718_community_resources", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&communityschema.Resource{})
		}},
		{ID: "20260719_subscription_first_purchase_discount", Run: func(tx *gorm.DB) error {
			return migrateSubscriptionFirstPurchaseDiscount(tx)
		}},
		{ID: "20260720_wallet_quota_conversion", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&commerceschema.WalletQuotaConversion{})
		}},
		{ID: "20260721_blind_box_zero_hour", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&commerceschema.BlindBoxZeroHourState{})
		}},
		{ID: "20260724_gateway_route_pools", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&gatewayschema.RoutePool{}, &gatewayschema.RoutePoolMember{})
		}},
		{ID: "20260724_gateway_route_pool_auto_discovery", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&gatewayschema.RoutePool{}, &gatewayschema.RoutePoolMember{})
		}},
		{ID: "20260724_billing_funding_attribution", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&billingschema.FundingSourcePolicy{}, &billingschema.FundingLot{}, &billingschema.FundingAllocation{}, &billingschema.RequestEconomics{})
		}},
		{ID: "20260801_daily_lucky_number", Run: migrateDailyLuckyNumber},
		{ID: "20260802_gateway_route_pool_fault_domains", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&gatewayschema.RoutePoolMember{})
		}},
		{ID: "20260804_daily_lucky_reward_notifications", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&commerceschema.SubscriptionLuckyRewardNotification{})
		}},
		{ID: "20260805_billing_outbox_pending_lookup", RunOutsideTx: migratePendingOutboxLookupIndex},
		{ID: "20260808_commerce_invoice_requests", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&commerceschema.InvoiceRequest{})
		}},
		{ID: "20260812_blind_box_daily_lucky_numbers", Run: func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&commerceschema.BlindBoxDailyLuckyNumber{}); err != nil {
				return err
			}
			for _, field := range []string{"BlindBoxOpenRecordId", "ParticipationType"} {
				if tx.Migrator().HasColumn(&commerceschema.SubscriptionLuckyReward{}, field) {
					continue
				}
				if err := tx.Migrator().AddColumn(&commerceschema.SubscriptionLuckyReward{}, field); err != nil {
					return err
				}
			}
			return nil
		}},
	}
	for _, step := range steps {
		var applied schemaMigration
		err := db.Where("id = ?", step.ID).First(&applied).Error
		if err == nil {
			if step.ID == "20260715_blind_box_admin_grants" &&
				(!db.Migrator().HasTable(&commerceschema.BlindBoxOrder{}) ||
					!db.Migrator().HasTable(&commerceschema.BlindBoxGrant{})) {
				if dryRun {
					continue
				}
				if err := db.Transaction(func(tx *gorm.DB) error {
					return step.Run(tx)
				}); err != nil {
					return fmt.Errorf("repair migration %s: %w", step.ID, err)
				}
			}
			continue
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		if dryRun {
			continue
		}
		if step.RunOutsideTx != nil {
			if err := step.RunOutsideTx(db); err != nil {
				return fmt.Errorf("apply migration %s: %w", step.ID, err)
			}
			if err := db.Create(&schemaMigration{ID: step.ID}).Error; err != nil {
				return fmt.Errorf("record migration %s: %w", step.ID, err)
			}
			continue
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := step.Run(tx); err != nil {
				return err
			}
			return tx.Create(&schemaMigration{ID: step.ID}).Error
		}); err != nil {
			return fmt.Errorf("apply migration %s: %w", step.ID, err)
		}
	}
	return nil
}

// migratePendingOutboxLookupIndex keeps the ledger worker's pending-event
// query index-only instead of repeatedly scanning the full outbox table.
// PostgreSQL requires CONCURRENTLY outside a transaction to avoid blocking
// relay billing while the index is built on a busy production table.
func migratePendingOutboxLookupIndex(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable(&billingschema.BillingOutboxEvent{}) {
		return nil
	}
	if platformdb.UsingPostgreSQL {
		return db.Exec(`
			CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_billing_outbox_pending_created
			ON billing.outbox_events (created_at, event_id) INCLUDE (account_id)
			WHERE status = 'pending'
		`).Error
	}
	return db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_billing_outbox_pending_created
		ON billing_outbox_events (status, created_at, event_id)
	`).Error
}

func migrateFirstPurchaseDiscount(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&commerceschema.TopUp{}) {
		return tx.AutoMigrate(&commerceschema.TopUp{})
	}
	for _, field := range []string{"FirstPurchaseDiscountApplied", "FirstPurchaseDiscountMultiplier"} {
		if tx.Migrator().HasColumn(&commerceschema.TopUp{}, field) {
			continue
		}
		if err := tx.Migrator().AddColumn(&commerceschema.TopUp{}, field); err != nil {
			return err
		}
	}
	return nil
}

func migrateSubscriptionFirstPurchaseDiscount(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&commerceschema.SubscriptionOrder{}) {
		return tx.AutoMigrate(&commerceschema.SubscriptionOrder{})
	}
	for _, field := range []string{
		"OriginalMoney",
		"FirstPurchaseDiscountApplied",
		"FirstPurchaseDiscountMultiplier",
		"FirstPurchaseDiscountStartAt",
		"FirstPurchaseDiscountEndAt",
	} {
		if tx.Migrator().HasColumn(&commerceschema.SubscriptionOrder{}, field) {
			continue
		}
		if err := tx.Migrator().AddColumn(&commerceschema.SubscriptionOrder{}, field); err != nil {
			return err
		}
	}
	return nil
}

func migrateUserExternalIDs(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&identityschema.User{}) {
		return nil
	}
	if !tx.Migrator().HasColumn(&identityschema.User{}, "ExternalId") {
		if err := tx.Migrator().AddColumn(&identityschema.User{}, "ExternalId"); err != nil {
			return err
		}
	}

	var users []identityschema.User
	if err := tx.Unscoped().Where("external_id IS NULL OR external_id = ''").Find(&users).Error; err != nil {
		return err
	}
	for _, user := range users {
		var externalID string
		for attempt := 0; attempt < 5; attempt++ {
			generatedID, err := identityschema.GenerateExternalUserID()
			if err != nil {
				return err
			}
			var existing int64
			if err := tx.Unscoped().Model(&identityschema.User{}).Where("external_id = ?", generatedID).Count(&existing).Error; err != nil {
				return err
			}
			if existing == 0 {
				externalID = generatedID
				break
			}
		}
		if externalID == "" {
			return fmt.Errorf("could not allocate a unique external user id")
		}
		if err := tx.Unscoped().Model(&identityschema.User{}).Where("id = ?", user.Id).Update("external_id", externalID).Error; err != nil {
			return err
		}
	}
	if tx.Migrator().HasIndex(&identityschema.User{}, "idx_users_external_id") {
		return nil
	}
	return tx.Migrator().CreateIndex(&identityschema.User{}, "idx_users_external_id")
}

func migrateSubscriptionCore(tx *gorm.DB) error {
	if !platformdb.UsingSQLite {
		return tx.AutoMigrate(
			&commerceschema.SubscriptionPlan{},
			&commerceschema.SubscriptionOrder{},
			&commerceschema.UserSubscription{},
			&commerceschema.SubscriptionPreConsumeRecord{},
		)
	}
	if err := migrateSubscriptionPlan(tx); err != nil {
		return err
	}
	if err := migrateSubscriptionOrder(tx); err != nil {
		return err
	}
	if err := migrateUserSubscription(tx); err != nil {
		return err
	}
	return tx.AutoMigrate(&commerceschema.SubscriptionPreConsumeRecord{})
}

func migrateSubscriptionPlan(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&commerceschema.SubscriptionPlan{}) {
		return tx.AutoMigrate(&commerceschema.SubscriptionPlan{})
	}
	return ensureSubscriptionPlanTableSQLiteTx(tx)
}

func migrateSubscriptionOrder(tx *gorm.DB) error {
	if !platformdb.UsingSQLite || !tx.Migrator().HasTable(&commerceschema.SubscriptionOrder{}) {
		return tx.AutoMigrate(&commerceschema.SubscriptionOrder{})
	}
	return ensureSubscriptionOrderTableSQLiteTx(tx)
}

func migrateUserSubscription(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&commerceschema.UserSubscription{}) {
		return tx.AutoMigrate(&commerceschema.UserSubscription{})
	}
	return ensureUserSubscriptionTableSQLiteTx(tx)
}

func migrateDailyLuckyNumber(tx *gorm.DB) error {
	if !platformdb.UsingSQLite {
		if err := addDailyLuckyNumberColumns(tx); err != nil {
			return err
		}
		return tx.AutoMigrate(
			&commerceschema.SubscriptionLuckyNumber{},
			&commerceschema.SubscriptionLuckyDraw{},
			&commerceschema.SubscriptionLuckyReward{},
			&commerceschema.SubscriptionLuckyRewardNotification{},
			&commerceschema.SubscriptionBlindBoxBenefitCycle{},
		)
	}
	if err := migrateSubscriptionPlan(tx); err != nil {
		return err
	}
	if err := migrateUserSubscription(tx); err != nil {
		return err
	}
	if err := tx.AutoMigrate(&commerceschema.BlindBoxOrder{}); err != nil {
		return err
	}
	return tx.AutoMigrate(
		&commerceschema.SubscriptionLuckyNumber{},
		&commerceschema.SubscriptionLuckyDraw{},
		&commerceschema.SubscriptionLuckyReward{},
		&commerceschema.SubscriptionLuckyRewardNotification{},
		&commerceschema.SubscriptionBlindBoxBenefitCycle{},
	)
}

// addDailyLuckyNumberColumns only appends the fields introduced by this
// migration. Running AutoMigrate on established PostgreSQL tables can issue
// type-changing DDL and then reuse an invalid prepared SELECT plan.
func addDailyLuckyNumberColumns(tx *gorm.DB) error {
	columns := []struct {
		model  interface{}
		fields []string
	}{
		{&commerceschema.SubscriptionPlan{}, []string{"MembershipTier", "LuckyDrawEnabled", "BlindBoxBenefitCount"}},
		{&commerceschema.UserSubscription{}, []string{"LuckyBenefitCycle"}},
		{&commerceschema.BlindBoxOrder{}, []string{"UserSubscriptionId", "BenefitCycle", "ExpiresAt"}},
	}
	for _, entry := range columns {
		for _, field := range entry.fields {
			if tx.Migrator().HasColumn(entry.model, field) {
				continue
			}
			if err := tx.Migrator().AddColumn(entry.model, field); err != nil {
				return err
			}
		}
	}
	for _, statement := range []string{
		"CREATE INDEX IF NOT EXISTS idx_subscription_plans_membership_tier ON subscription_plans (membership_tier)",
		"CREATE INDEX IF NOT EXISTS idx_subscription_plans_lucky_draw_enabled ON subscription_plans (lucky_draw_enabled)",
		"CREATE INDEX IF NOT EXISTS idx_user_subscriptions_lucky_benefit_cycle ON user_subscriptions (lucky_benefit_cycle)",
		"CREATE INDEX IF NOT EXISTS idx_blind_box_orders_user_subscription_id ON blind_box_orders (user_subscription_id)",
		"CREATE INDEX IF NOT EXISTS idx_blind_box_orders_benefit_cycle ON blind_box_orders (benefit_cycle)",
		"CREATE INDEX IF NOT EXISTS idx_blind_box_orders_expires_at ON blind_box_orders (expires_at)",
	} {
		if err := tx.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}
