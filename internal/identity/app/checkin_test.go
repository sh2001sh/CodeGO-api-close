package app

import (
	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func setupCheckinAppTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	platformdb.UsingSQLite = true
	platformdb.UsingMySQL = false
	platformdb.UsingPostgreSQL = false
	platformcache.RedisEnabled = false

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	platformdb.DB = db
	platformdb.LogDB = db

	require.NoError(t, db.AutoMigrate(
		&billingschema.BillingAccount{},
		&billingschema.BillingBalanceSnapshot{},
		&billingschema.BillingLedgerEntry{},
		&billingschema.BillingReservation{},
		&billingschema.BillingSettlement{},
		&billingschema.BillingOutboxEvent{},
		&identityschema.User{},
		&auditschema.Log{},
		&identitydomain.Checkin{},
	))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func snapshotCheckinSettingForAppTest() func() {
	current := *identitystore.GetCheckinSetting()
	return func() {
		*identitystore.GetCheckinSetting() = current
	}
}

func TestLoadCheckinStatusReturnsMonthlyStats(t *testing.T) {
	db := setupCheckinAppTestDB(t)
	t.Cleanup(snapshotCheckinSettingForAppTest())

	setting := identitystore.GetCheckinSetting()
	setting.Enabled = true
	setting.MinQuota = 100
	setting.MaxQuota = 200

	user := &identityschema.User{Id: 1, Username: "checkin-app-user", Status: constant.UserStatusEnabled, Group: "default"}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(&identitydomain.Checkin{
		UserId:       user.Id,
		CheckinDate:  time.Now().Format("2006-01-02"),
		QuotaAwarded: 150,
		CreatedAt:    time.Now().Unix(),
	}).Error)

	status, err := LoadCheckinStatus(user.Id, time.Now().Format("2006-01"))
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.True(t, status.Enabled)
	assert.Equal(t, 100, status.MinQuota)
	assert.Equal(t, 200, status.MaxQuota)
	assert.Equal(t, true, status.Stats["checked_in_today"])
	assert.Equal(t, int64(1), status.Stats["total_checkins"])
	assert.Equal(t, int64(150), status.Stats["total_quota"])
}

func TestPerformCheckinCreatesRecordUpdatesQuotaAndWritesAuditLog(t *testing.T) {
	db := setupCheckinAppTestDB(t)
	t.Cleanup(snapshotCheckinSettingForAppTest())

	setting := identitystore.GetCheckinSetting()
	setting.Enabled = true
	setting.MinQuota = 120
	setting.MaxQuota = 120

	user := &identityschema.User{
		Id:          2,
		Username:    "checkin-perform-user",
		Status:      constant.UserStatusEnabled,
		Group:       "default",
		ClaudeQuota: 500,
	}
	require.NoError(t, db.Create(user).Error)

	result, err := PerformCheckin(user.Id)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 120, result.QuotaAwarded)
	assert.Equal(t, time.Now().Format("2006-01-02"), result.CheckinDate)

	var savedUser identityschema.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&savedUser).Error)
	assert.Equal(t, 620, savedUser.ClaudeQuota)

	var records []identitydomain.Checkin
	require.NoError(t, db.Where("user_id = ?", user.Id).Find(&records).Error)
	require.Len(t, records, 1)
	assert.Equal(t, 120, records[0].QuotaAwarded)

	var account billingschema.BillingAccount
	require.NoError(t, db.Where("owner_type = ? AND owner_id = ? AND account_type = ?", "user", user.Id, "claude_wallet").First(&account).Error)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	assert.EqualValues(t, 620, snapshot.AvailableBalance)

	var logs []auditschema.Log
	require.NoError(t, db.Where("user_id = ? AND type = ?", user.Id, auditschema.LogTypeSystem).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0].Content, "用户签到")
}
