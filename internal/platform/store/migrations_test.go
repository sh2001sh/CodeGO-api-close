package store

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformschema "github.com/sh2001sh/new-api/internal/platform/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type legacyUserForExternalIDMigration struct {
	Id        int `gorm:"primaryKey"`
	Username  string
	DeletedAt gorm.DeletedAt
}

type legacyWalletTransferForFeeFieldsMigration struct {
	Id              int `gorm:"primaryKey"`
	AmountQuota     int64
	SenderBalanceAt int64
}

func (legacyWalletTransferForFeeFieldsMigration) TableName() string {
	return "wallet_transfers"
}

func (legacyUserForExternalIDMigration) TableName() string {
	return "users"
}

func TestApplyV2MigrationsIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	originalDB := platformdb.DB
	originalSQLite := platformdb.UsingSQLite
	originalPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalSQLite
		platformdb.UsingPostgreSQL = originalPostgreSQL
	})
	platformdb.DB = db
	platformdb.UsingSQLite = true
	platformdb.UsingPostgreSQL = false
	for _, tableName := range []string{
		"user_companion_pets",
		"daily_mission_rewards",
		"achievement_unlocks",
	} {
		require.NoError(t, db.Exec("CREATE TABLE "+tableName+" (id integer primary key)").Error)
	}

	require.NoError(t, ApplyV2Migrations(context.Background(), false))
	require.NoError(t, ApplyV2Migrations(context.Background(), false))

	var migrationCount int64
	require.NoError(t, db.Model(&schemaMigration{}).Count(&migrationCount).Error)
	require.Equal(t, int64(len(V2MigrationIDs())), migrationCount)
	for _, table := range []string{
		"billing_outbox_events",
		"billing_funding_source_policies",
		"billing_funding_lots",
		"billing_funding_allocations",
		"billing_request_economics",
		"workflow_task_workflows",
		"workflow_task_snapshots",
		"workflow_task_terminal_results",
		"subscription_plans",
		"subscription_orders",
		"user_subscriptions",
		"subscription_pre_consume_records",
		"gateway_request_executions",
		"gateway_route_plans",
		"gateway_execution_attempts",
		"gateway_usage_evidence",
		"route_pools",
		"route_pool_members",
		"blind_box_orders",
		"blind_box_grants",
	} {
		require.True(t, db.Migrator().HasTable(table), table)
	}
	require.NoError(t, db.Migrator().DropTable(&commerceschema.BlindBoxGrant{}))
	require.False(t, db.Migrator().HasTable(&commerceschema.BlindBoxGrant{}))
	require.NoError(t, ApplyV2Migrations(context.Background(), false))
	require.True(t, db.Migrator().HasTable(&commerceschema.BlindBoxGrant{}))
	require.NoError(t, db.Migrator().DropColumn(&marketplaceschema.AutoRoutePoolMember{}, "Priority"))
	require.False(t, db.Migrator().HasColumn(&marketplaceschema.AutoRoutePoolMember{}, "Priority"))
	require.NoError(t, ApplyV2Migrations(context.Background(), false))
	require.True(t, db.Migrator().HasColumn(&marketplaceschema.AutoRoutePoolMember{}, "Priority"))
	for _, tableName := range []string{
		"user_companion_pets",
		"daily_mission_rewards",
		"achievement_unlocks",
	} {
		require.False(t, db.Migrator().HasTable(tableName), tableName)
	}
}

func TestMigrateMarketplaceAutoRoutePoolIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)

	originalPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.UsingPostgreSQL = originalPostgreSQL
	})
	platformdb.UsingPostgreSQL = false

	require.NoError(t, migrateMarketplaceAutoRoutePool(db))
	require.NoError(t, migrateMarketplaceAutoRoutePool(db))
	require.True(t, db.Migrator().HasTable(&marketplaceschema.AutoRoutePoolMember{}))
	require.True(t, db.Migrator().HasIndex(&marketplaceschema.AutoRoutePoolMember{}, "uq_marketplace_auto_pool_member"))
	require.True(t, db.Migrator().HasColumn(&marketplaceschema.AutoRoutePoolMember{}, "Priority"))
}

