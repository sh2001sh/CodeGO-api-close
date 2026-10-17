package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func co04PostgresDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("CO04_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CO04_TEST_POSTGRES_DSN requires a dedicated codego_co04_ database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	var database string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&database).Error)
	require.True(t, strings.HasPrefix(database, "codego_co04_"), "dedicated CO04 test database required")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(20)
	oldDB, oldLog := platformdb.DB, platformdb.LogDB
	oldPG, oldSQLite, oldMySQL, oldRedis := platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled
	oldZeroHourReady := zeroHourStateTableReady.Load()
	platformdb.DB, platformdb.LogDB = db, db
	platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled = true, false, false, false
	zeroHourStateTableReady.Store(false)
	t.Cleanup(func() {
		platformdb.DB, platformdb.LogDB = oldDB, oldLog
		platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled = oldPG, oldSQLite, oldMySQL, oldRedis
		zeroHourStateTableReady.Store(oldZeroHourReady)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&commerceschema.BlindBoxOrder{}, &commerceschema.BlindBoxOpenRecord{}, &commerceschema.BlindBoxPityState{}, &commerceschema.BlindBoxZeroHourState{}, &commerceschema.BlindBoxProp{}, &commerceschema.InvoiceRequest{}, &billingschema.PointAccount{}, &billingschema.PointLedger{}, &billingschema.BonusQuotaCredit{}))
	return db
}

func TestCO04PostgresConcurrentBlindBoxOrders(t *testing.T) {
	db := co04PostgresDB(t)
	setting := blindboxsettings.Get()
	t.Cleanup(func() { blindboxsettings.Set(setting) })
	configured := setting
	configured.Enabled, configured.SubscriptionPrizeProbability = true, 0
	configured.PityThreshold, configured.DailyOpenLimit = 1000000, 1000000
	configured.FirstPurchaseGuaranteeUSD = 0
	configured.Tiers = []blindboxsettings.TierSetting{{Name: "九折充值卡", Probability: 1, RewardType: commerceschema.BlindBoxRewardTypeProp}}
	configured.BalanceBlindBoxTiers = configured.Tiers
	blindboxsettings.Set(configured)
	for _, sameOrder := range []bool{true, false} {
		t.Run(fmt.Sprintf("same_order_%t", sameOrder), func(t *testing.T) {
			const workers = 8
			userID := int(time.Now().UnixNano() % 1000000000)
			// Suppress the independent random zero-hour prize without creating
			// either counter: different first orders must create both safely.
			require.NoError(t, db.Create(&commerceschema.BlindBoxProp{UserId: userID, PropType: commerceschema.BlindBoxPropTypeZeroHourMultiplier, Status: commerceschema.BlindBoxPropStatusAvailable}).Error)
			orders := make([]commerceschema.BlindBoxOrder, workers)
			for i := range orders {
				orders[i] = commerceschema.BlindBoxOrder{UserId: userID, Quantity: 1, Money: 1, Source: commerceschema.BlindBoxOrderSourcePurchase, Status: constant.TopUpStatusSuccess, TradeNo: fmt.Sprintf("co04-%d-%d", userID, i)}
			}
			if sameOrder {
				orders = orders[:1]
			}
			require.NoError(t, db.Create(&orders).Error)
			start := make(chan struct{})
			type outcome struct {
				count int
				err   error
			}
			results := make(chan outcome, workers)
			for i := 0; i < workers; i++ {
				trade := orders[i%len(orders)].TradeNo
				go func() {
					<-start
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					var records []commerceschema.BlindBoxOpenRecord
					err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
						var err error
						records, err = OpenBlindBoxOrderByTradeNoTx(tx, trade)
						return err
					})
					results <- outcome{len(records), err}
				}()
			}
			close(start)
			opened := 0
			errors := make([]error, workers)
			for i := 0; i < workers; i++ {
				result := <-results
				errors[i] = result.err
				opened += result.count
			}
			for _, err := range errors {
				require.NoError(t, err)
			}
			require.Equal(t, len(orders), opened)
			var count int64
			require.NoError(t, db.Model(&commerceschema.BlindBoxOpenRecord{}).Where("user_id = ?", userID).Count(&count).Error)
			require.EqualValues(t, len(orders), count)
			var pity commerceschema.BlindBoxPityState
			require.NoError(t, db.Where("user_id = ?", userID).First(&pity).Error)
			require.Equal(t, len(orders), pity.ConsecutiveLowRewards)
			var zero commerceschema.BlindBoxZeroHourState
			require.NoError(t, db.Where("user_id = ?", userID).First(&zero).Error)
			require.EqualValues(t, int64(len(orders))*zeroHourProgressPerPaidOpen, zero.Points)
			for _, order := range orders {
				require.NoError(t, db.First(&order, order.Id).Error)
				require.Equal(t, 1, order.OpenedCount)
			}
			_, err := OpenBlindBoxes(userID, 1)
			require.Error(t, err, "exhausted inventory must not produce another prize")
		})
	}
}

