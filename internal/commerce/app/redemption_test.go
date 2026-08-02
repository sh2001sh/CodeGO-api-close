package app

import (
	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func setupRedemptionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	platformdb.UsingSQLite = true
	platformdb.UsingMySQL = false
	platformdb.UsingPostgreSQL = false
	platformcache.RedisEnabled = false
	platformconfig.BatchUpdateEnabled = false
	platformconfig.LogConsumeEnabled = true

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
		&commerceschema.Redemption{},
		&commerceschema.TopUp{},
		&commerceschema.SubscriptionPlan{},
		&commerceschema.SubscriptionOrder{},
		&commerceschema.UserSubscription{},
		&commerceschema.GroupBuyOrder{},
		&commerceschema.GroupBuyMember{},
		&commerceschema.SubscriptionClaudeConversion{},
		&commerceschema.WalletQuotaConversion{},
		&commerceschema.BlindBoxOrder{},
		&commerceschema.BlindBoxCredit{},
		&commerceschema.BlindBoxOpenRecord{},
		&commerceschema.BlindBoxPityState{},
		&commerceschema.BlindBoxZeroHourState{},
		&commerceschema.BlindBoxProp{},
		&billingschema.PointAccount{},
		&billingschema.PointLedger{},
		&commerceschema.SubscriptionResetOpportunityAccount{},
		&commerceschema.SubscriptionResetOpportunityLedger{},
	))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestRedeemCodeBlindBoxCreatesPendingOrderForManualOpen(t *testing.T) {
	db := setupRedemptionTestDB(t)

	originalSetting := blindboxsettings.Get()
	setting := originalSetting
	setting.Enabled = true
	setting.DailyOpenLimit = 100
	setting.SubscriptionPrizeProbability = 0
	setting.PityThreshold = 999
	setting.PityGuaranteeUSD = 0
	setting.LowRewardThresholdUSD = 0
	setting.FirstPurchaseGuaranteeUSD = 0.0001
	setting.Tiers = []blindboxsettings.TierSetting{
		{Name: "redemption reward", MinUSD: 1, MaxUSD: 1, Probability: 1},
	}
	blindboxsettings.Set(setting)
	t.Cleanup(func() {
		blindboxsettings.Set(originalSetting)
	})

	user := &identityschema.User{Id: 8801, Username: "blind_box_redeem_user", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)

	redemption := &commerceschema.Redemption{
		Id:               9901,
		Key:              "blind-box-redeem-key",
		Name:             "Blind Box x3",
		Status:           constant.RedemptionCodeStatusEnabled,
		RedeemType:       commerceschema.RedemptionTypeBlindBox,
		BlindBoxQuantity: 3,
		CreatedTime:      time.Now().Unix(),
	}
	require.NoError(t, db.Create(redemption).Error)

	result, err := RedeemCode(user.Id, redemption.Key)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, commerceschema.RedemptionTypeBlindBox, result.RedeemType)
	assert.Equal(t, 3, result.BlindBoxQuantity)
	assert.NotZero(t, result.BlindBoxOrderId)
	assert.Zero(t, result.BlindBoxOpenCount)
	assert.Empty(t, result.BlindBoxRecords)

	var order commerceschema.BlindBoxOrder
	require.NoError(t, db.Where("id = ?", result.BlindBoxOrderId).First(&order).Error)
	assert.Equal(t, user.Id, order.UserId)
	assert.Equal(t, 3, order.Quantity)
	assert.Zero(t, order.OpenedCount)
	assert.Equal(t, constant.TopUpStatusSuccess, order.Status)
	assert.Equal(t, "redemption", order.PaymentMethod)
	assert.Equal(t, "redemption", order.PaymentProvider)

	var savedUser identityschema.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&savedUser).Error)
	assert.Zero(t, savedUser.Quota)

	records, err := OpenBlindBoxes(user.Id, 3)
	require.NoError(t, err)
	require.Len(t, records, 3)
	require.NoError(t, db.Where("id = ?", order.Id).First(&order).Error)
	assert.Equal(t, 3, order.OpenedCount)
	require.NoError(t, db.Where("id = ?", user.Id).First(&savedUser).Error)
	snapshot := loadCommerceBillingSnapshot(t, user.Id, "wallet")
	assert.Equal(t, int64(savedUser.Quota), snapshot.AvailableBalance)

	var saved commerceschema.Redemption
	require.NoError(t, db.Where("id = ?", redemption.Id).First(&saved).Error)
	assert.Equal(t, constant.RedemptionCodeStatusUsed, saved.Status)
	assert.Equal(t, user.Id, saved.UsedUserId)
	assert.NotZero(t, saved.RedeemedTime)
}

