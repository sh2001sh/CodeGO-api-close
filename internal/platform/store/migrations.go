package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	luckysettings "github.com/sh2001sh/new-api/internal/commerce/luckysettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	communityschema "github.com/sh2001sh/new-api/internal/community/schema"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
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

type legacyMarketplaceModelFeedback struct {
	ID        uint64 `gorm:"primaryKey;autoIncrement"`
	ChannelID string `gorm:"column:channel_id;uniqueIndex:uq_marketplace_model_feedback,priority:1"`
	UserID    int    `gorm:"column:user_id;uniqueIndex:uq_marketplace_model_feedback,priority:2"`
	Model     string `gorm:"column:model;uniqueIndex:uq_marketplace_model_feedback,priority:3"`
	Status    string `gorm:"column:status"`
}

func (legacyMarketplaceModelFeedback) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "marketplace.model_consistency_feedback"
	}
	return "marketplace_model_consistency_feedback"
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
		"20260721_blind_box_zero_hour",
		"20260724_gateway_route_pools",
		"20260724_gateway_route_pool_auto_discovery",
		"20260724_billing_funding_attribution",
		"20260801_daily_lucky_number",
		"20260802_gateway_route_pool_fault_domains",
		"20260804_daily_lucky_reward_notifications",
		"20260805_billing_outbox_pending_lookup",
		"20260808_commerce_invoice_requests",
		"20260812_blind_box_daily_lucky_numbers",
		"20260813_balance_blind_box",
		"20260813_balance_blind_box_small_pity",
		"20260813_blind_box_lucky_draw_window",
		"20260814_wallet_transfers",
		"20260814_balance_blind_box_inventory",
		"20260815_blind_box_legacy_credit_marker",
		"20260815_remove_bounty_market",
		"20260815_marketplace_channel_source_labels",
		"20260815_marketplace_model_verification",
		"20260815_marketplace_auto_route_pool",
		"20260815_marketplace_soft_delete",
		"20260815_marketplace_numeric_channel_ids",
		"20260816_wallet_transfer_fee_fields",
		"20260816_marketplace_incremental_channel_ids",
		"20260817_marketplace_gpt56_mapping_detection",
		"20260817_marketplace_channel_connectivity_test",
		"20260817_marketplace_channel_auto_probe",
		"20260816_token_marketplace_multiplier_limit",
		"20260816_unified_credit_v1_schema",
		"20260816_unified_credit_v1_channel_scope",
		"20260816_subscription_claude_conversion_fields",
		"20260816_daily_lucky_unified_credit_rewards",
		"20260816_blind_box_prop_gifts",
		"20260816_marketplace_model_consistency_feedback",
		"20260816_marketplace_channel_feedback_and_prices",
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
			return nil
		}},
		{ID: "20260713_bounty_market_followups", Run: func(tx *gorm.DB) error {
			return nil
		}},
		{ID: "20260713_bounty_delivery_summary", Run: func(tx *gorm.DB) error {
			return nil
		}},
		{ID: "20260713_bounty_submission_version_index", Run: func(tx *gorm.DB) error {
			return nil
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
		{ID: "20260813_balance_blind_box", Run: migrateBalanceBlindBox},
		{ID: "20260813_balance_blind_box_small_pity", Run: migrateBalanceBlindBoxSmallPity},
		{ID: "20260813_blind_box_lucky_draw_window", Run: migrateBlindBoxLuckyDrawWindow},
		{ID: "20260814_wallet_transfers", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&commerceschema.WalletTransferSecurity{}, &commerceschema.WalletTransfer{})
		}},
		{ID: "20260814_balance_blind_box_inventory", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(
				&commerceschema.BalanceBlindBoxPurchase{},
				&commerceschema.BalanceBlindBoxItem{},
				&commerceschema.BalanceBlindBoxGift{},
				&commerceschema.BalanceBlindBoxGiftItem{},
			)
		}},
		{ID: "20260815_blind_box_legacy_credit_marker", Run: migrateBlindBoxLegacyCreditMarker},
		{ID: "20260815_remove_bounty_market", Run: migrateRemoveBountyMarket},
		{ID: "20260815_marketplace_channel_source_labels", Run: migrateMarketplaceChannelSourceLabels},
		{ID: "20260815_marketplace_model_verification", Run: migrateMarketplaceModelVerification},
		{ID: "20260815_marketplace_auto_route_pool", Run: migrateMarketplaceAutoRoutePool},
		{ID: "20260815_marketplace_soft_delete", Run: migrateMarketplaceSoftDelete},
		{ID: "20260815_marketplace_numeric_channel_ids", Run: migrateMarketplaceNumericChannelIDs},
		{ID: "20260816_wallet_transfer_fee_fields", Run: migrateWalletTransferFeeFields},
		{ID: "20260816_marketplace_incremental_channel_ids", Run: migrateMarketplaceIncrementalChannelIDs},
		{ID: "20260817_marketplace_gpt56_mapping_detection", Run: migrateMarketplaceGPT56MappingDetection},
		{ID: "20260817_marketplace_channel_connectivity_test", Run: migrateMarketplaceChannelConnectivityTest},
		{ID: "20260817_marketplace_channel_auto_probe", Run: migrateMarketplaceChannelAutoProbe},
		{ID: "20260816_token_marketplace_multiplier_limit", Run: migrateTokenMarketplaceMultiplierLimit},
		{ID: "20260816_unified_credit_v1_schema", Run: migrateUnifiedCreditV1Schema},
		{ID: "20260816_unified_credit_v1_channel_scope", Run: migrateUnifiedCreditV1ChannelScope},
		{ID: "20260816_subscription_claude_conversion_fields", Run: migrateSubscriptionClaudeConversionFields},
		{ID: "20260816_daily_lucky_unified_credit_rewards", Run: migrateDailyLuckyUnifiedCreditRewards},
		{ID: "20260816_blind_box_prop_gifts", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&commerceschema.BlindBoxPropGift{})
		}},
		{ID: "20260816_marketplace_model_consistency_feedback", Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&legacyMarketplaceModelFeedback{})
		}},
		{ID: "20260816_marketplace_channel_feedback_and_prices", Run: migrateMarketplaceChannelFeedbackAndPrices},
	}
	for _, step := range steps {
		var applied schemaMigration
		err := db.Where("id = ?", step.ID).First(&applied).Error
		if err == nil {
			if appliedMigrationNeedsRepair(db, step.ID) {
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

func migrateUnifiedCreditV1Schema(tx *gorm.DB) error {
	if err := tx.AutoMigrate(
		&commerceschema.UnifiedCreditUserMigration{},
		&commerceschema.SubscriptionTierSettlement{},
		&commerceschema.UnifiedCreditGroupRatioMigration{},
	); err != nil {
		return err
	}
	for _, target := range []struct {
		model any
		field string
	}{
		{model: &commerceschema.UserSubscription{}, field: "MembershipTier"},
		{model: &commerceschema.BlindBoxProp{}, field: "MaxDiscountQuota"},
		{model: &commerceschema.BlindBoxProp{}, field: "UsedDiscountQuota"},
	} {
		if !tx.Migrator().HasTable(target.model) || tx.Migrator().HasColumn(target.model, target.field) {
			continue
		}
		if err := tx.Migrator().AddColumn(target.model, target.field); err != nil {
			return err
		}
	}
	if tx.Migrator().HasTable(&commerceschema.BlindBoxProp{}) {
		if err := tx.Model(&commerceschema.BlindBoxProp{}).
			Where("prop_type = ? AND duration_seconds = ?", commerceschema.BlindBoxPropTypeMonthlyPassMultiplier, 0).
			Update("duration_seconds", int64(15*60)).Error; err != nil {
			return err
		}
		if err := tx.Model(&commerceschema.BlindBoxProp{}).
			Where("prop_type = ?", commerceschema.BlindBoxPropTypeMonthlyPassMultiplier).
			Update("title", "0.10 倍率体验卡").Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateUnifiedCreditV1ChannelScope(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&commerceschema.BlindBoxPropDiscountUsage{}) {
		if err := tx.AutoMigrate(&commerceschema.BlindBoxPropDiscountUsage{}); err != nil {
			return err
		}
	}
	if !tx.Migrator().HasTable(&gatewayschema.Channel{}) {
		return nil
	}
	if !tx.Migrator().HasColumn(&gatewayschema.Channel{}, "ChannelScope") {
		if err := tx.Migrator().AddColumn(&gatewayschema.Channel{}, "ChannelScope"); err != nil {
			return err
		}
	}
	if err := tx.Model(&gatewayschema.Channel{}).
		Where("channel_scope = '' OR channel_scope IS NULL").
		Update("channel_scope", gatewayschema.ChannelScopeOfficial).Error; err != nil {
		return err
	}
	if !tx.Migrator().HasColumn(&gatewayschema.Channel{}, "SensitiveWordInterceptionEnabled") {
		if err := tx.Migrator().AddColumn(&gatewayschema.Channel{}, "SensitiveWordInterceptionEnabled"); err != nil {
			return err
		}
	}
	if !tx.Migrator().HasColumn(&gatewayschema.Channel{}, "MarketplaceMaxConcurrency") {
		if err := tx.Migrator().AddColumn(&gatewayschema.Channel{}, "MarketplaceMaxConcurrency"); err != nil {
			return err
		}
	}
	if err := tx.Model(&gatewayschema.Channel{}).
		Where("sensitive_word_interception_enabled IS NULL").
		Update("sensitive_word_interception_enabled", true).Error; err != nil {
		return err
	}
	if !tx.Migrator().HasTable(&marketplaceschema.Channel{}) {
		return nil
	}
	var marketplaceChannelIDs []int
	if err := tx.Model(&marketplaceschema.Channel{}).
		Where("internal_channel_id IS NOT NULL").
		Pluck("internal_channel_id", &marketplaceChannelIDs).Error; err != nil {
		return err
	}
	if len(marketplaceChannelIDs) == 0 {
		return nil
	}
	return tx.Model(&gatewayschema.Channel{}).
		Where("id IN ?", marketplaceChannelIDs).
		Update("channel_scope", gatewayschema.ChannelScopeExternal).Error
}

// migrateDailyLuckyUnifiedCreditRewards scales only the historical defaults.
// Custom operator settings are left untouched, while a deployment that still
// has the old 1/10/50/100 ladder is moved to the smaller unified-credit ladder.
func migrateDailyLuckyUnifiedCreditRewards(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("options") {
		return nil
	}
	updates := []struct {
		key     string
		legacy  []string
		revised string
	}{
		{key: "daily_lucky_number_setting.base_reward_1_usd", legacy: []string{"1", "1.0", "1.00"}, revised: "0.25"},
		{key: "daily_lucky_number_setting.base_reward_2_usd", legacy: []string{"10", "10.0", "10.00"}, revised: "2.5"},
		{key: "daily_lucky_number_setting.base_reward_3_usd", legacy: []string{"50", "50.0", "50.00"}, revised: "12.5"},
		{key: "daily_lucky_number_setting.base_reward_4_usd", legacy: []string{"100", "100.0", "100.00"}, revised: "25"},
		{key: "daily_lucky_number_setting.jackpot_initial_usd", legacy: []string{"100", "100.0", "100.00"}, revised: "25"},
		{key: "daily_lucky_number_setting.jackpot_increment_usd", legacy: []string{"20", "20.0", "20.00"}, revised: "5"},
		{key: "daily_lucky_number_setting.jackpot_cap_usd", legacy: []string{"1000", "1000.0", "1000.00"}, revised: "250"},
	}
	for _, item := range updates {
		if err := tx.Table("options").Where("key = ? AND value IN ?", item.key, item.legacy).Update("value", item.revised).Error; err != nil {
			return err
		}
	}
	return nil
}

func appliedMigrationNeedsRepair(db *gorm.DB, migrationID string) bool {
	switch migrationID {
	case "20260715_blind_box_admin_grants":
		return !db.Migrator().HasTable(&commerceschema.BlindBoxOrder{}) ||
			!db.Migrator().HasTable(&commerceschema.BlindBoxGrant{})
	case "20260815_marketplace_auto_route_pool":
		return !db.Migrator().HasTable(&marketplaceschema.AutoRoutePoolMember{}) ||
			!db.Migrator().HasColumn(&marketplaceschema.AutoRoutePoolMember{}, "Priority")
	case "20260815_marketplace_channel_source_labels":
		return !db.Migrator().HasTable(&marketplaceschema.RankingSnapshot{}) ||
			!db.Migrator().HasColumn(&marketplaceschema.RankingSnapshot{}, "CacheHitRate")
	case "20260817_marketplace_gpt56_mapping_detection":
		return db.Migrator().HasTable(&marketplaceschema.Channel{}) &&
			(!db.Migrator().HasColumn(&marketplaceschema.Channel{}, "GPT56MappingResults") ||
				!db.Migrator().HasColumn(&marketplaceschema.Channel{}, "GPT56MappingStatus") ||
				!db.Migrator().HasColumn(&marketplaceschema.Channel{}, "GPT56MappingCheckedAt"))
	case "20260817_marketplace_channel_connectivity_test":
		return marketplaceChannelSchemaNeedsRepair(db,
			[]string{"ConnectivityTestStatus", "ConnectivityTestCheckedAt"},
			[]string{"ConnectivityTestStatus", "ConnectivityTestCheckedAt"},
		)
	case "20260817_marketplace_channel_auto_probe":
		return marketplaceChannelSchemaNeedsRepair(db,
			[]string{"AutoProbeEnabled", "AutoProbeIntervalMinutes", "AutoProbeModel", "AutoProbeLastStatus", "AutoProbeLastAt"},
			[]string{"AutoProbeEnabled", "AutoProbeLastStatus", "AutoProbeLastAt"},
		)
	case "20260816_unified_credit_v1_channel_scope":
		return !db.Migrator().HasTable(&commerceschema.BlindBoxPropDiscountUsage{}) ||
			db.Migrator().HasTable(&gatewayschema.Channel{}) &&
				(!db.Migrator().HasColumn(&gatewayschema.Channel{}, "ChannelScope") ||
					!db.Migrator().HasColumn(&gatewayschema.Channel{}, "SensitiveWordInterceptionEnabled") ||
					!db.Migrator().HasColumn(&gatewayschema.Channel{}, "MarketplaceMaxConcurrency"))
	case "20260816_subscription_claude_conversion_fields":
		return !db.Migrator().HasTable(&commerceschema.SubscriptionClaudeConversion{}) ||
			!db.Migrator().HasColumn(&commerceschema.SubscriptionClaudeConversion{}, "PlanPriceAmount") ||
			!db.Migrator().HasColumn(&commerceschema.SubscriptionClaudeConversion{}, "UnusedRatio") ||
			!db.Migrator().HasColumn(&commerceschema.SubscriptionClaudeConversion{}, "ConversionPercent")
	case "20260816_marketplace_channel_feedback_and_prices":
		return !db.Migrator().HasTable(&marketplaceschema.ChannelFeedback{}) ||
			db.Migrator().HasTable(&marketplaceschema.Channel{}) &&
				(!db.Migrator().HasColumn(&marketplaceschema.Channel{}, "ModelPrices") ||
					!db.Migrator().HasColumn(&marketplaceschema.Channel{}, "SensitiveWordInterceptionEnabled"))
	default:
		return false
	}
}

func marketplaceChannelSchemaNeedsRepair(db *gorm.DB, fields, indexes []string) bool {
	if !db.Migrator().HasTable(&marketplaceschema.Channel{}) {
		return false
	}
	for _, field := range fields {
		if !db.Migrator().HasColumn(&marketplaceschema.Channel{}, field) {
			return true
		}
	}
	for _, index := range indexes {
		if !db.Migrator().HasIndex(&marketplaceschema.Channel{}, index) {
			return true
		}
	}
	return false
}

func migrateMarketplaceChannelFeedbackAndPrices(db *gorm.DB) error {
	if db.Migrator().HasTable(&marketplaceschema.Channel{}) {
		for _, field := range []string{"ModelPrices", "SensitiveWordInterceptionEnabled"} {
			if db.Migrator().HasColumn(&marketplaceschema.Channel{}, field) {
				continue
			}
			if err := db.Migrator().AddColumn(&marketplaceschema.Channel{}, field); err != nil {
				return err
			}
		}
	}
	if err := db.AutoMigrate(&marketplaceschema.ChannelFeedback{}); err != nil {
		return err
	}
	legacy := legacyMarketplaceModelFeedback{}
	if !db.Migrator().HasTable(&legacy) {
		return nil
	}
	var rows []legacyMarketplaceModelFeedback
	if err := db.Find(&rows).Error; err != nil {
		return err
	}
	type key struct {
		ChannelID string
		UserID    int
	}
	merged := make(map[key]string)
	for _, row := range rows {
		itemKey := key{ChannelID: row.ChannelID, UserID: row.UserID}
		if current, exists := merged[itemKey]; exists && current != row.Status {
			merged[itemKey] = "questionable"
		} else if !exists {
			merged[itemKey] = row.Status
		}
	}
	for itemKey, status := range merged {
		feedback := marketplaceschema.ChannelFeedback{ChannelID: itemKey.ChannelID, UserID: itemKey.UserID, Status: status}
		if err := db.Create(&feedback).Error; err != nil {
			return err
		}
	}
	return db.Migrator().DropTable(&legacy)
}

func migrateBlindBoxLegacyCreditMarker(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&commerceschema.BlindBoxCredit{}) ||
		tx.Migrator().HasColumn(&commerceschema.BlindBoxCredit{}, "MigratedAt") {
		return nil
	}
	return tx.Migrator().AddColumn(&commerceschema.BlindBoxCredit{}, "MigratedAt")
}

func migrateMarketplaceChannelSourceLabels(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&marketplaceschema.Channel{}) {
		return tx.AutoMigrate(
			&marketplaceschema.Channel{},
			&marketplaceschema.Group{},
			&marketplaceschema.VerificationRun{},
			&marketplaceschema.RankingSnapshot{},
			&marketplaceschema.Settlement{},
		)
	}
	for _, field := range []string{
		"SubmittedSourceLabel",
		"ApprovedSourceLabel",
		"SourceLabelStatus",
		"SourceLabelReviewReason",
	} {
		if tx.Migrator().HasColumn(&marketplaceschema.Channel{}, field) {
			continue
		}
		if err := tx.Migrator().AddColumn(&marketplaceschema.Channel{}, field); err != nil {
			return err
		}
	}
	return tx.AutoMigrate(
		&marketplaceschema.Group{},
		&marketplaceschema.VerificationRun{},
		&marketplaceschema.RankingSnapshot{},
		&marketplaceschema.Settlement{},
	)
}

func migrateMarketplaceModelVerification(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&marketplaceschema.Channel{}) {
		return tx.AutoMigrate(&marketplaceschema.Channel{})
	}
	for _, field := range []string{"ModelVerificationResults", "ModelConsistencyStatus"} {
		if tx.Migrator().HasColumn(&marketplaceschema.Channel{}, field) {
			continue
		}
		if err := tx.Migrator().AddColumn(&marketplaceschema.Channel{}, field); err != nil {
			return err
		}
	}
	return nil
}

func migrateMarketplaceGPT56MappingDetection(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&marketplaceschema.Channel{}) {
		return tx.AutoMigrate(&marketplaceschema.Channel{})
	}
	for _, field := range []string{"GPT56MappingResults", "GPT56MappingStatus", "GPT56MappingCheckedAt"} {
		if tx.Migrator().HasColumn(&marketplaceschema.Channel{}, field) {
			continue
		}
		if err := tx.Migrator().AddColumn(&marketplaceschema.Channel{}, field); err != nil {
			return err
		}
	}
	return nil
}

