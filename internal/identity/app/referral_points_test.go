package app

import (
	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"strings"
	"testing"
)

func setupReferralPointsAppTestDB(t *testing.T) *gorm.DB {
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
		&identityschema.User{},
		&commerceschema.BlindBoxOrder{},
		&auditschema.Log{},
		&billingschema.PointAccount{},
		&billingschema.PointLedger{},
	))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestGrantRegistrationBlindBoxesIsIdempotent(t *testing.T) {
	db := setupReferralPointsAppTestDB(t)
	user := &identityschema.User{Id: 8103, Username: "registration-blind-box", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)

	require.NoError(t, grantRegistrationBlindBoxes(user.Id))
	require.NoError(t, grantRegistrationBlindBoxes(user.Id))

	var orders []commerceschema.BlindBoxOrder
	require.NoError(t, db.Where("user_id = ?", user.Id).Find(&orders).Error)
	require.Len(t, orders, 1)
	assert.Equal(t, registrationBlindBoxQuantity, orders[0].Quantity)
	assert.Equal(t, 0, orders[0].OpenedCount)
	assert.Equal(t, commerceschema.BlindBoxOrderSourceRegistrationBenefit, orders[0].Source)
	assert.Equal(t, constant.TopUpStatusSuccess, orders[0].Status)
}

func snapshotPaymentSettingForAppTest() func() {
	current := *commercestore.GetPaymentSetting()
	return func() {
		*commercestore.GetPaymentSetting() = current
	}
}

func TestInsertUserAndApplyRegistrationRewardsCreditsInviteePoints(t *testing.T) {
	db := setupReferralPointsAppTestDB(t)
	t.Cleanup(snapshotPaymentSettingForAppTest())

	paymentSetting := commercestore.GetPaymentSetting()
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = commercestore.CurrentComplianceTermsVersion

	inviter := &identityschema.User{
		Id:       8101,
		Username: "referral-inviter",
		Status:   constant.UserStatusEnabled,
		AffCode:  "AFF8101",
	}
	require.NoError(t, db.Create(inviter).Error)

	invitee := &identityschema.User{
		Username:    "referral-invitee",
		DisplayName: "referral-invitee",
		Status:      constant.UserStatusEnabled,
		Role:        constant.RoleCommonUser,
	}
	require.NoError(t, insertUserAndApplyRegistrationRewards(invitee, inviter.Id))

	var refreshedInviter identityschema.User
	require.NoError(t, db.Where("id = ?", inviter.Id).First(&refreshedInviter).Error)
	assert.Equal(t, 1, refreshedInviter.AffCount)

	inviteeAccount, err := billingapp.EnsurePointAccountTx(db, invitee.Id)
	require.NoError(t, err)
	assert.EqualValues(t, referralInviteeRegisterRewardPoints, inviteeAccount.Balance)

	inviterAccount, err := billingapp.EnsurePointAccountTx(db, inviter.Id)
	require.NoError(t, err)
	assert.Zero(t, inviterAccount.Balance)
}
