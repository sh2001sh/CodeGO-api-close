package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/constant"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func welfarePostgresDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("WELFARE_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("CO01_TEST_DSN")
	}
	if dsn == "" {
		t.Skip("WELFARE_TEST_DSN requires a dedicated codego_welfare_test_ database")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil || !strings.HasPrefix(config.Database, "codego_welfare_test_") {
		t.Fatal("only a dedicated codego_welfare_test_ PostgreSQL database is allowed")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("could not connect to the dedicated welfare PostgreSQL database")
	}
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(20)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	var database string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&database).Error)
	require.True(t, strings.HasPrefix(database, "codego_welfare_test_"), "dedicated welfare database required before schema writes")
	oldDB, oldLog := platformdb.DB, platformdb.LogDB
	oldPG, oldSQLite, oldMySQL, oldRedis := platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	db = db.WithContext(ctx)
	platformdb.DB, platformdb.LogDB = db, db
	platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled = true, false, false, false
	t.Cleanup(func() {
		cancel()
		platformdb.DB, platformdb.LogDB = oldDB, oldLog
		platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled = oldPG, oldSQLite, oldMySQL, oldRedis
	})
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS marketplace").Error)
	require.NoError(t, db.AutoMigrate(
		&identityschema.User{}, &auditschema.Log{}, &marketplaceschema.Channel{},
		&billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{},
		&billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{},
		&billingschema.FundingLot{}, &billingschema.FundingAllocation{},
		&commerceschema.WalletTransfer{}, &commerceschema.WalletTransferSecurity{},
		&commerceschema.BlindBoxProp{}, &commerceschema.BlindBoxPropGift{},
	))
	return db
}

func welfarePostgresFixture(t *testing.T, db *gorm.DB, initial int64) (*gin.Engine, identityschema.User, identityschema.User, string, billingschema.BillingAccount) {
	t.Helper()
	users := make([]identityschema.User, 2)
	for index := range users {
		externalID, err := identityschema.GenerateExternalUserID()
		require.NoError(t, err)
		users[index] = identityschema.User{ExternalId: externalID, Username: "welfare-" + externalID, AffCode: externalID, Status: constant.UserStatusEnabled}
		require.NoError(t, db.Create(&users[index]).Error)
	}
	sender, recipient := users[0], users[1]
	if initial > 0 {
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			return billingapp.CreditClaudeWalletQuotaTx(tx, sender.Id, int(initial), "welfare-seed:"+sender.ExternalId, "test_welfare_seed")
		}))
	}
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "claude_wallet", OwnerType: "user", OwnerID: int64(sender.Id), QuotaUnit: "quota"})
	require.NoError(t, err)
	require.NoError(t, commerceapp.ConfigureWalletTransferPassword(sender.Id, "", "Paypass123"))
	channelID := "welfare-" + sender.ExternalId
	require.NoError(t, db.Create(&marketplaceschema.Channel{ID: channelID, OwnerUserID: sender.Id, ProviderType: "openai", Status: "active"}).Error)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/welfare/:id", func(c *gin.Context) { c.Set("id", sender.Id) }, BatchWelfare)
	return engine, sender, recipient, channelID, *account
}

type welfarePostgresOutcome struct {
	success bool
	result  MarketplaceWelfareTestResult
	err     error
}

func welfarePostgresRequest(engine *gin.Engine, channelID, body string) welfarePostgresOutcome {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/welfare/"+channelID, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)
	var envelope struct {
		Success bool                         `json:"success"`
		Data    MarketplaceWelfareTestResult `json:"data"`
	}
	err := json.Unmarshal(recorder.Body.Bytes(), &envelope)
	if recorder.Code != http.StatusOK {
		err = fmt.Errorf("unexpected HTTP status %d", recorder.Code)
	}
	return welfarePostgresOutcome{success: envelope.Success, result: envelope.Data, err: err}
}

func concurrentWelfarePostgresRequests(t *testing.T, db *gorm.DB, senderID int, engine *gin.Engine, channelID string, body func(int) string) []welfarePostgresOutcome {
	t.Helper()
	// Hold the actual sender row until all eight handlers are observed waiting
	// for it, so a sequential run cannot accidentally satisfy the assertions.
	blocker := db.Begin()
	require.NoError(t, blocker.Error)
	defer blocker.Rollback()
	var sender identityschema.User
	require.NoError(t, blocker.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sender, senderID).Error)
	const workers = 8
	start, outcomes := make(chan struct{}), make(chan welfarePostgresOutcome, workers)
	for worker := range workers {
		go func() { <-start; outcomes <- welfarePostgresRequest(engine, channelID, body(worker)) }()
	}
	close(start)
	overlapped := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var blocked int64
		require.NoError(t, db.Raw("SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock' AND query LIKE '%users%'").Scan(&blocked).Error)
		if blocked >= workers {
			overlapped = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NoError(t, blocker.Rollback().Error)
	results := make([]welfarePostgresOutcome, workers)
	for worker := range workers {
		results[worker] = <-outcomes
	}
	require.True(t, overlapped, "eight handlers must overlap while waiting on the real PostgreSQL sender row")
	return results
}

