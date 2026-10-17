package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Run only against a dedicated, newly created local test database. This tests
// PostgreSQL FOR UPDATE/native reservation behavior, not SQLite's lock model.
func TestRefundPostgresConcurrentCreationUsesOneProviderRequest(t *testing.T) {
	dsn := os.Getenv("REFUND_TEST_DSN")
	if dsn == "" {
		t.Skip("REFUND_TEST_DSN is not configured")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	var database string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&database).Error)
	require.True(t, strings.HasPrefix(database, "codego_refund_test_"), "dedicated refund test database required")
	oldDB, oldLogDB := platformdb.DB, platformdb.LogDB
	oldPG, oldSQLite, oldMySQL, oldRedis := platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled
	platformdb.DB, platformdb.LogDB = db, db
	platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled = true, false, false, false
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqlDB.Close()
		platformdb.DB, platformdb.LogDB = oldDB, oldLogDB
		platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled = oldPG, oldSQLite, oldMySQL, oldRedis
	})
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &commerceschema.TopUp{}, &billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{}, &billingschema.FundingLot{}, &billingschema.FundingAllocation{}))
	order, account := seedRefundWallet(t, db)
	var creates atomic.Int32
	var queries atomic.Int32
	var responseStatus atomic.Int32
	responseStatus.Store(1)
	mockRefundProvider(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/create") {
			creates.Add(1)
		} else {
			queries.Add(1)
		}
		return refundProviderResponse(t, r, "provider-paid", int(responseStatus.Load())), nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	platformdb.DB = db.WithContext(ctx)
	const workers = 8
	start := make(chan struct{})
	errors := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := createRefund(order.UserId, commerceschema.RefundRequest{OrderType: commerceschema.RefundOrderTypeBalance, TradeNo: order.TradeNo})
			errors <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errors)
	succeeded := 0
	for err := range errors {
		if err == nil {
			succeeded++
		}
	}
	require.Equal(t, 1, succeeded)
	require.EqualValues(t, 1, creates.Load())
	assertRefundWallet(t, db, account, 0, 1000, 0)
	var reservations int64
	require.NoError(t, db.Model(&billingschema.BillingReservation{}).Where("account_id = ?", account).Count(&reservations).Error)
	require.EqualValues(t, 1, reservations)
	fmt.Println("PASS: PostgreSQL 8 concurrent refund attempts produced 1 provider call and 1 durable hold; canonical wallet, user balance and exact lot are frozen")

	require.NoError(t, db.First(order, order.Id).Error)
	firstRefundNo := order.RefundNo
	responseStatus.Store(3)
	failed, err := syncRefund(order.UserId, firstRefundNo)
	require.NoError(t, err)
	require.Equal(t, commerceschema.RefundStatusFailed, failed.Status)
	assertRefundWallet(t, db, account, 1000, 0, 1000)
	queryCount := queries.Load()
	failedReplay, err := syncRefund(order.UserId, firstRefundNo)
	require.NoError(t, err)
	require.Equal(t, commerceschema.RefundStatusFailed, failedReplay.Status)
	require.Equal(t, queryCount, queries.Load(), "known terminal failure is not queried or refunded again")

	responseStatus.Store(1)
	retry, err := createRefund(order.UserId, commerceschema.RefundRequest{OrderType: commerceschema.RefundOrderTypeBalance, TradeNo: order.TradeNo})
	require.NoError(t, err)
	require.NotEqual(t, firstRefundNo, retry.RefundNo)
	require.EqualValues(t, 2, creates.Load(), "a second provider create requires a confirmed failed, released attempt")
	assertRefundWallet(t, db, account, 0, 1000, 0)
	responseStatus.Store(2)
	results := make(chan error, workers)
	start = make(chan struct{})
	for range workers {
		wg.Go(func() {
			<-start
			_, err := syncRefund(order.UserId, retry.RefundNo)
			results <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	assertRefundWallet(t, db, account, 0, 0, 0)
	var settled int64
	require.NoError(t, db.Model(&billingschema.BillingReservation{}).Where("account_id = ? AND status = ?", account, billingschema.BillingReservationStatusSettled).Count(&settled).Error)
	require.EqualValues(t, 1, settled)
	queryCount = queries.Load()
	completedReplay, err := syncRefund(order.UserId, retry.RefundNo)
	require.NoError(t, err)
	require.Equal(t, commerceschema.RefundStatusSuccess, completedReplay.Status)
	require.Equal(t, queryCount, queries.Load())
	fmt.Println("PASS: PostgreSQL explicit failure restores the exact lot, terminal replay makes no provider call, and 8 successful syncs settle one native hold")
}
