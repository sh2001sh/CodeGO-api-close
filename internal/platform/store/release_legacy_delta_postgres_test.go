package store

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This rehearsal uses representative a4f8b05 columns, never AutoMigrate of the
// current legacy models. It is synthetic schema evidence, not a production dump.
// Everything, including the migrations, rolls back in the dedicated database.
func TestReleaseLegacyDeltaPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_RELEASE_DELTA_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set TEST_RELEASE_DELTA_POSTGRES_DSN to an empty dedicated loopback release database")
	}
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "postgres", parsed.Scheme)
	require.Contains(t, []string{"127.0.0.1", "localhost", "::1"}, parsed.Hostname())
	require.True(t, strings.HasPrefix(strings.TrimPrefix(parsed.Path, "/"), "codego_release_delta_"), "dedicated release database required")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	oldDB, oldPG, oldSQLite := platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite
	platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = db, true, false
	t.Cleanup(func() { platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = oldDB, oldPG, oldSQLite })
	var database, version string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&database).Error)
	require.NoError(t, db.Raw("SHOW server_version").Scan(&version).Error)
	require.Equal(t, strings.TrimPrefix(parsed.Path, "/"), database)
	var tables int64
	require.NoError(t, db.Raw("SELECT count(*) FROM information_schema.tables WHERE table_schema IN ('public','billing','marketplace') AND table_type='BASE TABLE'").Scan(&tables).Error)
	require.Zero(t, tables, "refuse an existing database with tables")
	t.Logf("PostgreSQL %s; dedicated database=%s; synthetic legacy fields based on a4f8b05", version, database)

	tx := db.Begin()
	require.NoError(t, tx.Error)
	rolledBack := false
	t.Cleanup(func() {
		if !rolledBack {
			require.NoError(t, tx.Rollback().Error)
		}
	})
	for _, statement := range releaseLegacyDeltaDDL {
		require.NoError(t, tx.Exec(statement).Error)
	}
	for _, statement := range releaseLegacyDeltaSeeds {
		require.NoError(t, tx.Exec(statement).Error)
	}
	legacy := make(map[string]string)
	legacyColumns := make(map[string]string)
	for table, additions := range releaseLegacyDeltaColumns {
		legacy[table] = releaseDeltaRows(t, tx, table, additions)
		legacyColumns[table] = releaseDeltaColumnCatalog(t, tx, table, additions)
		for column := range additions {
			var exists bool
			parts := strings.Split(table, ".")
			require.NoError(t, tx.Raw("SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=? AND table_name=? AND column_name=?)", parts[0], parts[1], column).Scan(&exists).Error)
			require.False(t, exists, "old schema unexpectedly contains %s.%s", table, column)
		}
	}
	t.Logf("Seeded %d legacy tables; new fields are absent before migrations", len(legacy))
	steps := []struct {
		id  string
		run func(*gorm.DB) error
	}{
		// This is the exact inline step in ApplyV2Migrations, followed by its
		// production function implementations in the registry's actual order.
		{"20261015_credit_pack_order_snapshot", func(db *gorm.DB) error {
			if db.Migrator().HasColumn(&commerceschema.SubscriptionOrder{}, "CreditPackQuota") {
				return nil
			}
			return db.Migrator().AddColumn(&commerceschema.SubscriptionOrder{}, "CreditPackQuota")
		}},
		{"20261015_marketplace_shops_and_presentation", migrateMarketplacePresentation},
		{"20261015_credit_pack_funding_policy", migrateCreditPackFundingPolicy},
		{"20261015_route_pool_credit_pack_filter", migrateRoutePoolCreditPackFilter},
		{"20261015_referral_consumption_rebate", migrateReferralConsumptionRebate},
		{"20261016_referral_channel_funding", migrateReferralChannelFunding},
	}
	lastIndex := -1
	for _, step := range steps {
		index := -1
		for i, id := range V2MigrationIDs() {
			if id == step.id {
				index = i
			}
		}
		require.Greater(t, index, lastIndex, "migration order must match the release contract: %s", step.id)
		lastIndex = index
	}
	before := time.Now().Unix()
	for _, step := range steps {
		require.NoError(t, tx.Transaction(step.run), step.id)
		t.Logf("applied %s", step.id)
	}
	for table, additions := range releaseLegacyDeltaColumns {
		require.JSONEq(t, legacy[table], releaseDeltaRows(t, tx, table, additions), "historical columns changed: %s", table)
		require.JSONEq(t, legacyColumns[table], releaseDeltaColumnCatalog(t, tx, table, additions), "historical column types/defaults/precision changed: %s", table)
		for column, expected := range additions {
			var mismatches int64
			require.NoError(t, tx.Raw("SELECT count(*) FROM "+table+" WHERE "+column+" IS DISTINCT FROM "+expected).Scan(&mismatches).Error)
			require.Zero(t, mismatches, "historical default %s.%s", table, column)
		}
	}
	for _, table := range []string{"marketplace.shops", "marketplace.shop_ratings", "marketplace.reports", "billing.referral_consumption_rebates"} {
		var count int64
		require.NoError(t, tx.Table(table).Count(&count).Error)
		require.Zero(t, count, "migration fabricated historical rows in %s", table)
	}
	var program billingschema.ReferralRebateProgram
	require.NoError(t, tx.First(&program, 1).Error)
	require.EqualValues(t, 300, program.RateBPS)
	require.GreaterOrEqual(t, program.EffectiveAt, before)
	require.LessOrEqual(t, program.EffectiveAt, time.Now().Unix())
	t.Logf("Historical values and column types/defaults/precision unchanged; paid snapshots remain 0; historical settlements remain releasable; program rate=%d effective_at=%d", program.RateBPS, program.EffectiveAt)

	// A distinct persisted boundary makes resetting EffectiveAt observable even
	// if both migration passes happen within the same clock second.
	sentinel := before - 86400
	require.NoError(t, tx.Model(&program).Update("effective_at", sentinel).Error)
	firstRows := releaseDeltaAllRows(t, tx)
	firstCatalog := releaseDeltaCatalog(t, tx)
	for _, step := range steps {
		require.NoError(t, tx.Transaction(step.run), "repeat %s", step.id)
	}
	require.Equal(t, firstRows, releaseDeltaAllRows(t, tx), "second pass changed rows")
	require.JSONEq(t, firstCatalog, releaseDeltaCatalog(t, tx), "second pass changed schema/indexes/constraints")
	require.NoError(t, tx.First(&program, 1).Error)
	require.Equal(t, sentinel, program.EffectiveAt)
	t.Log("Second pass: all six migration functions succeeded; rows and catalog unchanged; distinct EffectiveAt preserved")
	require.NoError(t, tx.Rollback().Error)
	rolledBack = true
	require.NoError(t, db.Raw("SELECT count(*) FROM information_schema.tables WHERE table_schema IN ('public','billing','marketplace') AND table_type='BASE TABLE'").Scan(&tables).Error)
	require.Zero(t, tables)
	t.Log("Outer transaction rolled back; dedicated database contains 0 business tables; no DROP/TRUNCATE issued")
}

