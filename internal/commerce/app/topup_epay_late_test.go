package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/sh2001sh/new-api/constant"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func configureEpayLateTest(t *testing.T) {
	t.Helper()
	confirmPaymentComplianceForTest(t)
	address, id, key, methods := commercestore.PayAddress, commercestore.EpayId, commercestore.EpayKey, commercestore.PayMethods
	commercestore.PayAddress, commercestore.EpayId, commercestore.EpayKey = "https://epay.example.test", "test-merchant", "synthetic-epay-key"
	commercestore.PayMethods = []map[string]string{{"type": "alipay"}}
	t.Cleanup(func() {
		commercestore.PayAddress, commercestore.EpayId, commercestore.EpayKey, commercestore.PayMethods = address, id, key, methods
	})
}

func seedEpayLateOrder(t *testing.T, db *gorm.DB, discount bool) (*identityschema.User, *commerceschema.TopUp, *commerceschema.BlindBoxProp) {
	t.Helper()
	user := &identityschema.User{Id: 39801, Username: "epay-late-test", AffCode: "epay-late-test", Status: constant.UserStatusEnabled, ClaudeQuota: 123}
	if platformdb.UsingPostgreSQL {
		user.Id = int(time.Now().UnixNano()%900000000) + 100000000
		user.Username = fmt.Sprintf("epay-%d", user.Id)
		user.AffCode = user.Username
	}
	require.NoError(t, db.Create(user).Error)
	var prop *commerceschema.BlindBoxProp
	if discount {
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			var err error
			prop, err = createBlindBoxPropTx(tx, user.Id, 1, "九折充值卡")
			return err
		}))
	}
	order := &commerceschema.TopUp{UserId: user.Id, Amount: 10, Money: 73, TradeNo: "epay-original-order", PaymentMethod: "alipay", PaymentProvider: commerceschema.PaymentProviderEpay, CreateTime: time.Now().Add(-time.Hour).Unix(), Status: constant.TopUpStatusPending}
	if platformdb.UsingPostgreSQL {
		order.TradeNo = fmt.Sprintf("epay-original-order-%d", user.Id)
	}
	_, err := CreatePendingTopUpOrderWithBlindBoxDiscount(order)
	require.NoError(t, err)
	return user, order, prop
}

func epayLateParams(order *commerceschema.TopUp) map[string]string {
	return map[string]string{"pid": "test-merchant", "out_trade_no": order.TradeNo, "trade_no": "original-provider-order", "type": order.PaymentMethod, "money": fmt.Sprintf("%.2f", order.Money), "trade_status": epay.StatusTradeSuccess, "name": "synthetic topup"}
}

func deliverEpayLate(params map[string]string) (bool, error) {
	return HandleEpayWebhook(epay.GenerateParams(params, "synthetic-epay-key"), "127.0.0.1")
}

func epayLateSnapshot(t *testing.T, db *gorm.DB) []any {
	t.Helper()
	var snapshot []any
	for _, model := range []any{&commerceschema.TopUp{}, &commerceschema.BlindBoxProp{}, &identityschema.User{}, &billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.FundingLot{}, &billingschema.BillingReservation{}, &billingschema.BillingOutboxEvent{}, &billingschema.FundingAllocation{}} {
		var rows []map[string]any
		require.NoError(t, db.Model(model).Find(&rows).Error)
		snapshot = append(snapshot, rows)
	}
	return snapshot
}