func migrateMarketplaceChannelConnectivityTest(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&marketplaceschema.Channel{}) {
		return tx.AutoMigrate(&marketplaceschema.Channel{})
	}
	for _, field := range []string{"ConnectivityTestStatus", "ConnectivityTestCheckedAt"} {
		if tx.Migrator().HasColumn(&marketplaceschema.Channel{}, field) {
			continue
		}
		if err := tx.Migrator().AddColumn(&marketplaceschema.Channel{}, field); err != nil {
			return err
		}
	}
	return ensureMarketplaceChannelIndexes(tx, "ConnectivityTestStatus", "ConnectivityTestCheckedAt")
}

func migrateMarketplaceChannelAutoProbe(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&marketplaceschema.Channel{}) {
		return tx.AutoMigrate(&marketplaceschema.Channel{})
	}
	for _, field := range []string{
		"AutoProbeEnabled",
		"AutoProbeIntervalMinutes",
		"AutoProbeModel",
		"AutoProbeLastStatus",
		"AutoProbeLastAt",
	} {
		if tx.Migrator().HasColumn(&marketplaceschema.Channel{}, field) {
			continue
		}
		if err := tx.Migrator().AddColumn(&marketplaceschema.Channel{}, field); err != nil {
			return err
		}
	}
	return ensureMarketplaceChannelIndexes(tx, "AutoProbeEnabled", "AutoProbeLastStatus", "AutoProbeLastAt")
}