func TestMigrateWalletTransferFeeFieldsAddsMissingColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&legacyWalletTransferForFeeFieldsMigration{}))
	require.NoError(t, db.Create(&legacyWalletTransferForFeeFieldsMigration{Id: 1, AmountQuota: 256}).Error)

	require.NoError(t, migrateWalletTransferFeeFields(db))
	require.True(t, db.Migrator().HasColumn(&commerceschema.WalletTransfer{}, "FeeQuota"))
	require.True(t, db.Migrator().HasColumn(&commerceschema.WalletTransfer{}, "TotalDebitQuota"))
	var totalDebitQuota int64
	require.NoError(t, db.Raw("SELECT total_debit_quota FROM wallet_transfers WHERE id = ?", 1).Scan(&totalDebitQuota).Error)
	require.EqualValues(t, 256, totalDebitQuota)
	require.NoError(t, migrateWalletTransferFeeFields(db))
}

func TestMigrateTokenMarketplaceMultiplierLimitAddsMissingColumn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE tokens (id integer primary key)`).Error)
	require.False(t, db.Migrator().HasColumn(&identityschema.Token{}, "MarketplaceMultiplierLimit"))

	require.NoError(t, migrateTokenMarketplaceMultiplierLimit(db))
	require.True(t, db.Migrator().HasColumn(&identityschema.Token{}, "MarketplaceMultiplierLimit"))
	require.NoError(t, migrateTokenMarketplaceMultiplierLimit(db))
}

func TestMigrateDailyLuckyUnifiedCreditRewardsOnlyScalesLegacyDefaults(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&platformschema.Option{}))
	require.NoError(t, db.Create(&[]platformschema.Option{
		{Key: "daily_lucky_number_setting.base_reward_1_usd", Value: "1"},
		{Key: "daily_lucky_number_setting.base_reward_2_usd", Value: "2.75"},
		{Key: "daily_lucky_number_setting.jackpot_cap_usd", Value: "1000.00"},
	}).Error)

	require.NoError(t, migrateDailyLuckyUnifiedCreditRewards(db))
	var revised platformschema.Option
	require.NoError(t, db.First(&revised, "key = ?", "daily_lucky_number_setting.base_reward_1_usd").Error)
	require.Equal(t, "0.25", revised.Value)
	var custom platformschema.Option
	require.NoError(t, db.First(&custom, "key = ?", "daily_lucky_number_setting.base_reward_2_usd").Error)
	require.Equal(t, "2.75", custom.Value)
}

func TestMigrateMarketplaceSoftDeleteAndNumericChannelIDs(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	originalPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() { platformdb.UsingPostgreSQL = originalPostgreSQL })
	platformdb.UsingPostgreSQL = false

	require.NoError(t, db.AutoMigrate(
		&gatewayschema.Channel{},
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.VerificationRun{},
	))
	internal := gatewayschema.Channel{Key: "encrypted", OtherInfo: `{"marketplace_channel_id":"legacy-uuid"}`}
	require.NoError(t, db.Create(&internal).Error)
	channel := marketplaceschema.Channel{
		ID: "legacy-uuid", OwnerUserID: 1, ProviderType: "openai_compatible",
		BaseURLCiphertext: "url", CredentialCiphertext: "key", InternalChannelID: &internal.Id,
	}
	group := marketplaceschema.Group{
		ID: "legacy-group", ChannelID: channel.ID, OwnerUserID: 1, PublicSlug: "legacy",
		SystemDisplayName: "legacy", InternalGroupName: "legacy", SourceType: "marketplace_user",
		CreditPoolPolicy: "universal_only", Multiplier: 1, LifecycleStatus: "active",
		VerificationStatus: "passed", Visibility: "public",
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)
	require.NoError(t, db.Create(&marketplaceschema.VerificationRun{ID: "legacy-run", ChannelID: channel.ID, Status: "passed"}).Error)

	require.NoError(t, migrateMarketplaceSoftDelete(db))
	require.NoError(t, migrateMarketplaceNumericChannelIDs(db))
	require.NoError(t, migrateMarketplaceIncrementalChannelIDs(db))

	var migrated marketplaceschema.Channel
	require.NoError(t, db.First(&migrated).Error)
	require.Equal(t, "1", migrated.ID)
	var migratedGroup marketplaceschema.Group
	require.NoError(t, db.First(&migratedGroup, "id = ?", group.ID).Error)
	require.Equal(t, migrated.ID, migratedGroup.ChannelID)
	var migratedRun marketplaceschema.VerificationRun
	require.NoError(t, db.First(&migratedRun, "id = ?", "legacy-run").Error)
	require.Equal(t, migrated.ID, migratedRun.ChannelID)
	require.NoError(t, db.First(&internal, internal.Id).Error)
	var metadata map[string]any
	require.NoError(t, json.Unmarshal([]byte(internal.OtherInfo), &metadata))
	require.Equal(t, migrated.ID, metadata["marketplace_channel_id"])
	var sequence marketplaceschema.ChannelIDSequence
	require.NoError(t, db.First(&sequence).Error)
	require.Equal(t, uint64(1), sequence.ID)
}

func TestMigrateBlindBoxLegacyCreditMarkerUpgradesExistingSQLiteTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE blind_box_credits (
		id integer primary key,
		user_id integer not null,
		remaining_amount integer not null
	)`).Error)

	require.False(t, db.Migrator().HasColumn(&commerceschema.BlindBoxCredit{}, "MigratedAt"))
	require.NoError(t, migrateBlindBoxLegacyCreditMarker(db))
	require.True(t, db.Migrator().HasColumn(&commerceschema.BlindBoxCredit{}, "MigratedAt"))
	require.NoError(t, migrateBlindBoxLegacyCreditMarker(db))
}

