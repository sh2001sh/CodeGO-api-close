package app

import (
	"testing"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMultiplierCardRoutePolicyIsOneReadAndHonorsExpiry(t *testing.T) {
	db := setupRedemptionTestDB(t)
	now := platformruntime.GetTimestamp()
	props := []commerceschema.BlindBoxProp{
		{UserId: 9841, PropType: commerceschema.BlindBoxPropTypeMonthlyPassMultiplier, Status: commerceschema.BlindBoxPropStatusActive, ExpiresAt: now - 1},
		{UserId: 9841, PropType: commerceschema.BlindBoxPropTypeConsumeDiscount95, Status: commerceschema.BlindBoxPropStatusPaused, ExpiresAt: now + 3600},
	}
	require.NoError(t, db.Create(&props).Error)
	var reads, writes, rawSQL int
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("count_policy_reads", func(*gorm.DB) { reads++ }))
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("count_policy_writes", func(*gorm.DB) { writes++ }))
	require.NoError(t, db.Callback().Raw().Before("gorm:raw").Register("count_policy_catalog", func(*gorm.DB) { rawSQL++ }))
	official, monthly := ActiveMultiplierCardRoutePolicy(9841)
	require.False(t, official)
	require.False(t, monthly)
	require.Equal(t, 1, reads)
	require.Zero(t, writes)
	require.Zero(t, rawSQL)
	require.NoError(t, db.Model(&props[0]).Update("expires_at", now+3600).Error)
	official, monthly = ActiveMultiplierCardRoutePolicy(9841)
	require.True(t, official)
	require.True(t, monthly)
	require.NoError(t, db.Model(&props[0]).Update("status", commerceschema.BlindBoxPropStatusPaused).Error)
	official, monthly = ActiveMultiplierCardRoutePolicy(9841)
	require.False(t, official, "pausing a card takes effect on the next request without cache delay")
	require.False(t, monthly)
}