func ensureMarketplaceChannelIndexes(tx *gorm.DB, fields ...string) error {
	for _, field := range fields {
		if tx.Migrator().HasIndex(&marketplaceschema.Channel{}, field) {
			continue
		}
		if err := tx.Migrator().CreateIndex(&marketplaceschema.Channel{}, field); err != nil {
			return err
		}
	}
	return nil
}

func migrateMarketplaceAutoRoutePool(tx *gorm.DB) error {
	return tx.AutoMigrate(&marketplaceschema.AutoRoutePoolMember{})
}

func migrateWalletTransferFeeFields(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&commerceschema.WalletTransfer{}) {
		return tx.AutoMigrate(&commerceschema.WalletTransfer{})
	}
	for _, field := range []string{"FeeQuota", "TotalDebitQuota"} {
		if tx.Migrator().HasColumn(&commerceschema.WalletTransfer{}, field) {
			continue
		}
		if err := tx.Migrator().AddColumn(&commerceschema.WalletTransfer{}, field); err != nil {
			return err
		}
	}
	return tx.Model(&commerceschema.WalletTransfer{}).
		Where("total_debit_quota = ?", 0).
		Update("total_debit_quota", gorm.Expr("amount_quota")).Error
}

func migrateMarketplaceSoftDelete(tx *gorm.DB) error {
	for _, target := range []struct {
		model any
		field string
	}{
		{model: &marketplaceschema.Channel{}, field: "DeletedAt"},
		{model: &marketplaceschema.Group{}, field: "DeletedAt"},
	} {
		if !tx.Migrator().HasTable(target.model) || tx.Migrator().HasColumn(target.model, target.field) {
			continue
		}
		if err := tx.Migrator().AddColumn(target.model, target.field); err != nil {
			return err
		}
	}
	return nil
}