func assertEpayCreditedOnce(t *testing.T, db *gorm.DB, user *identityschema.User, order *commerceschema.TopUp) {
	t.Helper()
	var savedUser identityschema.User
	require.NoError(t, db.First(&savedUser, user.Id).Error)
	require.Equal(t, user.ClaudeQuota+calculateTopUpQuotaToAdd(order), savedUser.ClaudeQuota)
	var saved commerceschema.TopUp
	require.NoError(t, db.First(&saved, order.Id).Error)
	require.Equal(t, constant.TopUpStatusSuccess, saved.Status)
	require.Equal(t, order.TradeNo, saved.TradeNo)
	require.Equal(t, "original-provider-order", saved.ExternalPaymentID)
	require.Equal(t, order.Money, saved.Money)
	require.Equal(t, order.PaidQuota, saved.PaidQuota)
	var payload map[string]string
	require.NoError(t, json.Unmarshal([]byte(saved.ProviderPayload), &payload))
	require.Equal(t, order.TradeNo, payload["out_trade_no"])
	var lots []billingschema.FundingLot
	require.NoError(t, db.Where("idempotency_key = ?", "topup:"+order.TradeNo+":unified").Find(&lots).Error)
	require.Len(t, lots, 1)
	require.Equal(t, int64(calculateTopUpQuotaToAdd(order)), lots[0].OriginalAmount)
	require.Equal(t, lots[0].OriginalAmount, lots[0].RemainingAmount)
	require.Equal(t, order.PaidQuota, lots[0].PaidQuota)
	require.Equal(t, billingschema.FundingSourceTopup, lots[0].Source)
	var account billingschema.BillingAccount
	require.NoError(t, db.First(&account, "account_id = ?", lots[0].AccountID).Error)
	require.EqualValues(t, user.Id, account.OwnerID)
	require.Equal(t, "user", account.OwnerType)
	require.Equal(t, "claude_wallet", account.AccountType)
	var balance billingschema.BillingBalanceSnapshot
	require.NoError(t, db.First(&balance, "account_id = ?", account.AccountID).Error)
	require.EqualValues(t, savedUser.ClaudeQuota, balance.AvailableBalance)
	var entries int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("idempotency_key = ?", "topup:"+order.TradeNo+":unified").Count(&entries).Error)
	require.EqualValues(t, 1, entries)
}

func TestEpayLatePaymentCompletesOnlyOriginalPendingOrProvableExpired(t *testing.T) {
	for _, scenario := range []string{"pending", "expired-no-card", "expired-reserved-original-card", "pending-card", "legacy-pending-reserved-original-card"} {
		t.Run(scenario, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			configureEpayLateTest(t)
			discount := strings.Contains(scenario, "reserved") || scenario == "pending-card"
			user, order, prop := seedEpayLateOrder(t, db, discount)
			if scenario == "expired-no-card" {
				n, err := ExpireDueTopUps(1)
				require.NoError(t, err)
				require.Equal(t, 1, n)
			} else if strings.Contains(scenario, "expired") {
				require.NoError(t, db.Model(order).Update("status", constant.TopUpStatusExpired).Error)
			}
			if scenario == "legacy-pending-reserved-original-card" {
				require.NoError(t, db.Model(order).Update("provider_payload", "").Error)
			}
			oldPrice := commercestore.Price
			commercestore.Price = 999
			t.Cleanup(func() { commercestore.Price = oldPrice })
			ok, err := deliverEpayLate(epayLateParams(order))
			require.NoError(t, err)
			require.True(t, ok)
			assertEpayCreditedOnce(t, db, user, order)
			before := epayLateSnapshot(t, db)
			ok, err = deliverEpayLate(epayLateParams(order))
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, before, epayLateSnapshot(t, db))
			if prop != nil {
				require.NoError(t, db.First(prop, prop.Id).Error)
				require.Equal(t, commerceschema.BlindBoxPropStatusUsed, prop.Status)
			}
		})
	}
}

