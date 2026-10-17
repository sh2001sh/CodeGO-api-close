package http

import (
	"testing"

	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
)

func TestTokenGroupMonthlyPassRequiresLiveOwnedEntitlement(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&commerceschema.BlindBoxProp{}))
	user := &identityschema.User{Id: 7, Group: "default"}
	check := func(allowed bool) {
		_, _, err := normalizeTokenGroupSelection(user.Id, commerceapp.LegacyMonthlyPassGroup, false)
		_, desktopErr := identityapp.ValidateDesktopGroupForUser(user, commerceapp.LegacyMonthlyPassGroup)
		if allowed {
			require.NoError(t, err)
			require.NoError(t, desktopErr)
		} else {
			require.Error(t, err)
			require.Error(t, desktopErr)
		}
	}
	check(false)
	prop := commerceschema.BlindBoxProp{UserId: 8, PropType: commerceschema.BlindBoxPropTypeMonthlyPassMultiplier, Status: commerceschema.BlindBoxPropStatusActive, ExpiresAt: platformruntime.GetTimestamp() + 3600}
	require.NoError(t, db.Create(&prop).Error)
	check(false)
	require.NoError(t, db.Model(&prop).Update("user_id", user.Id).Error)
	check(true)
	for _, status := range []string{commerceschema.BlindBoxPropStatusPaused, commerceschema.BlindBoxPropStatusAvailable, commerceschema.BlindBoxPropStatusExpired} {
		require.NoError(t, db.Model(&prop).Update("status", status).Error)
		check(false)
	}
	require.NoError(t, db.Model(&prop).Updates(map[string]any{"status": commerceschema.BlindBoxPropStatusActive, "expires_at": platformruntime.GetTimestamp() - 1}).Error)
	check(false)
	require.NoError(t, db.Migrator().DropTable(&commerceschema.BlindBoxProp{}))
	check(false)
}