func migrateMarketplaceNumericChannelIDs(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&marketplaceschema.Channel{}) {
		return nil
	}
	var channels []marketplaceschema.Channel
	if err := tx.Unscoped().Order("created_at asc").Find(&channels).Error; err != nil {
		return err
	}
	used := make(map[string]struct{}, len(channels))
	for _, channel := range channels {
		if isNumericMarketplaceID(channel.ID) {
			used[channel.ID] = struct{}{}
		}
	}
	nextID := int64(100_000_000_000)
	for index := range channels {
		channel := &channels[index]
		if isNumericMarketplaceID(channel.ID) {
			continue
		}
		newID := nextNumericMarketplaceID(used, &nextID)
		if err := replaceMarketplaceChannelID(tx, channel, newID); err != nil {
			return err
		}
		used[newID] = struct{}{}
	}
	return nil
}

func migrateMarketplaceIncrementalChannelIDs(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&marketplaceschema.Channel{}) {
		return nil
	}
	if err := tx.AutoMigrate(&marketplaceschema.ChannelIDSequence{}); err != nil {
		return err
	}
	var sequenceCount int64
	if err := tx.Model(&marketplaceschema.ChannelIDSequence{}).Count(&sequenceCount).Error; err != nil {
		return err
	}
	if sequenceCount > 0 {
		return nil
	}

	var channels []marketplaceschema.Channel
	if err := tx.Unscoped().Order("created_at asc, id asc").Find(&channels).Error; err != nil {
		return err
	}
	used := make(map[string]struct{}, len(channels))
	for _, channel := range channels {
		used[channel.ID] = struct{}{}
	}
	for index := range channels {
		channel := &channels[index]
		temporaryID := nextMarketplaceTemporaryID(used, index+1)
		if err := replaceMarketplaceChannelID(tx, channel, temporaryID); err != nil {
			return err
		}
		delete(used, channel.ID)
		used[temporaryID] = struct{}{}
		channel.ID = temporaryID
	}
	for index := range channels {
		channel := &channels[index]
		incrementalID := strconv.Itoa(index + 1)
		if err := replaceMarketplaceChannelID(tx, channel, incrementalID); err != nil {
			return err
		}
		channel.ID = incrementalID
		if err := tx.Create(&marketplaceschema.ChannelIDSequence{}).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateTokenMarketplaceMultiplierLimit(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&identityschema.Token{}) || tx.Migrator().HasColumn(&identityschema.Token{}, "MarketplaceMultiplierLimit") {
		return nil
	}
	return tx.Migrator().AddColumn(&identityschema.Token{}, "MarketplaceMultiplierLimit")
}

