package app

import (
	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	workflowschema "github.com/sh2001sh/new-api/internal/workflow/schema"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	platformdb.DB = db
	platformdb.LogDB = db
	platformdb.UsingSQLite = true
	platformcache.RedisEnabled = false
	platformconfig.BatchUpdateEnabled = false
	platformconfig.LogConsumeEnabled = true

	if err := db.AutoMigrate(
		&workflowschema.Task{},
		&identityschema.User{},
		&identityschema.Token{},
		&auditschema.Log{},
		&gatewayschema.Channel{},
		&commerceschema.TopUp{},
		&billingschema.BillingAccount{},
		&billingschema.BillingBalanceSnapshot{},
		&billingschema.BillingLedgerEntry{},
		&billingschema.BillingReservation{},
		&billingschema.BillingSettlement{},
		&billingschema.BillingOutboxEvent{},
		&billingschema.RequestEconomics{},
		&marketplaceschema.Settlement{},
		&commerceschema.SubscriptionPlan{},
		&commerceschema.UserSubscription{},
		&commerceschema.BlindBoxOpenRecord{},
		&commerceschema.BlindBoxPityState{},
		&billingschema.BonusQuotaCredit{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

func truncate(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		platformdb.DB.Exec("DELETE FROM tasks")
		platformdb.DB.Exec("DELETE FROM users")
		platformdb.DB.Exec("DELETE FROM tokens")
		platformdb.DB.Exec("DELETE FROM logs")
		platformdb.DB.Exec("DELETE FROM channels")
		platformdb.DB.Exec("DELETE FROM top_ups")
		platformdb.DB.Exec("DELETE FROM billing_settlements")
		platformdb.DB.Exec("DELETE FROM billing_reservations")
		platformdb.DB.Exec("DELETE FROM billing_ledger_entries")
		platformdb.DB.Exec("DELETE FROM billing_balance_snapshots")
		platformdb.DB.Exec("DELETE FROM billing_accounts")
		platformdb.DB.Exec("DELETE FROM billing_request_economics")
		platformdb.DB.Exec("DELETE FROM marketplace_settlements")
		platformdb.DB.Exec("DELETE FROM subscription_plans")
		platformdb.DB.Exec("DELETE FROM user_subscriptions")
		platformdb.DB.Exec("DELETE FROM blind_box_open_records")
		platformdb.DB.Exec("DELETE FROM blind_box_pity_states")
		platformdb.DB.Exec("DELETE FROM bonus_quota_credits")
	})
}

func seedUser(t *testing.T, id int, quota int) {
	t.Helper()
	user := &identityschema.User{Id: id, Username: "test_user", ClaudeQuota: quota, Status: constant.UserStatusEnabled}
	require.NoError(t, platformdb.DB.Create(user).Error)
}

func seedToken(t *testing.T, id int, userID int, key string, remainQuota int) {
	t.Helper()
	token := &identityschema.Token{
		Id:          id,
		UserId:      userID,
		Key:         key,
		Name:        "test_token",
		Status:      constant.TokenStatusEnabled,
		RemainQuota: remainQuota,
		UsedQuota:   0,
	}
	require.NoError(t, platformdb.DB.Create(token).Error)
}