func TestSubscriptionFulfillmentMigrationMarksHistoricalSuccessCompleted(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	originalDB := platformdb.DB
	originalSQLite := platformdb.UsingSQLite
	originalPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalSQLite
		platformdb.UsingPostgreSQL = originalPostgreSQL
	})
	platformdb.DB = db
	platformdb.UsingSQLite = true
	platformdb.UsingPostgreSQL = false

	require.NoError(t, db.AutoMigrate(&commerceschema.SubscriptionOrder{}))
	require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{Id: 1, UserId: 1, PlanId: 1, Status: constant.TopUpStatusSuccess, TradeNo: "historic-success"}).Error)
	require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{Id: 2, UserId: 2, PlanId: 2, Status: constant.TopUpStatusPending, TradeNo: "historic-pending"}).Error)
	require.NoError(t, db.Model(&commerceschema.SubscriptionOrder{}).Where("id IN ?", []int{1, 2}).Update("fulfillment_status", "").Error)

	// Mark the preceding migration steps as already applied to simulate a
	// production database upgraded from the prior v2 revision.
	require.NoError(t, db.AutoMigrate(&schemaMigration{}))
	for _, migrationID := range []string{"20260710_billing_core", "20260710_workflow_core", "20260711_subscription_core", "20260711_gateway_execution_core"} {
		require.NoError(t, db.Create(&schemaMigration{ID: migrationID}).Error)
	}

	require.NoError(t, ApplyV2Migrations(context.Background(), false))
	var completed commerceschema.SubscriptionOrder
	require.NoError(t, db.Where("id = ?", 1).First(&completed).Error)
	require.Equal(t, commerceschema.SubscriptionOrderFulfillmentCompleted, completed.FulfillmentStatus)
	var pending commerceschema.SubscriptionOrder
	require.NoError(t, db.Where("id = ?", 2).First(&pending).Error)
	require.Empty(t, pending.FulfillmentStatus)
}

func TestBootstrapPrimarySchemaThenApplyV2Migrations(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	originalDB := platformdb.DB
	originalSQLite := platformdb.UsingSQLite
	originalPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalSQLite
		platformdb.UsingPostgreSQL = originalPostgreSQL
	})
	platformdb.DB = db
	platformdb.UsingSQLite = true
	platformdb.UsingPostgreSQL = false

	require.NoError(t, BootstrapPrimarySchema())
	require.NoError(t, ApplyV2Migrations(context.Background(), false))
	require.NoError(t, ApplyV2Migrations(context.Background(), false))
	for _, table := range []string{
		"subscription_plans",
		"subscription_orders",
		"user_subscriptions",
		"subscription_pre_consume_records",
		"gateway_request_executions",
	} {
		require.True(t, db.Migrator().HasTable(table), table)
	}
}