func releaseDeltaRows(t *testing.T, tx *gorm.DB, table string, omitted map[string]string) string {
	t.Helper()
	expr := "to_jsonb(row)"
	for column := range omitted {
		expr += " - '" + column + "'"
	}
	var result string
	require.NoError(t, tx.Raw("SELECT COALESCE(jsonb_agg(value ORDER BY value::text), '[]'::jsonb)::text FROM (SELECT "+expr+" AS value FROM "+table+" AS row) AS rows").Scan(&result).Error)
	return result
}

func releaseDeltaAllRows(t *testing.T, tx *gorm.DB) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for table := range releaseLegacyDeltaColumns {
		result[table] = releaseDeltaRows(t, tx, table, nil)
	}
	for _, table := range []string{"marketplace.shops", "marketplace.shop_ratings", "marketplace.reports", "billing.referral_consumption_rebates", "billing.referral_rebate_programs"} {
		result[table] = releaseDeltaRows(t, tx, table, nil)
	}
	return result
}

func releaseDeltaColumnCatalog(t *testing.T, tx *gorm.DB, table string, omitted map[string]string) string {
	t.Helper()
	parts := strings.Split(table, ".")
	query := "SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY column_name),'[]'::jsonb)::text FROM information_schema.columns c WHERE table_schema=? AND table_name=?"
	for column := range omitted {
		query += " AND column_name <> '" + column + "'"
	}
	var result string
	require.NoError(t, tx.Raw(query, parts[0], parts[1]).Scan(&result).Error)
	return result
}