func TestCO04PostgresPointAndBonusReadModifyWrite(t *testing.T) {
	db := co04PostgresDB(t)
	const workers = 8
	userID := int(time.Now().UnixNano() % 1000000000)
	require.NoError(t, db.Create(&billingschema.PointAccount{UserId: userID}).Error)
	require.NoError(t, db.Create(&billingschema.BonusQuotaCredit{UserId: userID, OriginalAmount: 100, RemainingAmount: 100, SourceType: "co04", IdempotencyKey: fmt.Sprintf("co04-bonus-%d", userID), Status: billingschema.BonusQuotaStatusActive}).Error)
	start, results := make(chan struct{}), make(chan error, workers)
	for i := 0; i < workers; i++ {
		key := fmt.Sprintf("co04-points-%d-%d", userID, i)
		go func() {
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			results <- db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				_, _, err := billingapp.AddPointLedgerTx(tx, userID, billingschema.PointLedgerTypeEarn, 1, "co04", key, key, "")
				return err
			})
		}()
	}
	close(start)
	errors := make([]error, workers)
	for i := 0; i < workers; i++ {
		errors[i] = <-results
	}
	for _, err := range errors {
		require.NoError(t, err)
	}
	var account billingschema.PointAccount
	require.NoError(t, db.Where("user_id = ?", userID).First(&account).Error)
	require.EqualValues(t, workers, account.Balance)
	var ledgers int64
	require.NoError(t, db.Model(&billingschema.PointLedger{}).Where("user_id = ?", userID).Count(&ledgers).Error)
	require.EqualValues(t, workers, ledgers)
	// Use separate transactions: the point-account lock must not serialize
	// bonus consumption and accidentally hide a missing bonus-credit lock.
	bonusStart := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() {
			<-bonusStart
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			results <- db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				return billingapp.ConsumeBonusWalletQuotaCreditsTx(tx, userID, 20)
			})
		}()
	}
	close(bonusStart)
	for i := 0; i < workers; i++ {
		errors[i] = <-results
	}
	for _, err := range errors {
		require.NoError(t, err)
	}
	var bonus billingschema.BonusQuotaCredit
	require.NoError(t, db.Where("user_id = ?", userID).First(&bonus).Error)
	require.Zero(t, bonus.RemainingAmount)
	require.Equal(t, billingschema.BonusQuotaStatusExhausted, bonus.Status)
	err := db.Transaction(func(tx *gorm.DB) error {
		_, _, err := billingapp.AddPointLedgerTx(tx, userID, billingschema.PointLedgerTypeSpend, -workers-1, "co04", "overspend", fmt.Sprintf("co04-overspend-%d", userID), "")
		return err
	})
	require.ErrorContains(t, err, "insufficient")
	require.NoError(t, db.Where("user_id = ?", userID).First(&account).Error)
	require.EqualValues(t, workers, account.Balance)
}

func TestCO04PostgresBonusReloadsAfterConcurrentConsumption(t *testing.T) {
	db := co04PostgresDB(t)
	userID := int(time.Now().UnixNano() % 1000000000)
	bonus := billingschema.BonusQuotaCredit{UserId: userID, OriginalAmount: 100, RemainingAmount: 100, SourceType: "co04", IdempotencyKey: fmt.Sprintf("co04-blocked-bonus-%d", userID), Status: billingschema.BonusQuotaStatusActive}
	require.NoError(t, db.Create(&bonus).Error)
	blocker := db.Begin()
	require.NoError(t, blocker.Error)
	t.Cleanup(func() { _ = blocker.Rollback().Error })
	require.NoError(t, blocker.Clauses(clause.Locking{Strength: "UPDATE"}).First(&bonus, bonus.Id).Error)
	var blockerPID int
	require.NoError(t, blocker.Raw("SELECT pg_backend_pid()").Scan(&blockerPID).Error)
	require.NoError(t, blocker.Model(&bonus).Update("remaining_amount", 40).Error)
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		done <- db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return billingapp.ConsumeBonusWalletQuotaCreditsTx(tx, userID, 20)
		})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var blocked int64
		require.NoError(t, db.Raw("SELECT COUNT(*) FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid))", blockerPID).Scan(&blocked).Error)
		if blocked > 0 {
			break
		}
		require.True(t, time.Now().Before(deadline), "independent bonus connection never waited for the blocker")
		time.Sleep(10 * time.Millisecond)
	}
	require.NoError(t, blocker.Commit().Error)
	require.NoError(t, <-done)
	require.NoError(t, db.First(&bonus, bonus.Id).Error)
	require.EqualValues(t, 20, bonus.RemainingAmount)
}

func TestCO04PostgresInvoiceReloadsAfterConcurrentArchive(t *testing.T) {
	db := co04PostgresDB(t)
	invoice := commerceschema.InvoiceRequest{UserID: 1, SourceType: "topup", TradeNo: fmt.Sprintf("co04-invoice-%d", time.Now().UnixNano()), InvoiceType: "personal", Title: "test", Email: "test@example.invalid", Status: "pending"}
	require.NoError(t, db.Create(&invoice).Error)
	blocker := db.Begin()
	require.NoError(t, blocker.Error)
	t.Cleanup(func() { _ = blocker.Rollback().Error })
	require.NoError(t, blocker.Clauses(clause.Locking{Strength: "UPDATE"}).First(&invoice, invoice.ID).Error)
	var blockerPID int
	require.NoError(t, blocker.Raw("SELECT pg_backend_pid()").Scan(&blockerPID).Error)
	// The concurrent archive must become visible before the administrator
	// evaluates immutability; locking only the later UPDATE is too late.
	require.NoError(t, blocker.Model(&invoice).Update("remark", `{"commercial_invoice_version":1}`).Error)
	done := make(chan error, 1)
	go func() {
		_, err := UpdateAdminInvoiceRequest(invoice.ID, 1, UpdateInvoiceRequestInput{Status: "issued", InvoiceNumber: "co04"})
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var blocked int64
		require.NoError(t, db.Raw("SELECT COUNT(*) FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid))", blockerPID).Scan(&blocked).Error)
		if blocked > 0 {
			break
		}
		require.True(t, time.Now().Before(deadline), "independent invoice connection never waited for the blocker")
		time.Sleep(10 * time.Millisecond)
	}
	require.NoError(t, blocker.Commit().Error)
	require.ErrorIs(t, <-done, ErrCommercialInvoiceConflict)
	require.NoError(t, db.First(&invoice, invoice.ID).Error)
	require.Equal(t, "pending", invoice.Status)
}