func TestEpayLatePaymentRejectsMismatchAndUnsafeStatesWithoutMutation(t *testing.T) {
	for _, scenario := range []string{"wrong-money", "overpaid", "subcent", "nan", "wrong-provider", "wrong-method", "wrong-merchant", "wrong-order", "empty-provider-order", "wrong-currency", "bad-signature", "failed", "refunding", "refunded", "refund-processing", "refund-failed", "refund-success", "refund-marker", "legacy-expired-unknown", "legacy-expired-discount", "legacy-expired-full-paid", "legacy-pending-unknown", "legacy-pending-discount", "legacy-pending-full-paid", "released-prop", "reassigned-prop", "gifted-prop", "used-prop", "invalid-paid-quota"} {
		t.Run(scenario, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			configureEpayLateTest(t)
			discount := strings.Contains(scenario, "prop")
			_, order, prop := seedEpayLateOrder(t, db, discount)
			params := epayLateParams(order)
			switch scenario {
			case "wrong-money":
				params["money"] = "0.01"
			case "overpaid":
				params["money"] = "100.00"
			case "subcent":
				params["money"] = "73.001"
			case "nan":
				params["money"] = "NaN"
			case "wrong-provider":
				require.NoError(t, db.Model(order).Update("payment_provider", "stripe").Error)
			case "wrong-method":
				params["type"] = "wxpay"
			case "wrong-merchant":
				params["pid"] = "another-merchant"
			case "wrong-order":
				params["out_trade_no"] = "unknown-order"
			case "empty-provider-order":
				params["trade_no"] = ""
			case "wrong-currency":
				params["currency"] = "USD"
			case "failed", "refunding", "refunded":
				require.NoError(t, db.Model(order).Update("status", scenario).Error)
			case "refund-processing", "refund-failed", "refund-success":
				require.NoError(t, db.Model(order).Update("refund_status", strings.TrimPrefix(scenario, "refund-")).Error)
			case "refund-marker":
				require.NoError(t, db.Model(order).Update("refund_no", "original-refund-no").Error)
			case "legacy-expired-unknown", "legacy-expired-discount", "legacy-expired-full-paid", "legacy-pending-unknown", "legacy-pending-discount", "legacy-pending-full-paid":
				paid := int64(0)
				if strings.HasSuffix(scenario, "-discount") {
					paid = order.PaidQuota / 2
				}
				if strings.HasSuffix(scenario, "-full-paid") {
					paid = order.PaidQuota
				}
				status := constant.TopUpStatusPending
				if strings.Contains(scenario, "-expired-") {
					status = constant.TopUpStatusExpired
				}
				require.NoError(t, db.Model(order).Updates(map[string]any{"status": status, "provider_payload": "", "paid_quota": paid}).Error)
			case "released-prop", "reassigned-prop", "gifted-prop", "used-prop":
				_, err := ExpireDueTopUps(1)
				require.NoError(t, err)
				if scenario == "reassigned-prop" {
					require.NoError(t, db.Model(prop).Updates(map[string]any{"status": commerceschema.BlindBoxPropStatusReserved, "reserved_order_trade_no": "another-order", "reserved_order_type": "topup"}).Error)
				}
				if scenario == "gifted-prop" {
					require.NoError(t, db.Model(prop).Update("user_id", 100002).Error)
				}
				if scenario == "used-prop" {
					require.NoError(t, db.Model(prop).Update("status", commerceschema.BlindBoxPropStatusUsed).Error)
				}
			case "invalid-paid-quota":
				require.NoError(t, db.Model(order).Update("paid_quota", int64(calculateTopUpQuotaToAdd(order))+1).Error)
			}
			before := epayLateSnapshot(t, db)
			params = epay.GenerateParams(params, "synthetic-epay-key")
			if scenario == "bad-signature" {
				params["sign"] = "invalid"
			}
			ok, _ := HandleEpayWebhook(params, "127.0.0.1")
			require.False(t, ok)
			require.Equal(t, before, epayLateSnapshot(t, db))
		})
	}
}

func TestEpayLatePaymentTransactionFailureRollsBackOriginalCardAndWallet(t *testing.T) {
	db := setupRedemptionTestDB(t)
	configureEpayLateTest(t)
	_, order, _ := seedEpayLateOrder(t, db, true)
	before := epayLateSnapshot(t, db)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("epay_test_fail_lot", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == (billingschema.FundingLot{}).TableName() {
			tx.AddError(errors.New("synthetic funding lot failure"))
		}
	}))
	ok, err := deliverEpayLate(epayLateParams(order))
	require.ErrorContains(t, err, "synthetic funding lot failure")
	require.False(t, ok)
	require.Equal(t, before, epayLateSnapshot(t, db))
}

func TestEpayLatePaymentDoesNotRelaxManualOrOtherProviderCompletion(t *testing.T) {
	db := setupRedemptionTestDB(t)
	_, order, _ := seedEpayLateOrder(t, db, false)
	require.NoError(t, db.Model(order).Update("status", constant.TopUpStatusExpired).Error)
	before := epayLateSnapshot(t, db)
	_, quota, err := CompleteTopUpByTradeNo(order.TradeNo, commerceschema.PaymentProviderEpay, "alipay", "", "")
	require.ErrorIs(t, err, commerceschema.ErrTopUpStatusInvalid)
	require.Zero(t, quota)
	require.Error(t, ManualCompleteTopUp(order.TradeNo, "127.0.0.1"))
	require.Equal(t, before, epayLateSnapshot(t, db))
}