func nextMarketplaceTemporaryID(used map[string]struct{}, ordinal int) string {
	candidate := "__marketplace_id_tmp_" + strconv.Itoa(ordinal)
	for {
		if _, exists := used[candidate]; !exists {
			return candidate
		}
		candidate += "_"
	}
}

func isNumericMarketplaceID(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func nextNumericMarketplaceID(used map[string]struct{}, nextID *int64) string {
	for {
		(*nextID)++
		candidate := strconv.FormatInt(*nextID, 10)
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
}

func replaceMarketplaceChannelID(tx *gorm.DB, channel *marketplaceschema.Channel, newID string) error {
	oldID := channel.ID
	if tx.Migrator().HasTable(&marketplaceschema.Group{}) {
		if err := tx.Unscoped().Model(&marketplaceschema.Group{}).Where("channel_id = ?", oldID).Update("channel_id", newID).Error; err != nil {
			return err
		}
	}
	if tx.Migrator().HasTable(&marketplaceschema.VerificationRun{}) {
		if err := tx.Model(&marketplaceschema.VerificationRun{}).Where("channel_id = ?", oldID).Update("channel_id", newID).Error; err != nil {
			return err
		}
	}
	if err := updateMarketplaceInternalChannelMetadata(tx, channel, newID); err != nil {
		return err
	}
	return tx.Unscoped().Model(&marketplaceschema.Channel{}).Where("id = ?", oldID).Update("id", newID).Error
}

func updateMarketplaceInternalChannelMetadata(tx *gorm.DB, channel *marketplaceschema.Channel, newID string) error {
	if channel.InternalChannelID == nil {
		return nil
	}
	var internal gatewayschema.Channel
	err := tx.First(&internal, "id = ?", *channel.InternalChannelID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	metadata := make(map[string]any)
	if json.Unmarshal([]byte(strings.TrimSpace(internal.OtherInfo)), &metadata) != nil {
		metadata = make(map[string]any)
	}
	metadata["marketplace_channel_id"] = newID
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return tx.Model(&internal).Update("other_info", string(encoded)).Error
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

func migrateSubscriptionClaudeConversionFields(tx *gorm.DB) error {
	model := &commerceschema.SubscriptionClaudeConversion{}
	isSQLite := tx.Dialector != nil && tx.Dialector.Name() == "sqlite"
	if !tx.Migrator().HasTable(model) || !isSQLite {
		return tx.AutoMigrate(model)
	}

	columns := []struct {
		name string
		ddl  string
	}{
		{name: "plan_price_amount", ddl: `"plan_price_amount" REAL NOT NULL DEFAULT 0`},
		{name: "unused_ratio", ddl: `"unused_ratio" REAL NOT NULL DEFAULT 0`},
		{name: "conversion_percent", ddl: `"conversion_percent" INTEGER NOT NULL DEFAULT 0`},
	}
	for _, column := range columns {
		if tx.Migrator().HasColumn(model, column.name) {
			continue
		}
		if err := tx.Exec("ALTER TABLE subscription_claude_conversions ADD COLUMN " + column.ddl).Error; err != nil {
			return err
		}
	}
	return nil
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

// migrateBalanceBlindBox appends the fields required by the wallet-funded
// blind-box pool without asking PostgreSQL to rebuild the established records table.
func migrateBalanceBlindBox(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&commerceschema.BlindBoxOpenRecord{}) {
		return nil
	}
	for _, field := range []string{"PoolType", "RequestId"} {
		if tx.Migrator().HasColumn(&commerceschema.BlindBoxOpenRecord{}, field) {
			continue
		}
		if err := tx.Migrator().AddColumn(&commerceschema.BlindBoxOpenRecord{}, field); err != nil {
			return err
		}
	}
	if err := tx.AutoMigrate(&commerceschema.BalanceBlindBoxPityState{}); err != nil {
		return err
	}
	legacyColumn := "consecutive_under_35_usd"
	currentColumn := "consecutive_under35_usd"
	if tx.Migrator().HasColumn(&commerceschema.BalanceBlindBoxPityState{}, legacyColumn) &&
		!tx.Migrator().HasColumn(&commerceschema.BalanceBlindBoxPityState{}, currentColumn) {
		if err := tx.Migrator().RenameColumn(&commerceschema.BalanceBlindBoxPityState{}, legacyColumn, currentColumn); err != nil {
			return err
		}
	}
	return tx.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_blind_box_open_records_request_id ON blind_box_open_records (request_id) WHERE request_id IS NOT NULL").Error
}

func migrateBalanceBlindBoxSmallPity(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&commerceschema.BalanceBlindBoxPityState{}) ||
		tx.Migrator().HasColumn(&commerceschema.BalanceBlindBoxPityState{}, "ConsecutiveUnder6USD") {
		return nil
	}
	return tx.Migrator().AddColumn(&commerceschema.BalanceBlindBoxPityState{}, "ConsecutiveUnder6USD")
}

func migrateBlindBoxLuckyDrawWindow(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&commerceschema.BlindBoxDailyLuckyNumber{}) {
		return nil
	}
	setting := luckysettings.Get()
	location, err := setting.Location()
	if err != nil {
		return err
	}
	now := time.Now().In(location)
	drawAt := time.Date(
		now.Year(), now.Month(), now.Day(),
		setting.DrawHour, setting.DrawMinute, 0, 0, location,
	)
	if !now.Before(drawAt) {
		drawAt = drawAt.AddDate(0, 0, 1)
	}
	windowStart := drawAt.AddDate(0, 0, -1)
	return tx.Model(&commerceschema.BlindBoxDailyLuckyNumber{}).
		Where("created_at >= ? AND created_at < ?", windowStart.Unix(), drawAt.Unix()).
		Updates(map[string]interface{}{
			"draw_date":  drawAt.Format("2006-01-02"),
			"expires_at": drawAt.Unix(),
		}).Error
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
