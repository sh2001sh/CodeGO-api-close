package app

import (
	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestMigrateBlindBoxLegacyCreditsCreditsUnifiedBalanceAndCleansRows(t *testing.T) {
	db := setupRedemptionTestDB(t)

	users := []*identityschema.User{
		{Id: 8804, Username: "legacy_wallet_user", AffCode: "legacy-wallet-user", Status: constant.UserStatusEnabled, Quota: 100, ClaudeQuota: 200},
		{Id: 8805, Username: "legacy_claude_user", AffCode: "legacy-claude-user", Status: constant.UserStatusEnabled, Quota: 300, ClaudeQuota: 400},
	}
	for _, user := range users {
		require.NoError(t, db.Create(user).Error)
	}

	records := []*commerceschema.BlindBoxOpenRecord{
		{Id: 9901, UserId: users[0].Id, RewardType: commerceschema.BlindBoxRewardTypeQuota, RewardWalletType: string(commerceschema.BlindBoxRewardWalletTypeDefault)},
		{Id: 9902, UserId: users[1].Id, RewardType: commerceschema.BlindBoxRewardTypeClaudeQuota, RewardWalletType: string(commerceschema.BlindBoxRewardWalletTypeClaude)},
	}
	for _, record := range records {
		require.NoError(t, db.Create(record).Error)
	}

	credits := []*commerceschema.BlindBoxCredit{
		{UserId: users[0].Id, OpenRecordId: records[0].Id, OriginalAmount: 120, RemainingAmount: 120, RewardUSD: 1.2, ExpiresAt: time.Now().Add(24 * time.Hour).Unix(), Status: commerceschema.BlindBoxCreditStatusActive},
		{UserId: users[1].Id, OpenRecordId: records[1].Id, OriginalAmount: 80, RemainingAmount: 80, RewardUSD: 0.8, ExpiresAt: time.Now().Add(24 * time.Hour).Unix(), Status: commerceschema.BlindBoxCreditStatusActive},
	}
	for _, credit := range credits {
		require.NoError(t, db.Create(credit).Error)
	}

	require.NoError(t, MigrateBlindBoxLegacyCredits())

	var afterUsers []*identityschema.User
	require.NoError(t, db.Order("id asc").Find(&afterUsers).Error)
	require.Len(t, afterUsers, 2)
	assert.Equal(t, 100, afterUsers[0].Quota)
	assert.Equal(t, 320, afterUsers[0].ClaudeQuota)
	assert.Equal(t, 300, afterUsers[1].Quota)
	assert.Equal(t, 480, afterUsers[1].ClaudeQuota)

	unifiedSnapshot := loadCommerceBillingSnapshot(t, users[0].Id, "claude_wallet")
	assert.Equal(t, int64(afterUsers[0].ClaudeQuota), unifiedSnapshot.AvailableBalance)
	claudeSnapshot := loadCommerceBillingSnapshot(t, users[1].Id, "claude_wallet")
	assert.Equal(t, int64(afterUsers[1].ClaudeQuota), claudeSnapshot.AvailableBalance)

	var afterCredits []commerceschema.BlindBoxCredit
	require.NoError(t, db.Order("id asc").Find(&afterCredits).Error)
	require.Len(t, afterCredits, 0)

	require.NoError(t, MigrateBlindBoxLegacyCredits())

	var finalUsers []*identityschema.User
	require.NoError(t, db.Order("id asc").Find(&finalUsers).Error)
	require.Len(t, finalUsers, 2)
	assert.Equal(t, 100, finalUsers[0].Quota)
	assert.Equal(t, 320, finalUsers[0].ClaudeQuota)
	assert.Equal(t, 300, finalUsers[1].Quota)
	assert.Equal(t, 480, finalUsers[1].ClaudeQuota)
}

func TestMigrateBlindBoxLegacyCredits_SkipsCacheInvalidationWhenRedisClientNotReady(t *testing.T) {
	db := setupRedemptionTestDB(t)

	originalRedisEnabled := platformcache.RedisEnabled
	originalRDB := platformcache.RDB
	platformcache.RedisEnabled = true
	platformcache.RDB = nil
	t.Cleanup(func() {
		platformcache.RedisEnabled = originalRedisEnabled
		platformcache.RDB = originalRDB
	})

	user := &identityschema.User{Id: 8807, Username: "legacy_no_redis_client", Status: constant.UserStatusEnabled, Quota: 100, ClaudeQuota: 50}
	require.NoError(t, db.Create(user).Error)

	record := &commerceschema.BlindBoxOpenRecord{
		Id:               9904,
		UserId:           user.Id,
		RewardType:       commerceschema.BlindBoxRewardTypeQuota,
		RewardWalletType: string(commerceschema.BlindBoxRewardWalletTypeDefault),
	}
	require.NoError(t, db.Create(record).Error)

	credit := &commerceschema.BlindBoxCredit{
		UserId:          user.Id,
		OpenRecordId:    record.Id,
		OriginalAmount:  75,
		RemainingAmount: 75,
		RewardUSD:       0.75,
		ExpiresAt:       time.Now().Add(24 * time.Hour).Unix(),
		Status:          commerceschema.BlindBoxCreditStatusActive,
	}
	require.NoError(t, db.Create(credit).Error)

	require.NotPanics(t, func() {
		require.NoError(t, MigrateBlindBoxLegacyCredits())
	})

	var savedUser identityschema.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&savedUser).Error)
	assert.Equal(t, 100, savedUser.Quota)
	assert.Equal(t, 125, savedUser.ClaudeQuota)

	var savedCredit commerceschema.BlindBoxCredit
	assert.ErrorIs(t, gorm.ErrRecordNotFound, db.Where("id = ?", credit.Id).First(&savedCredit).Error)
}

