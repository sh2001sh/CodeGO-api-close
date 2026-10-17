package store

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSearchUsersNormalizesIDsAndMatchesLiteralKeywords(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	// PostgreSQL LIKE is case-sensitive; use the same semantics in this fixture.
	require.NoError(t, db.Exec("PRAGMA case_sensitive_like = ON").Error)
	runUserSearchNormalizationCases(t, db, false)
}

func TestPostgresSearchUsersNormalizesIDsAndMatchesLiteralKeywords(t *testing.T) {
	dsn := os.Getenv("TEST_USER_SEARCH_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_USER_SEARCH_POSTGRES_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	var database string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&database).Error)
	require.True(t, strings.HasPrefix(database, "codego_wallet_conversion_review_"), "isolated search review database required")
	// Keep substring assertions independent of other suites in the fixture DB.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	schema := fmt.Sprintf("identity_search_%d", time.Now().UnixNano())
	require.NoError(t, db.Exec("CREATE SCHEMA "+schema).Error)
	require.NoError(t, db.Exec("SET search_path TO "+schema).Error)
	runUserSearchNormalizationCases(t, db, true)
}

func runUserSearchNormalizationCases(t *testing.T, db *gorm.DB, usingPostgres bool) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &commerceschema.UserSubscription{}, &commerceschema.SubscriptionPlan{}))
	previousDB, previousPG := platformdb.DB, platformdb.UsingPostgreSQL
	platformdb.DB, platformdb.UsingPostgreSQL = db, usingPostgres
	t.Cleanup(func() {
		platformdb.DB, platformdb.UsingPostgreSQL = previousDB, previousPG
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	for _, user := range []identityschema.User{
		{Id: 42, ExternalId: "ABC123", Username: "Alice", Email: "alice+dev@example.test", DisplayName: "R&D 100%_!", Group: "team&one", Password: "private", AffCode: "A"},
		{Id: 43, ExternalId: "XYZ789", Username: "Other", DisplayName: "R&D 100xx", Group: "other", AffCode: "B"},
	} {
		require.NoError(t, db.Create(&user).Error)
	}
	for _, tc := range []struct {
		name, keyword, group string
		want                 []int
	}{
		{"public_id", "ABC123", "", []int{42}},
		{"lowercase_id", "abc123", "", []int{42}},
		{"padded_id", " \tabc123\n", "", []int{42}},
		{"numeric_id", " 42 ", "", []int{42}},
		{"name_case", "ALICE", "", []int{42}},
		{"email_plus", "ALICE+DEV@example.test", "", []int{42}},
		{"literal_percent", "%", "", []int{42}},
		{"literal_underscore", "_", "", []int{42}},
		{"literal_escape", "!", "", []int{42}},
		{"group_match", "abc123", "team&one", []int{42}},
		{"group_isolation", "abc123", "other", nil},
		{"missing", "missing-id", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users, total, err := SearchUsers(tc.keyword, tc.group, 0, 20)
			require.NoError(t, err)
			require.EqualValues(t, len(tc.want), total)
			var ids []int
			for _, user := range users {
				ids = append(ids, user.Id)
				require.Empty(t, user.Password)
			}
			require.Equal(t, tc.want, ids)
		})
	}
	require.NoError(t, db.Create(&identityschema.User{Id: 1000, ExternalId: "DEF456", Username: "buyer42", AffCode: "C"}).Error)
	users, total, err := SearchUsers("42", "", 0, 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, users, 1)
	require.Equal(t, 42, users[0].Id, "exact IDs must stay on the first page ahead of substring matches")
}