func TestEpayLatePaymentSuccessReplayPreservesRefundAndEvidence(t *testing.T) {
	for _, refundStatus := range []string{"processing", "success", "refunded"} {
		t.Run(refundStatus, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			configureEpayLateTest(t)
			user, order, _ := seedEpayLateOrder(t, db, false)
			ok, err := deliverEpayLate(epayLateParams(order))
			require.NoError(t, err)
			require.True(t, ok)
			require.NoError(t, db.Model(order).Updates(map[string]any{"refund_status": refundStatus, "refund_no": "original-refund"}).Error)
			before := epayLateSnapshot(t, db)
			ok, err = deliverEpayLate(epayLateParams(order))
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, before, epayLateSnapshot(t, db))
			params := epayLateParams(order)
			params["trade_no"] = "different-provider-order"
			ok, err = deliverEpayLate(params)
			require.Error(t, err)
			require.False(t, ok)
			require.Equal(t, before, epayLateSnapshot(t, db))
			assertEpayCreditedOnce(t, db, user, order)
		})
	}
}

func setupEpayPostgresTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("EPAY_TEST_DSN")
	if dsn == "" {
		t.Skip("EPAY_TEST_DSN is unset; PostgreSQL concurrency test requires an isolated database")
	}
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"postgres", "postgresql"}, parsed.Scheme)
	require.True(t, strings.HasPrefix(strings.TrimPrefix(parsed.Path, "/"), "codego_epay_test_"), "refusing a database outside the isolated test prefix")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	var dbName string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&dbName).Error)
	require.True(t, strings.HasPrefix(dbName, "codego_epay_test_"))
	schema := fmt.Sprintf("epay_%d", time.Now().UnixNano())
	require.NoError(t, db.Exec(`CREATE SCHEMA "`+schema+`"`).Error)
	base, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, base.Close())
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err = gorm.Open(postgres.Open(parsed.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	oldDB, oldLogDB, oldPG, oldSQLite, oldMySQL := platformdb.DB, platformdb.LogDB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL
	platformdb.DB, platformdb.LogDB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL = db, db, true, false, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.LogDB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL = oldDB, oldLogDB, oldPG, oldSQLite, oldMySQL
		pool, err := db.DB()
		if err == nil {
			_ = pool.Close()
		}
	})
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.AutoMigrate(&billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{}, &billingschema.FundingLot{}, &billingschema.FundingAllocation{}, &identityschema.User{}, &commerceschema.TopUp{}, &commerceschema.BlindBoxProp{}, &auditschema.Log{}))
	return db
}

func TestEpayLatePaymentPostgresConcurrentOriginalCallbacks(t *testing.T) {
	db := setupEpayPostgresTestDB(t)
	configureEpayLateTest(t)
	user, order, prop := seedEpayLateOrder(t, db, true)
	require.NoError(t, db.Model(order).Update("status", constant.TopUpStatusExpired).Error)
	// Fail inside PostgreSQL after the payment row, wallet and ledger writes.
	// The immutable lot constraint proves those writes roll back together.
	constraint := fmt.Sprintf("epay_failure_%d", user.Id)
	require.NoError(t, db.Exec(`ALTER TABLE billing.funding_lots ADD CONSTRAINT "`+constraint+`" CHECK (idempotency_key <> 'topup:`+order.TradeNo+`:unified')`).Error)
	before := epayLateSnapshot(t, db)
	params := epay.GenerateParams(epayLateParams(order), "synthetic-epay-key")
	verified, err := GetEpayClient().Verify(params)
	require.NoError(t, err)
	_, quota, err := completeVerifiedEpayTopUp(verified, params)
	require.ErrorContains(t, err, constraint)
	require.Zero(t, quota)
	require.Equal(t, before, epayLateSnapshot(t, db))
	require.NoError(t, db.Exec(`ALTER TABLE billing.funding_lots DROP CONSTRAINT "`+constraint+`"`).Error)
	t.Log("PostgreSQL native funding failure: original expired payment, wallet, ledger, paid snapshot and lot all rolled back")
	const workers = 24
	var wg sync.WaitGroup
	results := make(chan error, workers)
	start := make(chan struct{})
	// Bypass the process-local LockOrder to prove native database serialization
	// across instances, while still using SDK-verified synthetic original data.
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			params := epay.GenerateParams(epayLateParams(order), "synthetic-epay-key")
			verified, err := GetEpayClient().Verify(params)
			if err == nil {
				_, _, err = completeVerifiedEpayTopUp(verified, params)
			}
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	assertEpayCreditedOnce(t, db, user, order)
	require.NoError(t, db.First(prop, prop.Id).Error)
	require.Equal(t, commerceschema.BlindBoxPropStatusUsed, prop.Status)
	t.Logf("PostgreSQL concurrent original callbacks: %d accepted; one funding lot and one ledger credit; original paid quota %d", workers, order.PaidQuota)
}

