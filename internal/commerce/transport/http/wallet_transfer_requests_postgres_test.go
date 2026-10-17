package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func setupWalletTransferHTTPPostgres(t *testing.T) (*gorm.DB, *gin.Engine, identityschema.User, identityschema.User, commerceapp.CreateWalletTransferRequest, map[int]*stdhttp.Cookie) {
	t.Helper()
	dsn := os.Getenv("WALLET_HTTP_TEST_DSN")
	if dsn == "" {
		t.Skip("WALLET_HTTP_TEST_DSN requires a dedicated codego_wallet_test_http_ database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	var name string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&name).Error)
	require.True(t, strings.HasPrefix(name, "codego_wallet_test_http_"), "isolated HTTP test database required")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(20)
	beforeDB, beforeLog := platformdb.DB, platformdb.LogDB
	beforePG, beforeSQLite, beforeMySQL := platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL
	beforeRedis, beforeRate := platformcache.RedisEnabled, platformconfig.CriticalRateLimitEnable
	platformdb.DB, platformdb.LogDB = db, db
	platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL = true, false, false
	platformcache.RedisEnabled, platformconfig.CriticalRateLimitEnable = false, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.LogDB = beforeDB, beforeLog
		platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL = beforePG, beforeSQLite, beforeMySQL
		platformcache.RedisEnabled, platformconfig.CriticalRateLimitEnable = beforeRedis, beforeRate
		require.NoError(t, sqlDB.Close())
	})
	previousRequests := walletTransferRequests
	walletTransferRequests = &walletTransferRequestSet{active: make(map[int]*walletTransferOperation), recent: make(map[walletTransferRequestKey]*walletTransferOperation)}
	requests := walletTransferRequests
	t.Cleanup(func() {
		require.Eventually(t, func() bool {
			requests.mu.Lock()
			defer requests.mu.Unlock()
			return len(requests.active) == 0
		}, 5*time.Second, 10*time.Millisecond)
		walletTransferRequests = previousRequests
	})
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &auditschema.Log{}, &commerceschema.WalletTransfer{}, &commerceschema.WalletTransferSecurity{},
		&billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{},
		&billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{}, &billingschema.FundingLot{}, &billingschema.FundingAllocation{}))
	identity := fmt.Sprintf("http%d", time.Now().UnixNano())
	from, err := identityschema.GenerateExternalUserID()
	require.NoError(t, err)
	to, err := identityschema.GenerateExternalUserID()
	require.NoError(t, err)
	sender := identityschema.User{Username: identity, ExternalId: from, AffCode: identity, Status: constant.UserStatusEnabled, Role: constant.RoleCommonUser}
	recipient := identityschema.User{Username: identity + "r", ExternalId: to, AffCode: identity + "r", Status: constant.UserStatusEnabled, Role: constant.RoleCommonUser}
	require.NoError(t, db.Create(&sender).Error)
	require.NoError(t, db.Create(&recipient).Error)
	require.NoError(t, commerceapp.ConfigureWalletTransferPassword(sender.Id, "", "Paypass123"))
	unit := int64(platformruntime.QuotaPerUnit)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return billingapp.CreditClaudeWalletQuotaTx(tx, sender.Id, int(10*unit), identity+":fund", "redemption_credit")
	}))
	req := commerceapp.CreateWalletTransferRequest{RecipientExternalId: to, AmountQuota: unit, RequestId: identity + ":transfer", PaymentPassword: "Paypass123"}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("wallet-test", cookie.NewStore([]byte("isolated-wallet-http-session-key-32"))))
	// Test-only session creation; all tested financial paths use the actual
	// registered UserAuth middleware and freshly loaded PostgreSQL identities.
	router.GET("/test-session/:id", func(c *gin.Context) {
		user := sender
		if c.Param("id") == strconv.Itoa(recipient.Id) {
			user = recipient
		}
		session := sessions.Default(c)
		session.Set("username", user.Username)
		session.Set("id", user.Id)
		session.Set("role", user.Role)
		session.Set("status", user.Status)
		session.Set("group", user.Group)
		require.NoError(t, session.Save())
		c.Status(stdhttp.StatusNoContent)
	})
	RegisterCommerceRoutes(router.Group("/api"), func(c *gin.Context) { c.Next() })
	cookies := make(map[int]*stdhttp.Cookie)
	for _, user := range []identityschema.User{sender, recipient} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(stdhttp.MethodGet, "/test-session/"+strconv.Itoa(user.Id), nil))
		require.NotEmpty(t, recorder.Result().Cookies())
		cookies[user.Id] = recorder.Result().Cookies()[0]
	}
	return db, router, sender, recipient, req, cookies
}

