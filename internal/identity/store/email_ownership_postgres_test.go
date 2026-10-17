package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func isolatedEmailOwnershipPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("IDENTITY_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires dedicated codego_identity_test_ PostgreSQL database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	var name string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&name).Error)
	require.True(t, strings.HasPrefix(name, "codego_identity_test_"), "isolated identity database required")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(24)
	oldDB, oldLog, oldRedis := platformdb.DB, platformdb.LogDB, platformcache.RedisEnabled
	platformdb.DB, platformdb.LogDB, platformcache.RedisEnabled = db, db, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.LogDB, platformcache.RedisEnabled = oldDB, oldLog, oldRedis
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&identityschema.User{}))
	return db
}

func TestEmailOwnershipPostgresSerializesCreateAndBinding(t *testing.T) {
	db := isolatedEmailOwnershipPostgres(t)
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("mixed_create_and_bind_%t", mixed), func(t *testing.T) {
			const workers = 8
			suffix := fmt.Sprint(time.Now().UnixNano())
			email := "email-lock-" + suffix + "@example.com"
			users := make([]identityschema.User, workers)
			for i := range users {
				users[i] = identityschema.User{Username: fmt.Sprintf("identity-%s-%d", suffix, i), AffCode: fmt.Sprintf("ID-%s-%d", suffix, i), Status: constant.UserStatusEnabled}
				if !mixed || i%2 == 0 {
					require.NoError(t, db.Create(&users[i]).Error)
				}
			}
			blocker := db.Begin()
			require.NoError(t, blocker.Error)
			defer blocker.Rollback()
			require.NoError(t, lockEmailOwnership(blocker, email))
			results := make(chan error, workers)
			var wg sync.WaitGroup
			for i := range users {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					candidate := email
					if i%2 == 0 {
						candidate = " " + strings.ToUpper(email) + " "
					}
					if mixed && i%2 == 1 {
						users[i].Email = candidate
						results <- db.Transaction(func(tx *gorm.DB) error { return CreateUserWithTx(tx, &users[i], 0) })
					} else {
						results <- BindUserEmail(users[i].Id, candidate)
					}
				}(i)
			}
			require.Eventually(t, func() bool {
				var waiting int64
				err := db.Raw("SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND database = (SELECT oid FROM pg_database WHERE datname = current_database()) AND NOT granted").Scan(&waiting).Error
				return err == nil && waiting == workers
			}, 5*time.Second, 10*time.Millisecond, "all eight transactions must actually wait on the database lock")
			require.NoError(t, blocker.Commit().Error)
			wg.Wait()
			close(results)
			success := 0
			for err := range results {
				if err == nil {
					success++
				} else {
					require.ErrorIs(t, err, ErrEmailConflict)
				}
			}
			require.Equal(t, 1, success)
			var count int64
			require.NoError(t, db.Unscoped().Model(&identityschema.User{}).Where("LOWER(TRIM(email)) = ?", email).Count(&count).Error)
			require.EqualValues(t, 1, count)
		})
	}
}

func TestEmailOwnershipPostgresRollbackAndLegacyDuplicates(t *testing.T) {
	db := isolatedEmailOwnershipPostgres(t)
	require.Error(t, lockEmailOwnership(db, "outside-transaction@example.com"))
	higherIsolation := db.Begin(&sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	require.NoError(t, higherIsolation.Error)
	require.Error(t, lockEmailOwnership(higherIsolation, "stale-snapshot@example.com"))
	require.NoError(t, higherIsolation.Rollback().Error)
	suffix := fmt.Sprint(time.Now().UnixNano())
	email := "rollback-" + suffix + "@example.com"
	user := identityschema.User{Username: "rollback-" + suffix, AffCode: "RB-" + suffix, Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	blocker := db.Begin()
	require.NoError(t, blocker.Error)
	defer blocker.Rollback()
	require.NoError(t, lockEmailOwnership(blocker, email))
	done := make(chan error, 1)
	go func() { done <- BindUserEmail(user.Id, email) }()
	require.Eventually(t, func() bool {
		var waiting int64
		err := db.Raw("SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND NOT granted").Scan(&waiting).Error
		return err == nil && waiting == 1
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, blocker.Rollback().Error)
	require.NoError(t, <-done)
	duplicate := identityschema.User{Username: "legacy-" + suffix, Email: strings.ToUpper(email), Password: "legacy-original", AffCode: "LEG-" + suffix, Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(&duplicate).Error)
	require.True(t, IsEmailTaken(email))
	err := ResetUserPasswordByEmail(email, "Replacement123")
	require.True(t, errors.Is(err, ErrEmailConflict))
	var original identityschema.User
	require.NoError(t, db.First(&original, duplicate.Id).Error)
	require.Equal(t, "legacy-original", original.Password)
	var count int64
	require.NoError(t, db.Unscoped().Model(&identityschema.User{}).Where("LOWER(TRIM(email)) = ?", email).Count(&count).Error)
	require.EqualValues(t, 2, count, "legacy duplicates must remain untouched")
}