func releaseDeltaCatalog(t *testing.T, tx *gorm.DB) string {
	t.Helper()
	var result string
	require.NoError(t, tx.Raw(`SELECT jsonb_build_object(
		'columns', (SELECT jsonb_agg(to_jsonb(c) ORDER BY table_schema,table_name,ordinal_position) FROM information_schema.columns c WHERE table_schema IN ('public','billing','marketplace')),
		'indexes', (SELECT jsonb_agg(to_jsonb(i) ORDER BY schemaname,tablename,indexname) FROM pg_indexes i WHERE schemaname IN ('public','billing','marketplace')),
		'constraints', (SELECT jsonb_agg(to_jsonb(c) ORDER BY constraint_schema,table_name,constraint_name) FROM information_schema.table_constraints c WHERE constraint_schema IN ('public','billing','marketplace'))
	)::text`).Scan(&result).Error)
	return result
}

var releaseLegacyDeltaColumns = map[string]map[string]string{
	"public.subscription_orders":             {"credit_pack_quota": "0", "credit_pack_paid_quota": "0", "credit_pack_channel_share_bps": "0"},
	"public.user_subscriptions":              {"credit_pack_granted_quota": "0", "credit_pack_paid_quota": "0", "credit_pack_channel_share_bps": "0"},
	"public.subscription_claude_conversions": {"target_credit_pack_subscription_id": "0"},
	"public.channels":                        {"credit_pack_supported": "false"},
	"marketplace.channels":                   {"credit_pack_supported": "false"},
	"marketplace.groups":                     {"custom_name": "NULL", "description": "NULL", "tags": "NULL", "moderation_status": "NULL", "moderation_reason": "NULL", "moderation_checked_at": "NULL"},
	"marketplace.settlements":                {"credit_pack_granted_quota": "0", "credit_pack_paid_quota": "0", "credit_pack_channel_share_bps": "0", "credit_pack_discount_amount": "0", "credit_pack_platform_subsidy_amount": "0", "referral_rebate_amount": "0", "referral_rebate_pending": "false"},
	"marketplace.route_pools":                {"auto_build_credit_pack_only": "false"},
	"billing.funding_lots":                   {"paid_quota": "0"},
	"billing.funding_allocations":            {"paid_quota": "0"},
	"public.top_ups":                         {"paid_quota": "0"},
}

var releaseLegacyDeltaDDL = []string{
	`CREATE SCHEMA IF NOT EXISTS billing`,
	`CREATE SCHEMA IF NOT EXISTS marketplace`,
	`CREATE TABLE public.subscription_orders (id bigint PRIMARY KEY, user_id bigint, plan_id bigint, money numeric(10,2), original_money numeric(10,2) NOT NULL DEFAULT 0, trade_no varchar(255) UNIQUE, payment_method varchar(50), status text, fulfillment_status varchar(32) DEFAULT 'completed', target_subscription_id integer DEFAULT 0, refund_status varchar(24) DEFAULT '', refund_quota bigint DEFAULT 0)`,
	`CREATE TABLE public.user_subscriptions (id bigint PRIMARY KEY, user_id bigint, plan_id bigint, amount_total bigint NOT NULL DEFAULT 0, amount_used bigint NOT NULL DEFAULT 0, period_amount bigint NOT NULL DEFAULT 0, period_used bigint NOT NULL DEFAULT 0, model_limits text, model_usage text, end_time bigint, status varchar(32), source varchar(32) DEFAULT 'order', next_reset_time bigint DEFAULT 0)`,
	`CREATE TABLE public.subscription_claude_conversions (id bigint PRIMARY KEY, user_id bigint, user_subscription_id bigint, source_quota bigint, target_claude_quota integer, ratio_numerator integer DEFAULT 1, ratio_denominator integer DEFAULT 10, request_id varchar(128) UNIQUE)`,
	`CREATE TABLE public.channels (id bigint PRIMARY KEY, name text, channel_scope varchar(32), multiplier_card_supported boolean NOT NULL DEFAULT false)`,
	`CREATE TABLE marketplace.channels (id varchar(64) PRIMARY KEY, owner_user_id bigint NOT NULL, status varchar(24) NOT NULL, internal_channel_id bigint, multiplier_card_supported boolean NOT NULL DEFAULT false)`,
	`CREATE TABLE marketplace.groups (id varchar(64) PRIMARY KEY, channel_id varchar(64) NOT NULL UNIQUE, owner_user_id bigint NOT NULL, public_slug varchar(80) NOT NULL UNIQUE, system_display_name varchar(128) NOT NULL, internal_group_name varchar(64) NOT NULL UNIQUE, multiplier numeric(12,6) NOT NULL, routing_version integer NOT NULL DEFAULT 1, lifecycle_status varchar(24) NOT NULL)`,
	`CREATE TABLE marketplace.settlements (id varchar(64) PRIMARY KEY, request_id varchar(64) NOT NULL UNIQUE, group_id varchar(64) NOT NULL, owner_user_id bigint NOT NULL, consumer_user_id bigint NOT NULL, billing_source varchar(24) NOT NULL DEFAULT 'wallet', consumer_amount bigint NOT NULL, settlement_gross_amount bigint NOT NULL DEFAULT 0, platform_commission bigint NOT NULL, transaction_fee bigint NOT NULL, owner_net_amount bigint NOT NULL, reclaimed_amount bigint NOT NULL DEFAULT 0, multiplier numeric(12,6) NOT NULL, subscription_multiplier numeric(12,6) NOT NULL DEFAULT 0, status varchar(24) NOT NULL, available_at timestamptz, released_at timestamptz, created_at timestamptz)`,
	`CREATE TABLE marketplace.route_pools (id varchar(64) PRIMARY KEY, owner_user_id bigint NOT NULL, name varchar(64) NOT NULL, strategy varchar(16) NOT NULL DEFAULT 'priority', auto_build_enabled boolean NOT NULL DEFAULT false, auto_build_models text NOT NULL DEFAULT '[]', auto_build_consumer_weight bigint NOT NULL DEFAULT 25, auto_build_success_weight bigint NOT NULL DEFAULT 35)`,
	`CREATE TABLE billing.funding_lots (lot_id varchar(64) PRIMARY KEY, account_id varchar(64), source varchar(32), idempotency_key varchar(255) UNIQUE, original_amount bigint, remaining_amount bigint, revenue_multiplier numeric(12,6))`,
	`CREATE TABLE billing.funding_allocations (allocation_id varchar(64) PRIMARY KEY, request_id varchar(64), lot_id varchar(64), account_id varchar(64), source varchar(32), amount bigint, revenue_multiplier numeric(12,6))`,
	`CREATE TABLE public.top_ups (id bigint PRIMARY KEY, user_id bigint, amount bigint, money numeric(10,2), trade_no varchar(255) UNIQUE, status text, refund_status varchar(24) DEFAULT '', refund_quota bigint DEFAULT 0)`,
}