func callWalletTransferHTTP(t *testing.T, router *gin.Engine, method, path string, body any, userID int, cookie *stdhttp.Cookie, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		require.NoError(t, err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(data)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("New-Api-User", strconv.Itoa(userID))
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func assertWalletTransferHTTPConservation(t *testing.T, db *gorm.DB, sender, recipient identityschema.User, req commerceapp.CreateWalletTransferRequest) {
	t.Helper()
	var senderAccount, recipientAccount billingschema.BillingAccount
	require.NoError(t, db.Where("owner_id = ? AND account_type = ?", sender.Id, "claude_wallet").First(&senderAccount).Error)
	require.NoError(t, db.Where("owner_id = ? AND account_type = ?", recipient.Id, "claude_wallet").First(&recipientAccount).Error)
	var from, to billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", senderAccount.AccountID).First(&from).Error)
	require.NoError(t, db.Where("account_id = ?", recipientAccount.AccountID).First(&to).Error)
	fee := (req.AmountQuota + 99) / 100
	require.Equal(t, 10*req.AmountQuota-req.AmountQuota-fee, from.AvailableBalance)
	require.Equal(t, req.AmountQuota+fee, from.ConsumedTotal)
	require.Zero(t, from.ReservedBalance)
	require.Equal(t, req.AmountQuota, to.AvailableBalance)
	var receipts, credits, debits int64
	require.NoError(t, db.Model(&commerceschema.WalletTransfer{}).Where("request_id = ?", req.RequestId).Count(&receipts).Error)
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("idempotency_key = ?", "wallet-transfer:"+req.RequestId+":credit").Count(&credits).Error)
	require.NoError(t, db.Model(&billingschema.BillingSettlement{}).Where("idempotency_key IN ?", []string{"ledger-settlement:wallet-transfer:" + req.RequestId + ":debit", "ledger-settlement:wallet-transfer:" + req.RequestId + ":fee"}).Count(&debits).Error)
	require.EqualValues(t, 1, receipts)
	require.EqualValues(t, 1, credits)
	require.EqualValues(t, 2, debits)
	var remaining int64
	require.NoError(t, db.Model(&billingschema.FundingLot{}).Select("COALESCE(SUM(remaining_amount),0)").Where("account_id = ?", senderAccount.AccountID).Scan(&remaining).Error)
	require.Equal(t, from.AvailableBalance, remaining, "native sender lots retain exact debit/fee conservation")
}

func TestWalletTransferHTTPPostgresLongRequestDisconnectAndDuplicate(t *testing.T) {
	db, router, sender, recipient, req, cookies := setupWalletTransferHTTPPostgres(t)
	path := "/api/wallet/transfers/requests/" + req.RequestId
	unauthorized := callWalletTransferHTTP(t, router, stdhttp.MethodGet, path, nil, sender.Id, nil, context.Background())
	require.Equal(t, stdhttp.StatusUnauthorized, unauthorized.Code)
	blocker := db.Begin()
	require.NoError(t, blocker.Error)
	t.Cleanup(func() { _ = blocker.Rollback().Error })
	var user identityschema.User
	require.NoError(t, blocker.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, sender.Id).Error)
	var pid int
	require.NoError(t, blocker.Raw("SELECT pg_backend_pid()").Scan(&pid).Error)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	responses := make(chan *httptest.ResponseRecorder, 1)
	started := time.Now()
	go func() {
		responses <- callWalletTransferHTTP(t, router, stdhttp.MethodPost, "/api/wallet/transfers", req, sender.Id, cookies[sender.Id], ctx)
	}()
	require.Eventually(t, func() bool {
		var count int64
		return db.Raw("SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND ? = ANY(pg_blocking_pids(pid))", pid).Scan(&count).Error == nil && count == 1
	}, 4*time.Second, 10*time.Millisecond, "native transaction must actually wait on the real sender row")
	operation := walletTransferRequests.lookup(sender.Id, req.RequestId)
	require.NotNil(t, operation)
	first := <-responses
	require.Equal(t, stdhttp.StatusAccepted, first.Code, first.Body.String())
	require.Contains(t, first.Body.String(), "TRANSFER_PROCESSING")
	require.Contains(t, first.Body.String(), req.RequestId)
	require.Less(t, time.Since(started), 3*time.Second)
	cancel() // The HTTP request ends; the admitted native transaction survives.
	var duplicates sync.WaitGroup
	for range 8 {
		duplicates.Go(func() {
			response := callWalletTransferHTTP(t, router, stdhttp.MethodPost, "/api/wallet/transfers", req, sender.Id, cookies[sender.Id], context.Background())
			require.Equal(t, stdhttp.StatusAccepted, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), "TRANSFER_PROCESSING")
		})
	}
	duplicates.Wait()
	for _, change := range []func(*commerceapp.CreateWalletTransferRequest){
		func(req *commerceapp.CreateWalletTransferRequest) { req.RequestId += "-other" },
		func(req *commerceapp.CreateWalletTransferRequest) { req.AmountQuota *= 2 },
		func(req *commerceapp.CreateWalletTransferRequest) { req.RecipientExternalId = sender.ExternalId },
	} {
		other := req
		change(&other)
		response := callWalletTransferHTTP(t, router, stdhttp.MethodPost, "/api/wallet/transfers", other, sender.Id, cookies[sender.Id], context.Background())
		require.Equal(t, stdhttp.StatusTooManyRequests, response.Code)
		require.Contains(t, response.Body.String(), "TRANSFER_BUSY")
	}
	foreign := callWalletTransferHTTP(t, router, stdhttp.MethodGet, path, nil, recipient.Id, cookies[recipient.Id], context.Background())
	require.Equal(t, stdhttp.StatusAccepted, foreign.Code)
	require.Contains(t, foreign.Body.String(), "TRANSFER_UNCONFIRMED")
	require.NotContains(t, foreign.Body.String(), "amount_quota")
	spoofed := callWalletTransferHTTP(t, router, stdhttp.MethodGet, path, nil, sender.Id, cookies[recipient.Id], context.Background())
	require.Equal(t, stdhttp.StatusUnauthorized, spoofed.Code)
	// Emulate a different process with no in-memory admission knowledge.
	set := walletTransferRequests
	walletTransferRequests = &walletTransferRequestSet{active: make(map[int]*walletTransferOperation), recent: make(map[walletTransferRequestKey]*walletTransferOperation)}
	otherInstance := callWalletTransferHTTP(t, router, stdhttp.MethodGet, path, nil, sender.Id, cookies[sender.Id], context.Background())
	walletTransferRequests = set
	require.Equal(t, stdhttp.StatusAccepted, otherInstance.Code)
	require.Contains(t, otherInstance.Body.String(), "TRANSFER_UNCONFIRMED")
	// Cross the old 20-second native ceiling while the database lock is held.
	if remaining := 21*time.Second - time.Since(started); remaining > 0 {
		<-time.After(remaining)
	}
	pending := callWalletTransferHTTP(t, router, stdhttp.MethodGet, path, nil, sender.Id, cookies[sender.Id], context.Background())
	require.Equal(t, stdhttp.StatusAccepted, pending.Code)
	require.Contains(t, pending.Body.String(), "TRANSFER_PROCESSING")
	require.NoError(t, blocker.Commit().Error)
	select {
	case <-operation.done:
	case <-time.After(5 * time.Second):
		t.Fatal("native transfer did not finish after row lock release")
	}
	require.NoError(t, operation.err)
	complete := callWalletTransferHTTP(t, router, stdhttp.MethodGet, path, nil, sender.Id, cookies[sender.Id], context.Background())
	require.Equal(t, stdhttp.StatusOK, complete.Code, complete.Body.String())
	var envelope struct {
		Success bool                                  `json:"success"`
		Data    commerceapp.WalletTransferHistoryItem `json:"data"`
	}
	require.NoError(t, json.Unmarshal(complete.Body.Bytes(), &envelope))
	require.True(t, envelope.Success)
	require.Equal(t, req.RequestId, envelope.Data.RequestId)
	require.Equal(t, "outgoing", envelope.Data.Direction)
	require.Equal(t, "completed", envelope.Data.Status)
	require.Equal(t, recipient.ExternalId, envelope.Data.CounterpartyExternalId)
	require.Equal(t, req.AmountQuota, envelope.Data.AmountQuota)
	for range 3 {
		replayed := callWalletTransferHTTP(t, router, stdhttp.MethodPost, "/api/wallet/transfers", req, sender.Id, cookies[sender.Id], context.Background())
		require.Equal(t, stdhttp.StatusOK, replayed.Code)
	}
	assertWalletTransferHTTPConservation(t, db, sender, recipient, req)
	t.Log("actual UserAuth API: 1s accepted; native PG lock survived 21s and disconnect; 8 duplicate POSTs shared one receipt; debit/fee/lots conserved")
}