func TestEpayLatePaymentPostgresOriginalCardExpiryRace(t *testing.T) {
	db := setupEpayPostgresTestDB(t)
	configureEpayLateTest(t)
	user, order, prop := seedEpayLateOrder(t, db, true)
	start := make(chan struct{})
	expiry := make(chan error, 1)
	payment := make(chan error, 1)
	go func() { <-start; _, err := ExpireDueTopUps(300); expiry <- err }()
	go func() {
		<-start
		params := epay.GenerateParams(epayLateParams(order), "synthetic-epay-key")
		verified, err := GetEpayClient().Verify(params)
		if err == nil {
			_, _, err = completeVerifiedEpayTopUp(verified, params)
		}
		payment <- err
	}()
	close(start)
	require.NoError(t, <-expiry)
	paymentErr := <-payment
	var saved commerceschema.TopUp
	require.NoError(t, db.First(&saved, order.Id).Error)
	require.NoError(t, db.First(prop, prop.Id).Error)
	if paymentErr == nil {
		assertEpayCreditedOnce(t, db, user, order)
		require.Equal(t, commerceschema.BlindBoxPropStatusUsed, prop.Status)
	} else {
		require.ErrorContains(t, paymentErr, "original discount prop released or conflicting")
		require.Equal(t, constant.TopUpStatusExpired, saved.Status)
		require.Equal(t, order.PaidQuota, saved.PaidQuota)
		require.Equal(t, commerceschema.BlindBoxPropStatusAvailable, prop.Status)
		var savedUser identityschema.User
		require.NoError(t, db.First(&savedUser, user.Id).Error)
		require.Equal(t, user.ClaudeQuota, savedUser.ClaudeQuota)
		var count int64
		require.NoError(t, db.Model(&billingschema.FundingLot{}).Where("idempotency_key = ?", "topup:"+order.TradeNo+":unified").Count(&count).Error)
		require.Zero(t, count)
	}
	t.Logf("PostgreSQL original-card expiry race: terminal status=%s; only paid+used or expired+available accepted", saved.Status)
}

func TestEpayLatePaymentPostgresReleasedOriginalCardRejected(t *testing.T) {
	db := setupEpayPostgresTestDB(t)
	configureEpayLateTest(t)
	_, order, prop := seedEpayLateOrder(t, db, true)
	n, err := ExpireDueTopUps(300)
	require.NoError(t, err)
	require.Positive(t, n)
	require.NoError(t, db.First(prop, prop.Id).Error)
	require.Equal(t, commerceschema.BlindBoxPropStatusAvailable, prop.Status)
	before := epayLateSnapshot(t, db)
	ok, err := deliverEpayLate(epayLateParams(order))
	require.False(t, ok)
	require.ErrorContains(t, err, "original discount prop released or conflicting")
	require.Equal(t, before, epayLateSnapshot(t, db))
	t.Log("PostgreSQL released original card: verified late callback rejected; expired original order and every wallet/funding row unchanged")
}

func TestEpayLatePaymentPostgresExpiryRacesOriginalCallback(t *testing.T) {
	db := setupEpayPostgresTestDB(t)
	configureEpayLateTest(t)
	user, order, _ := seedEpayLateOrder(t, db, false)
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, err := ExpireDueTopUps(300)
		results <- err
	}()
	go func() {
		<-start
		params := epay.GenerateParams(epayLateParams(order), "synthetic-epay-key")
		verified, err := GetEpayClient().Verify(params)
		if err == nil {
			_, _, err = completeVerifiedEpayTopUp(verified, params)
		}
		results <- err
	}()
	close(start)
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	assertEpayCreditedOnce(t, db, user, order)
	t.Log("PostgreSQL expiry versus original paid callback: success retained; exactly one native credit")
}