var releaseLegacyDeltaSeeds = []string{
	`INSERT INTO public.subscription_orders (id,user_id,plan_id,money,original_money,trade_no,payment_method,status,target_subscription_id) VALUES (1,42,7,64.00,68.00,'old-order','cash','success',2)`,
	`INSERT INTO public.user_subscriptions (id,user_id,plan_id,amount_total,amount_used,period_amount,period_used,model_limits,model_usage,end_time,status,source,next_reset_time) VALUES (2,42,7,680000,123456,60000,5000,'{"gpt-5":1000}','{"gpt-5":250}',1900000000,'active','order',1890000000)`,
	`INSERT INTO public.subscription_claude_conversions VALUES (3,42,2,100000,10000,1,10,'old-conversion')`,
	`INSERT INTO public.channels VALUES (21,'old-official','official',true)`,
	`INSERT INTO marketplace.channels VALUES ('22',43,'active',21,true)`,
	`INSERT INTO marketplace.groups VALUES ('old-group','22',43,'old-public','22 Original','marketplace_old',0.123456,4,'published')`,
	`INSERT INTO marketplace.settlements VALUES ('old-pending','request-pending','old-group',43,42,'subscription',100000,10000,500,125,9375,0,0.123456,1.75,'pending','2026-10-16T00:00:00Z',NULL,'2026-10-15T00:00:00Z'), ('old-released','request-released','old-group',43,42,'wallet',20000,20000,1000,250,18750,1000,0.654321,0,'released','2026-10-14T00:00:00Z','2026-10-15T00:00:00Z','2026-10-13T00:00:00Z')`,
	`INSERT INTO marketplace.route_pools VALUES ('old-pool',42,'Old pool','priority',true,'["gpt-5","deepseek-chat"]',40,30)`,
	`INSERT INTO billing.funding_lots VALUES ('old-lot','old-wallet','topup','topup:old:unified',5000,1234,0.125)`,
	`INSERT INTO billing.funding_allocations VALUES ('old-allocation','old-request','old-lot','old-wallet','topup',3766,0.125)`,
	`INSERT INTO public.top_ups VALUES (4,42,5000,10.00,'old','success','',0)`,
}