func TestMigrateUserExternalIDsBackfillsExistingUsers(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&legacyUserForExternalIDMigration{}))
	require.NoError(t, db.Create(&legacyUserForExternalIDMigration{Id: 1, Username: "legacy-one"}).Error)
	require.NoError(t, db.Create(&legacyUserForExternalIDMigration{Id: 2, Username: "legacy-two"}).Error)

	require.NoError(t, migrateUserExternalIDs(db))
	require.True(t, db.Migrator().HasColumn(&identityschema.User{}, "ExternalId"))
	require.True(t, db.Migrator().HasIndex(&identityschema.User{}, "idx_users_external_id"))

	var users []identityschema.User
	require.NoError(t, db.Order("id asc").Find(&users).Error)
	require.Len(t, users, 2)
	require.Len(t, users[0].ExternalId, identityschema.ExternalUserIDLength)
	require.Len(t, users[1].ExternalId, identityschema.ExternalUserIDLength)
	require.NotEqual(t, users[0].ExternalId, users[1].ExternalId)

	firstID := users[0].ExternalId
	require.NoError(t, migrateUserExternalIDs(db))
	require.NoError(t, db.First(&users[0], 1).Error)
	require.Equal(t, firstID, users[0].ExternalId)
}

func TestMigrateMarketplaceChannelSourceLabelsUpgradesExistingSQLiteTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE marketplace_channels (
		id text primary key,
		owner_user_id integer not null,
		provider_type text not null,
		base_url_ciphertext text not null,
		credential_ciphertext text not null,
		status text not null
	)`).Error)

	require.False(t, db.Migrator().HasColumn(&marketplaceschema.Channel{}, "SubmittedSourceLabel"))
	require.NoError(t, migrateMarketplaceChannelSourceLabels(db))
	for _, field := range []string{
		"SubmittedSourceLabel",
		"ApprovedSourceLabel",
		"SourceLabelStatus",
		"SourceLabelReviewReason",
	} {
		require.True(t, db.Migrator().HasColumn(&marketplaceschema.Channel{}, field), field)
	}
	require.True(t, db.Migrator().HasTable(&marketplaceschema.Settlement{}))
	require.NoError(t, migrateMarketplaceChannelSourceLabels(db))
}

func TestMigrateMarketplaceModelVerificationUpgradesExistingSQLiteTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE marketplace_channels (
		id text primary key,
		owner_user_id integer not null,
		provider_type text not null,
		base_url_ciphertext text not null,
		credential_ciphertext text not null,
		status text not null
	)`).Error)

	require.NoError(t, migrateMarketplaceModelVerification(db))
	for _, field := range []string{"ModelVerificationResults", "ModelConsistencyStatus"} {
		require.True(t, db.Migrator().HasColumn(&marketplaceschema.Channel{}, field), field)
	}
	require.NoError(t, migrateMarketplaceModelVerification(db))
}

func TestMigrateUnifiedCreditV1ChannelScopeMarksMarketplaceChannelsExternal(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gatewayschema.Channel{}, &marketplaceschema.Channel{}))

	official := gatewayschema.Channel{Id: 61, Key: "official", ChannelScope: gatewayschema.ChannelScopeOfficial}
	marketplaceInternal := gatewayschema.Channel{Id: 62, Key: "marketplace", ChannelScope: gatewayschema.ChannelScopeOfficial}
	require.NoError(t, db.Create(&[]gatewayschema.Channel{official, marketplaceInternal}).Error)
	internalID := marketplaceInternal.Id
	require.NoError(t, db.Create(&marketplaceschema.Channel{
		ID: "marketplace-62", OwnerUserID: 9, ProviderType: "openai",
		BaseURLCiphertext: "base", CredentialCiphertext: "credential",
		Status: "active", InternalChannelID: &internalID,
	}).Error)

	require.NoError(t, migrateUnifiedCreditV1ChannelScope(db))
	require.NoError(t, migrateUnifiedCreditV1ChannelScope(db))

	require.NoError(t, db.First(&official, official.Id).Error)
	require.Equal(t, gatewayschema.ChannelScopeOfficial, official.ChannelScope)
	require.NoError(t, db.First(&marketplaceInternal, marketplaceInternal.Id).Error)
	require.Equal(t, gatewayschema.ChannelScopeExternal, marketplaceInternal.ChannelScope)
	require.True(t, db.Migrator().HasTable(&commerceschema.BlindBoxPropDiscountUsage{}))
}