func TestMigrateBlindBoxLegacyCredits_DeletesExpiredCreditsWithoutWalletMigration(t *testing.T) {
	db := setupRedemptionTestDB(t)

	user := &identityschema.User{Id: 8806, Username: "expired_legacy_user", Status: constant.UserStatusEnabled, Quota: 100, ClaudeQuota: 200}
	require.NoError(t, db.Create(user).Error)

	record := &commerceschema.BlindBoxOpenRecord{
		Id:               9903,
		UserId:           user.Id,
		RewardType:       commerceschema.BlindBoxRewardTypeQuota,
		RewardWalletType: string(commerceschema.BlindBoxRewardWalletTypeDefault),
	}
	require.NoError(t, db.Create(record).Error)

	credit := &commerceschema.BlindBoxCredit{
		UserId:          user.Id,
		OpenRecordId:    record.Id,
		OriginalAmount:  90,
		RemainingAmount: 90,
		RewardUSD:       0.9,
		ExpiresAt:       time.Now().Add(-time.Hour).Unix(),
		Status:          commerceschema.BlindBoxCreditStatusActive,
	}
	require.NoError(t, db.Create(credit).Error)

	require.NoError(t, MigrateBlindBoxLegacyCredits())

	var savedUser identityschema.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&savedUser).Error)
	assert.Equal(t, 100, savedUser.Quota)
	assert.Equal(t, 200, savedUser.ClaudeQuota)

	var savedCredit commerceschema.BlindBoxCredit
	assert.ErrorIs(t, gorm.ErrRecordNotFound, db.Where("id = ?", credit.Id).First(&savedCredit).Error)
}

func TestMigrateBlindBoxLegacyCredits_DeletesExhaustedCredits(t *testing.T) {
	db := setupRedemptionTestDB(t)

	user := &identityschema.User{Id: 8808, Username: "exhausted_legacy_user", Status: constant.UserStatusEnabled, Quota: 500, ClaudeQuota: 600}
	require.NoError(t, db.Create(user).Error)

	record := &commerceschema.BlindBoxOpenRecord{
		Id:               9905,
		UserId:           user.Id,
		RewardType:       commerceschema.BlindBoxRewardTypeQuota,
		RewardWalletType: string(commerceschema.BlindBoxRewardWalletTypeDefault),
	}
	require.NoError(t, db.Create(record).Error)

	credit := &commerceschema.BlindBoxCredit{
		UserId:          user.Id,
		OpenRecordId:    record.Id,
		OriginalAmount:  100,
		RemainingAmount: 0,
		RewardUSD:       1.0,
		ExpiresAt:       time.Now().Add(24 * time.Hour).Unix(),
		Status:          commerceschema.BlindBoxCreditStatusExhausted,
	}
	require.NoError(t, db.Create(credit).Error)

	require.NoError(t, MigrateBlindBoxLegacyCredits())

	var savedUser identityschema.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&savedUser).Error)
	assert.Equal(t, 500, savedUser.Quota)
	assert.Equal(t, 600, savedUser.ClaudeQuota)

	var savedCredit commerceschema.BlindBoxCredit
	assert.ErrorIs(t, gorm.ErrRecordNotFound, db.Where("id = ?", credit.Id).First(&savedCredit).Error)
}
