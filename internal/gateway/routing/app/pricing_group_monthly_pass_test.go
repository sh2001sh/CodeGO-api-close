package app

import (
	"testing"

	"github.com/glebarez/sqlite"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestBuildUserGroupsPayloadDoesNotExposeLegacyMonthlyPassGroup(t *testing.T) {
	originalDB := platformdb.DB
	t.Cleanup(func() { platformdb.DB = originalDB })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	require.NoError(t, db.AutoMigrate(
		&identityschema.User{},
		&commerceschema.BlindBoxProp{},
		&commerceschema.BlindBoxZeroHourState{},
	))

	user := &identityschema.User{Id: 7201, Username: "monthly-pass-groups", Password: "password", Group: "default"}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(&commerceschema.BlindBoxProp{
		UserId: user.Id, PropType: commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
		Status: commerceschema.BlindBoxPropStatusActive, Multiplier: 0.1,
		ExpiresAt: platformruntime.GetTimestamp() + 900,
	}).Error)

	groups := BuildUserGroupsPayload(user.Id)
	_, exposed := groups[commerceapp.LegacyMonthlyPassGroup]
	require.False(t, exposed)
}