func TestWelfarePostgresSecurity(t *testing.T) {
	db := welfarePostgresDB(t)
	unit := int64(platformruntime.QuotaPerUnit)
	require.Positive(t, unit)
	fee := (unit + 99) / 100
	t.Run("eight identical batches debit once", func(t *testing.T) {
		engine, sender, recipient, channelID, account := welfarePostgresFixture(t, db, 10*unit)
		body := fmt.Sprintf(`{"user_ids":[%q],"type":"transfer","amount":%d,"request_id":"same-batch","payment_password":"Paypass123"}`, recipient.ExternalId, unit)
		for _, outcome := range concurrentWelfarePostgresRequests(t, db, sender.Id, engine, channelID, func(int) string { return body }) {
			require.NoError(t, outcome.err)
			require.True(t, outcome.success)
			require.Equal(t, 1, outcome.result.SuccessCount)
			require.Zero(t, outcome.result.FailedCount)
		}
		var snapshot billingschema.BillingBalanceSnapshot
		require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
		require.Equal(t, 10*unit-unit-fee, snapshot.AvailableBalance)
		recipientBalance, err := billingapp.GetUserClaudeWalletQuota(recipient.Id)
		require.NoError(t, err)
		require.EqualValues(t, unit, recipientBalance)
		var count int64
		require.NoError(t, db.Model(&commerceschema.WalletTransfer{}).Where("sender_user_id = ?", sender.Id).Count(&count).Error)
		require.EqualValues(t, 1, count)
		// Debit helpers represent principal and fee as two settled reservations;
		// their hold entries carry the native reservation_hold reason code.
		require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ? AND direction = ?", account.AccountID, billingschema.BillingDirectionDebit).Count(&count).Error)
		require.EqualValues(t, 2, count)
		require.Equal(t, unit+fee, snapshot.ConsumedTotal)
	})
	t.Run("different batches cannot spend reserved funds", func(t *testing.T) {
		initial, reserved := 4*unit, 2*unit
		engine, sender, recipient, channelID, account := welfarePostgresFixture(t, db, initial)
		reservation, err := billingdomain.CreateReservation(billingdomain.CreateReservationParams{AccountID: account.AccountID, RequestID: "welfare-hold:" + sender.ExternalId, ReservedAmount: reserved, IdempotencyKey: "welfare-hold:" + sender.ExternalId})
		require.NoError(t, err)
		outcomes := concurrentWelfarePostgresRequests(t, db, sender.Id, engine, channelID, func(worker int) string {
			return fmt.Sprintf(`{"user_ids":[%q],"type":"transfer","amount":%d,"request_id":"batch-%d","payment_password":"Paypass123"}`, recipient.ExternalId, unit, worker)
		})
		successes, failures := 0, 0
		for _, outcome := range outcomes {
			require.NoError(t, outcome.err)
			require.True(t, outcome.success)
			successes += outcome.result.SuccessCount
			failures += outcome.result.FailedCount
		}
		require.Equal(t, 1, successes)
		require.Equal(t, 7, failures)
		var snapshot billingschema.BillingBalanceSnapshot
		require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
		require.Equal(t, initial-reserved-unit-fee, snapshot.AvailableBalance)
		require.Equal(t, reserved, snapshot.ReservedBalance)
		require.Equal(t, initial, snapshot.AvailableBalance+snapshot.ReservedBalance+snapshot.ConsumedTotal)
		require.NoError(t, db.First(reservation, "reservation_id = ?", reservation.ReservationID).Error)
		require.Equal(t, billingschema.BillingReservationStatusOpen, reservation.Status)
		recipientBalance, err := billingapp.GetUserClaudeWalletQuota(recipient.Id)
		require.NoError(t, err)
		require.EqualValues(t, unit, recipientBalance)
		var count int64
		require.NoError(t, db.Model(&commerceschema.WalletTransfer{}).Where("sender_user_id = ?", sender.Id).Count(&count).Error)
		require.EqualValues(t, 1, count)
	})
	t.Run("gift replay and insufficient inventory preserve ownership", func(t *testing.T) {
		engine, sender, recipient, channelID, _ := welfarePostgresFixture(t, db, 0)
		for range 3 {
			require.NoError(t, db.Create(&commerceschema.BlindBoxProp{UserId: sender.Id, Status: commerceschema.BlindBoxPropStatusAvailable, Title: "welfare test", PropType: "discount"}).Error)
		}
		body := fmt.Sprintf(`{"user_ids":[%q],"type":"blind_box","amount":2,"request_id":"same-gift"}`, recipient.ExternalId)
		for _, outcome := range concurrentWelfarePostgresRequests(t, db, sender.Id, engine, channelID, func(int) string { return body }) {
			require.NoError(t, outcome.err)
			require.True(t, outcome.success)
			require.Equal(t, 1, outcome.result.SuccessCount)
		}
		// One prop remains: transferring two must roll back its first slot too.
		insufficient := welfarePostgresRequest(engine, channelID, strings.Replace(body, "same-gift", "insufficient-gift", 1))
		require.NoError(t, insufficient.err)
		require.True(t, insufficient.success)
		require.Equal(t, 1, insufficient.result.FailedCount)
		var senderCount, recipientCount, gifts int64
		require.NoError(t, db.Model(&commerceschema.BlindBoxProp{}).Where("user_id = ?", sender.Id).Count(&senderCount).Error)
		require.NoError(t, db.Model(&commerceschema.BlindBoxProp{}).Where("user_id = ?", recipient.Id).Count(&recipientCount).Error)
		require.NoError(t, db.Model(&commerceschema.BlindBoxPropGift{}).Where("sender_user_id = ?", sender.Id).Count(&gifts).Error)
		require.EqualValues(t, 1, senderCount)
		require.EqualValues(t, 2, recipientCount)
		require.EqualValues(t, 2, gifts)
	})
}
