package store

import (
	"os"
	"testing"

	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Uses the disposable large-data fixture, never the configured application DB.
func TestPostgresUserSearchWindowPreservesPaginationAndFilters(t *testing.T) {
	dsn := os.Getenv("TEST_QUERY_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_QUERY_POSTGRES_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	var name string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&name).Error)
	require.Equal(t, "codego_query_test", name)
	originalDB, originalPG := platformdb.DB, platformdb.UsingPostgreSQL
	platformdb.DB, platformdb.UsingPostgreSQL = db, true
	t.Cleanup(func() {
		platformdb.DB, platformdb.UsingPostgreSQL = originalDB, originalPG
		sqlDB, e := db.DB()
		if e == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	cases := []struct {
		keyword, group      string
		offset, size, count int
		total               int64
	}{
		{"Q99999", "", 0, 20, 1, 1}, {"bench-user-9999", "", 0, 3, 3, 11},
		{"bench-user-9999", "", 9, 3, 2, 11}, {"bench-user-9999", "", 30, 3, 0, 11},
		{"Q99999", "absent-group", 0, 20, 0, 0}, {"no-such-user", "", 0, 20, 0, 0},
		{"899999", "", 0, 20, 1, 1},
		{"Q99999", "", 0, 0, 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.keyword+tc.group+string(rune(tc.offset+'0')), func(t *testing.T) {
			rows, total, e := SearchUsers(tc.keyword, tc.group, tc.offset, tc.size)
			require.NoError(t, e)
			require.Equal(t, tc.total, total)
			require.Len(t, rows, tc.count)
			for _, row := range rows {
				require.Empty(t, row.Password)
			}
		})
	}
}