type walletHTTPUnknownCommitPool struct {
	*sql.DB
	commits atomic.Int32
}

func (pool *walletHTTPUnknownCommitPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := pool.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &walletHTTPUnknownCommitTx{Tx: tx, pool: pool}, nil
}

type walletHTTPUnknownCommitTx struct {
	*sql.Tx
	pool *walletHTTPUnknownCommitPool
}

func (tx *walletHTTPUnknownCommitTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	if tx.pool.commits.Add(1) == 2 {
		return io.EOF
	}
	return nil
}

func TestWalletTransferHTTPPostgresUnknownCommitOnlyQueriesNativeReceipt(t *testing.T) {
	db, router, sender, recipient, req, cookies := setupWalletTransferHTTPPostgres(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	pool := &walletHTTPUnknownCommitPool{DB: sqlDB}
	wrapped := db.Session(&gorm.Session{Context: context.Background()})
	wrapped.Statement.ConnPool, wrapped.Config.ConnPool = pool, pool
	platformdb.DB = wrapped
	response := callWalletTransferHTTP(t, router, stdhttp.MethodPost, "/api/wallet/transfers", req, sender.Id, cookies[sender.Id], context.Background())
	require.Equal(t, stdhttp.StatusServiceUnavailable, response.Code)
	require.Contains(t, response.Body.String(), "TRANSFER_UNCONFIRMED")
	require.NotContains(t, response.Body.String(), "EOF")
	require.EqualValues(t, 2, pool.commits.Load())
	replayed := callWalletTransferHTTP(t, router, stdhttp.MethodPost, "/api/wallet/transfers", req, sender.Id, cookies[sender.Id], context.Background())
	require.Equal(t, stdhttp.StatusServiceUnavailable, replayed.Code)
	require.EqualValues(t, 2, pool.commits.Load(), "unknown commit must not trigger another transaction")
	path := "/api/wallet/transfers/requests/" + req.RequestId
	for range 3 {
		found := callWalletTransferHTTP(t, router, stdhttp.MethodGet, path, nil, sender.Id, cookies[sender.Id], context.Background())
		require.Equal(t, stdhttp.StatusOK, found.Code, found.Body.String())
		require.Contains(t, found.Body.String(), `"status":"completed"`)
	}
	require.EqualValues(t, 2, pool.commits.Load(), "GET must not execute any transaction")
	assertWalletTransferHTTPConservation(t, db, sender, recipient, req)
	t.Log("real financial COMMIT succeeded; injected acknowledgement EOF produced unconfirmed; repeated POST did not reexecute; GET resolved immutable native receipt")
}

func TestWalletTransferHTTPPostgresRejectAndUnavailableRemainDistinct(t *testing.T) {
	db, router, sender, _, req, cookies := setupWalletTransferHTTPPostgres(t)
	wrong := req
	wrong.PaymentPassword = "wrong-password"
	response := callWalletTransferHTTP(t, router, stdhttp.MethodPost, "/api/wallet/transfers", wrong, sender.Id, cookies[sender.Id], context.Background())
	require.Equal(t, stdhttp.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "TRANSFER_REJECTED")
	path := "/api/wallet/transfers/requests/" + req.RequestId
	rejected := callWalletTransferHTTP(t, router, stdhttp.MethodGet, path, nil, sender.Id, cookies[sender.Id], context.Background())
	require.Equal(t, stdhttp.StatusOK, rejected.Code)
	require.Contains(t, rejected.Body.String(), `"status":"rejected"`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getContext, recorder := newCommerceContext(t, stdhttp.MethodGet, path, nil, sender.Id)
	getContext.Params = gin.Params{{Key: "request_id", Value: req.RequestId}}
	getContext.Request = getContext.Request.WithContext(ctx)
	getWalletTransferRequest(getContext)
	require.Equal(t, stdhttp.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Body.String(), "TRANSFER_UNCONFIRMED")
	require.NotContains(t, recorder.Body.String(), "context canceled")
	var count int64
	require.NoError(t, db.Model(&commerceschema.WalletTransfer{}).Where("request_id = ?", req.RequestId).Count(&count).Error)
	require.Zero(t, count)
	// An explicit corrected request with the same original parameters may retry
	// a known nonfinancial rejection; the handler never initiates this itself.
	corrected := callWalletTransferHTTP(t, router, stdhttp.MethodPost, "/api/wallet/transfers", req, sender.Id, cookies[sender.Id], context.Background())
	require.Equal(t, stdhttp.StatusOK, corrected.Code)
	require.True(t, decodeCommerceResponse(t, corrected).Success)
}

func TestWalletTransferHTTPPostgresDelayedRejectionRollsBackAllNativeMoney(t *testing.T) {
	db, router, sender, _, req, cookies := setupWalletTransferHTTPPostgres(t)
	req.AmountQuota *= 20
	var beforeEntries, beforeLots, beforeAccounts int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Count(&beforeEntries).Error)
	require.NoError(t, db.Model(&billingschema.FundingLot{}).Count(&beforeLots).Error)
	require.NoError(t, db.Model(&billingschema.BillingAccount{}).Count(&beforeAccounts).Error)
	blocker := db.Begin()
	require.NoError(t, blocker.Error)
	t.Cleanup(func() { _ = blocker.Rollback().Error })
	var locked identityschema.User
	require.NoError(t, blocker.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, sender.Id).Error)
	response := callWalletTransferHTTP(t, router, stdhttp.MethodPost, "/api/wallet/transfers", req, sender.Id, cookies[sender.Id], context.Background())
	require.Equal(t, stdhttp.StatusAccepted, response.Code)
	op := walletTransferRequests.lookup(sender.Id, req.RequestId)
	require.NotNil(t, op)
	require.NoError(t, blocker.Commit().Error)
	select {
	case <-op.done:
	case <-time.After(5 * time.Second):
		t.Fatal("delayed rejection did not complete")
	}
	require.ErrorIs(t, op.err, commerceschema.ErrWalletTransferInsufficientBalance)
	path := "/api/wallet/transfers/requests/" + req.RequestId
	result := callWalletTransferHTTP(t, router, stdhttp.MethodGet, path, nil, sender.Id, cookies[sender.Id], context.Background())
	require.Equal(t, stdhttp.StatusOK, result.Code)
	require.Contains(t, result.Body.String(), "TRANSFER_REJECTED")
	require.Contains(t, result.Body.String(), `"status":"rejected"`)
	var entries, lots, accounts, receipts int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Count(&entries).Error)
	require.NoError(t, db.Model(&billingschema.FundingLot{}).Count(&lots).Error)
	require.NoError(t, db.Model(&billingschema.BillingAccount{}).Count(&accounts).Error)
	require.NoError(t, db.Model(&commerceschema.WalletTransfer{}).Where("request_id = ?", req.RequestId).Count(&receipts).Error)
	require.Equal(t, beforeEntries, entries)
	require.Equal(t, beforeLots, lots)
	require.Equal(t, beforeAccounts, accounts)
	require.Zero(t, receipts)
	t.Log("accepted request later rejected for insufficient native balance; no ledger, funding lot, account or receipt survived rollback")
}