func TestRedeemCodeClaudeQuotaAddsClaudeQuotaOnly(t *testing.T) {
	db := setupRedemptionTestDB(t)

	user := &identityschema.User{
		Id:          8802,
		Username:    "claude_quota_redeem_user",
		Status:      constant.UserStatusEnabled,
		Quota:       1200,
		ClaudeQuota: 300,
	}
	require.NoError(t, db.Create(user).Error)

	redemption := &commerceschema.Redemption{
		Id:          9902,
		Key:         "claude-quota-redeem-key",
		Name:        "Claude Quota Pack",
		Status:      constant.RedemptionCodeStatusEnabled,
		RedeemType:  commerceschema.RedemptionTypeQuota,
		WalletType:  commerceschema.WalletTypeClaude,
		Quota:       800,
		CreatedTime: time.Now().Unix(),
	}
	require.NoError(t, db.Create(redemption).Error)

	result, err := RedeemCode(user.Id, redemption.Key)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, commerceschema.RedemptionTypeQuota, result.RedeemType)
	assert.Equal(t, commerceschema.WalletTypeClaude, result.WalletType)
	assert.Equal(t, 800, result.Quota)

	var savedUser identityschema.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&savedUser).Error)
	assert.Equal(t, 1200, savedUser.Quota)
	assert.Equal(t, 1100, savedUser.ClaudeQuota)
	snapshot := loadCommerceBillingSnapshot(t, user.Id, "claude_wallet")
	assert.EqualValues(t, 1100, snapshot.AvailableBalance)
}

func loadCommerceBillingSnapshot(t *testing.T, userID int, accountType string) *billingschema.BillingBalanceSnapshot {
	t.Helper()

	var account billingschema.BillingAccount
	require.NoError(t, platformdb.DB.Where("owner_type = ? AND owner_id = ? AND account_type = ?", "user", userID, accountType).First(&account).Error)

	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, platformdb.DB.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	return &snapshot
}

func TestRedeemCodeReturnsSpecificBusinessErrors(t *testing.T) {
	db := setupRedemptionTestDB(t)

	user := &identityschema.User{Id: 8810, Username: "redeem_error_user", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)

	tests := []struct {
		name       string
		key        string
		redemption commerceschema.Redemption
		wantErr    error
	}{
		{
			name: "invalid code",
			key:  "missing-code",
			redemption: commerceschema.Redemption{
				Id:          9910,
				Key:         "invalid-code",
				Name:        "Invalid",
				Status:      constant.RedemptionCodeStatusEnabled,
				CreatedTime: time.Now().Unix(),
			},
			wantErr: commercedomain.ErrRedemptionInvalid,
		},
		{
			name: "used code",
			redemption: commerceschema.Redemption{
				Id:           9911,
				Key:          "used-code",
				Name:         "Used",
				Status:       constant.RedemptionCodeStatusUsed,
				CreatedTime:  time.Now().Unix(),
				RedeemedTime: time.Now().Unix(),
				UsedUserId:   user.Id,
			},
			wantErr: commercedomain.ErrRedemptionUsed,
		},
		{
			name: "expired code",
			redemption: commerceschema.Redemption{
				Id:          9912,
				Key:         "expired-code",
				Name:        "Expired",
				Status:      constant.RedemptionCodeStatusEnabled,
				ExpiredTime: time.Now().Add(-time.Hour).Unix(),
				CreatedTime: time.Now().Unix(),
			},
			wantErr: commercedomain.ErrRedemptionExpired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, db.Create(&tt.redemption).Error)
			redeemKey := tt.redemption.Key
			if tt.key != "" {
				redeemKey = tt.key
			}
			_, err := RedeemCode(user.Id, redeemKey)
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}
