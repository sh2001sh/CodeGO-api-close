package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func TestGroupBuySecurityPostgres(t *testing.T) {
	dsn := os.Getenv("GROUP_BUY_TEST_DSN")
	if dsn == "" {
		t.Skip("GROUP_BUY_TEST_DSN is not configured")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	var database string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&database).Error)
	require.True(t, strings.HasPrefix(database, "codego_group_buy_test_"), "dedicated local group-buy test database required")
	oldDB, oldLogDB := platformdb.DB, platformdb.LogDB
	oldPG, oldSQLite, oldMySQL, oldRedis := platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled
	platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled = true, false, false, false
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(16)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	db = db.WithContext(ctx)
	platformdb.DB, platformdb.LogDB = db, db
	t.Cleanup(func() {
		cancel()
		_ = sqlDB.Close()
		platformdb.DB, platformdb.LogDB = oldDB, oldLogDB
		platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled = oldPG, oldSQLite, oldMySQL, oldRedis
	})
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &auditschema.Log{}, &commerceschema.SubscriptionPlan{}, &commerceschema.SubscriptionOrder{}, &commerceschema.UserSubscription{}, &commerceschema.GroupBuyOrder{}, &commerceschema.GroupBuyMember{}, &billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{}))

	t.Run("eight simultaneous settlements grant once", func(t *testing.T) {
		room, _, sub := seedQualifiedGroupBuy(t, db, 1)
		start := make(chan struct{})
		results := make(chan error, 8)
		var workers sync.WaitGroup
		for range 8 {
			workers.Go(func() { <-start; results <- settleGroupBuyOrder(room.Id) })
		}
		close(start)
		workers.Wait()
		close(results)
		for err := range results {
			require.NoError(t, err)
		}
		require.NoError(t, db.First(sub, sub.Id).Error)
		require.EqualValues(t, 1000+quotaUnitsFromUSD(1), sub.AmountTotal)
		var grants int64
		require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("reason_code = ? AND idempotency_key LIKE ?", "subscription_bonus", fmt.Sprintf("%%group-buy:%d:%%", room.Id)).Count(&grants).Error)
		require.EqualValues(t, 1, grants)
	})
	t.Run("refund payment lock excludes grant after commit", func(t *testing.T) {
		room, paid, sub := seedQualifiedGroupBuy(t, db, 2)
		refundTx := db.Begin()
		require.NoError(t, refundTx.Error)
		defer refundTx.Rollback()
		require.NoError(t, refundTx.Clauses(clause.Locking{Strength: "UPDATE"}).First(paid, paid.Id).Error)
		done := make(chan error, 1)
		go func() { done <- settleGroupBuyOrder(room.Id) }()
		require.Eventually(t, func() bool {
			var blocked int64
			err := db.Raw("SELECT COUNT(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock' AND query LIKE '%subscription_orders%'").Scan(&blocked).Error
			return err == nil && blocked > 0
		}, 2*time.Second, 10*time.Millisecond, "settlement must actually wait on the refund-owned payment row")
		select {
		case err := <-done:
			t.Fatalf("settlement bypassed the payment lock: %v", err)
		default:
		}
		require.NoError(t, refundTx.Model(paid).Update("refund_status", commerceschema.RefundStatusProcessing).Error)
		require.NoError(t, refundTx.Commit().Error)
		require.NoError(t, <-done)
		require.NoError(t, db.First(sub, sub.Id).Error)
		require.EqualValues(t, 1000, sub.AmountTotal)
		var member commerceschema.GroupBuyMember
		require.NoError(t, db.First(&member, "order_id = ?", paid.Id).Error)
		require.False(t, member.BonusGranted)
	})
	t.Run("eight periodic reset workers recheck the locked snapshot", func(t *testing.T) {
		plan := commerceschema.SubscriptionPlan{Title: "daily reset", TotalAmount: 1000, PeriodAmount: 500, QuotaResetPeriod: commerceschema.SubscriptionResetDaily}
		require.NoError(t, db.Create(&plan).Error)
		now := time.Now().Unix()
		sub := commerceschema.UserSubscription{UserId: 9991, PlanId: plan.Id, Status: "active", AmountTotal: 1000, AmountUsed: 100, PeriodAmount: 500, PeriodUsed: 100, StartTime: now - 2*86400, EndTime: now + 86400, NextResetTime: now - 1}
		require.NoError(t, db.Create(&sub).Error)
		var snapshots sync.WaitGroup
		snapshots.Add(8)
		const callback = "test:reset-candidate-snapshots"
		require.NoError(t, db.Callback().Query().After("gorm:after_query").Register(callback, func(tx *gorm.DB) {
			if _, ok := tx.Statement.Dest.(*[]commerceschema.UserSubscription); ok {
				snapshots.Done()
				snapshots.Wait()
			}
		}))
		defer db.Callback().Query().Remove(callback)
		counts := make(chan int, 8)
		results := make(chan error, 8)
		var workers sync.WaitGroup
		for range 8 {
			workers.Go(func() {
				n, err := ResetDueSubscriptions(10)
				counts <- n
				results <- err
			})
		}
		workers.Wait()
		close(counts)
		close(results)
		total := 0
		for n := range counts {
			total += n
		}
		for err := range results {
			require.NoError(t, err)
		}
		require.Equal(t, 1, total)
		require.NoError(t, db.First(&sub, sub.Id).Error)
		require.Greater(t, sub.NextResetTime, now)
		require.Zero(t, sub.PeriodUsed)
	})
	fmt.Println("PASS: PostgreSQL 8 concurrent group settlements grant once; refund locking excludes grants; 8 periodic reset workers apply once after a shared stale candidate snapshot")
}
