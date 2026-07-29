package middleware

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupNewAPIUserIdentifierTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB := platformdb.DB
	previousLogDB := platformdb.LogDB
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	require.NoError(t, err)
	platformdb.DB = db
	platformdb.LogDB = db
	require.NoError(t, db.AutoMigrate(&identityschema.User{}))
	t.Cleanup(func() {
		platformdb.DB = previousDB
		platformdb.LogDB = previousLogDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestMatchesAuthenticatedNewAPIUserSupportsExternalAndInternalIDs(t *testing.T) {
	db := setupNewAPIUserIdentifierTestDB(t)
	users := []*identityschema.User{
		{Id: 42, ExternalId: "ABCD23", Username: "first", Password: "password123", DisplayName: "First", AffCode: "first", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled},
		{Id: 43, ExternalId: "234567", Username: "second", Password: "password123", DisplayName: "Second", AffCode: "second", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled},
		{Id: 234567, ExternalId: "ZXCV23", Username: "third", Password: "password123", DisplayName: "Third", AffCode: "third", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled},
	}
	for _, user := range users {
		require.NoError(t, db.Create(user).Error)
	}

	matches, err := matchesAuthenticatedNewAPIUser("abcd23", 42)
	require.NoError(t, err)
	require.True(t, matches)

	matches, err = matchesAuthenticatedNewAPIUser("42", 42)
	require.NoError(t, err)
	require.True(t, matches)

	matches, err = matchesAuthenticatedNewAPIUser("234567", 43)
	require.NoError(t, err)
	require.True(t, matches)

	matches, err = matchesAuthenticatedNewAPIUser("234567", 234567)
	require.NoError(t, err)
	require.True(t, matches)
}

func TestMatchesAuthenticatedNewAPIUserRejectsInvalidOrForeignIdentifiers(t *testing.T) {
	db := setupNewAPIUserIdentifierTestDB(t)
	require.NoError(t, db.Create(&identityschema.User{Id: 42, ExternalId: "ABCD23", Username: "first", Password: "password123", DisplayName: "First", AffCode: "first", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&identityschema.User{Id: 43, ExternalId: "ZXCV23", Username: "second", Password: "password123", DisplayName: "Second", AffCode: "second", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled}).Error)

	matches, err := matchesAuthenticatedNewAPIUser("ZXCV23", 42)
	require.NoError(t, err)
	require.False(t, matches)

	matches, err = matchesAuthenticatedNewAPIUser("not-a-user", 42)
	require.False(t, matches)
	require.True(t, errors.Is(err, errInvalidNewAPIUserIdentifier))
}
