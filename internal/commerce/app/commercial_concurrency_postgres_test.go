package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/sh2001sh/new-api/constant"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func commercialConcurrentCalls(workers int, run func(int) error) []error {
	results := make([]error, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range results {
		wg.Add(1)
		go func(i int) { defer wg.Done(); <-start; results[i] = run(i) }(i)
	}
	close(start)
	wg.Wait()
	return results
}

func TestCommercialPostgresOneCardCannotDiscountConcurrentMixedOrders(t *testing.T) {
	db := setupEpayPostgresTestDB(t)
	require.NoError(t, db.AutoMigrate(&commerceschema.SubscriptionOrder{}))
	user, _, _ := seedEpayLateOrder(t, db, false)
	prop := &commerceschema.BlindBoxProp{UserId: user.Id, PropType: commerceschema.BlindBoxPropTypeTopupDiscount90, Status: commerceschema.BlindBoxPropStatusAvailable, DiscountRate: 0.1}
	require.NoError(t, db.Create(prop).Error)
	const workers = 24
	var discounted sync.Map
	errors := commercialConcurrentCalls(workers, func(i int) error {
		return db.Transaction(func(tx *gorm.DB) error {
			tradeNo := fmt.Sprintf("mixed-%d-%d", user.Id, i)
			var card *commerceschema.BlindBoxProp
			var err error
			if i%2 == 0 {
				card, err = ReserveBlindBoxTopupDiscountPropTx(tx, user.Id, tradeNo)
			} else {
				card, err = ReserveBlindBoxSubscriptionDiscountPropTx(tx, user.Id, tradeNo)
			}
			if err != nil {
				return err
			}
			money := 10.0
			if card != nil {
				money = 9
				discounted.Store(tradeNo, card.Id)
			}
			if i%2 == 0 {
				return tx.Create(&commerceschema.TopUp{UserId: user.Id, TradeNo: tradeNo, Money: money, Status: constant.TopUpStatusPending}).Error
			}
			return tx.Create(&commerceschema.SubscriptionOrder{UserId: user.Id, TradeNo: tradeNo, Money: money, Status: constant.TopUpStatusPending}).Error
		})
	})
	for _, err := range errors {
		require.NoError(t, err)
	}
	var discountedTrades []string
	discounted.Range(func(key, value any) bool {
		discountedTrades = append(discountedTrades, key.(string))
		require.Equal(t, prop.Id, value)
		return true
	})
	require.Len(t, discountedTrades, 1)
	require.NoError(t, db.First(prop, prop.Id).Error)
	require.Equal(t, commerceschema.BlindBoxPropStatusReserved, prop.Status)
	require.Equal(t, discountedTrades[0], prop.ReservedOrderTradeNo)
	var topups, subscriptions int64
	require.NoError(t, db.Model(&commerceschema.TopUp{}).Where("user_id = ? AND money = 9", user.Id).Count(&topups).Error)
	require.NoError(t, db.Model(&commerceschema.SubscriptionOrder{}).Where("user_id = ? AND money = 9", user.Id).Count(&subscriptions).Error)
	require.EqualValues(t, 1, topups+subscriptions)
	t.Logf("PostgreSQL: %d concurrent mixed topup/subscription orders, exactly one discounted order and one original card reservation", workers)
}

func TestCommercialPostgresCardReservationRollsBackWhenOrderInsertFails(t *testing.T) {
	db := setupEpayPostgresTestDB(t)
	user, existing, _ := seedEpayLateOrder(t, db, false)
	prop := &commerceschema.BlindBoxProp{UserId: user.Id, PropType: commerceschema.BlindBoxPropTypeTopupDiscount90, Status: commerceschema.BlindBoxPropStatusAvailable, DiscountRate: 0.1}
	require.NoError(t, db.Create(prop).Error)
	err := db.Transaction(func(tx *gorm.DB) error {
		card, err := ReserveBlindBoxTopupDiscountPropTx(tx, user.Id, "rollback-order")
		if err != nil {
			return err
		}
		if card == nil {
			return errors.New("card missing")
		}
		return tx.Create(&commerceschema.TopUp{UserId: user.Id, TradeNo: existing.TradeNo, Status: constant.TopUpStatusPending}).Error
	})
	require.Error(t, err)
	require.NoError(t, db.First(prop, prop.Id).Error)
	require.Equal(t, commerceschema.BlindBoxPropStatusAvailable, prop.Status)
	require.Empty(t, prop.ReservedOrderTradeNo)
}

func TestCommercialPostgresDifferentMultiplierCardsSerializePerUser(t *testing.T) {
	db := setupEpayPostgresTestDB(t)
	user, _, _ := seedEpayLateOrder(t, db, false)
	props := make([]commerceschema.BlindBoxProp, 2)
	for i := range props {
		props[i] = commerceschema.BlindBoxProp{UserId: user.Id, PropType: commerceschema.BlindBoxPropTypeConsumeDiscount10, Status: commerceschema.BlindBoxPropStatusAvailable, Multiplier: 0.1, DurationSeconds: 900}
		require.NoError(t, db.Create(&props[i]).Error)
	}
	results := commercialConcurrentCalls(16, func(i int) error { _, err := ActivateBlindBoxProp(user.Id, props[i%2].Id); return err })
	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	var active int64
	require.NoError(t, db.Model(&commerceschema.BlindBoxProp{}).Where("user_id = ? AND status = ?", user.Id, commerceschema.BlindBoxPropStatusActive).Count(&active).Error)
	require.EqualValues(t, 1, active)
	t.Log("PostgreSQL: 16 concurrent activations across two different multiplier cards yielded exactly one active card")
}

func TestCommercialPostgresSubscriptionConcurrentPaymentAndFulfillmentAreExact(t *testing.T) {
	db := setupEpayPostgresTestDB(t)
	configureEpayLateTest(t)
	resetCommercialWorkflowClient(t)
	t.Setenv("TEMPORAL_HOSTPORT", "")
	require.NoError(t, db.AutoMigrate(&commerceschema.SubscriptionPlan{}, &commerceschema.SubscriptionOrder{}, &commerceschema.UserSubscription{}))
	user, _, _ := seedEpayLateOrder(t, db, false)
	// Billing uses its own schema; avoid sharing a synthetic subscription owner
	// identity with a different per-test schema in this dedicated database.
	require.NoError(t, db.Exec("SELECT setval(pg_get_serial_sequence('user_subscriptions','id'), ?)", user.Id).Error)
	plan := defaultSubscriptionPlans()[0]
	require.NoError(t, db.Create(&plan).Error)
	order := &commerceschema.SubscriptionOrder{UserId: user.Id, PlanId: plan.Id, TradeNo: fmt.Sprintf("pg-paid-pack-%d", user.Id), Money: 64, PaymentProvider: commerceschema.PaymentProviderEpay, PaymentMethod: "alipay", PurchaseType: commerceschema.SubscriptionPurchaseTypeNormal, Status: constant.TopUpStatusPending, CreateTime: time.Now().Unix()}
	_, err := CreatePendingSubscriptionOrderWithDiscounts(order, 64)
	require.NoError(t, err)
	verified, err := VerifySubscriptionEpay(signedCommercialParams(order.TradeNo, order.Money))
	require.NoError(t, err)
	payload, err := json.Marshal(verified)
	require.NoError(t, err)
	// Skip the process-local LockOrder: exercise the database across independent
	// concurrent callback transactions, just as multiple service instances do.
	for _, err := range commercialConcurrentCalls(24, func(int) error {
		return CompleteSubscriptionOrder(order.TradeNo, string(payload), commerceschema.PaymentProviderEpay, "alipay")
	}) {
		require.NoError(t, err)
	}
	for _, err := range commercialConcurrentCalls(16, func(int) error { return FulfillPaidSubscriptionOrder(order.TradeNo) }) {
		require.NoError(t, err)
	}
	require.NoError(t, db.First(order, order.Id).Error)
	require.Equal(t, commerceschema.SubscriptionOrderFulfillmentCompleted, order.FulfillmentStatus)
	var subscriptions []commerceschema.UserSubscription
	require.NoError(t, db.Where("user_id = ?", user.Id).Find(&subscriptions).Error)
	require.Len(t, subscriptions, 1)
	require.Equal(t, quotaUnitsFromUSD(67.2), subscriptions[0].AmountTotal)
	var entries int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("idempotency_key = ?", "credit-pack:"+order.TradeNo).Count(&entries).Error)
	require.EqualValues(t, 1, entries)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = (SELECT account_id FROM billing.accounts WHERE owner_type = ? AND owner_id = ? AND account_type = ?)", "user_subscription", subscriptions[0].Id, "subscription").First(&snapshot).Error)
	require.Equal(t, quotaUnitsFromUSD(67.2), snapshot.AvailableBalance)
	t.Log("PostgreSQL: 24 payment callbacks and 16 fulfillment attempts yielded one permanent pack, one ledger credit and the exact frozen balance")
}

func signedCommercialParams(tradeNo string, money float64) map[string]string {
	return epay.GenerateParams(commercialEpayParams(tradeNo, money), "synthetic-epay-key")
}

func TestCommercialPostgresBlindBoxConcurrentPaymentIssuesOneBatch(t *testing.T) {
	db := setupEpayPostgresTestDB(t)
	configureEpayLateTest(t)
	require.NoError(t, db.AutoMigrate(&commerceschema.BlindBoxOrder{}, &commerceschema.BalanceBlindBoxPurchase{}, &commerceschema.BalanceBlindBoxItem{}))
	user, _, _ := seedEpayLateOrder(t, db, false)
	order := &commerceschema.BlindBoxOrder{UserId: user.Id, TradeNo: fmt.Sprintf("pg-paid-bb-%d", user.Id), Money: 7.5, Quantity: 3, PaymentProvider: commerceschema.PaymentProviderEpay, PaymentMethod: "alipay", Status: constant.TopUpStatusExpired}
	require.NoError(t, db.Create(order).Error)
	verified, err := VerifyBlindBoxEpay(signedCommercialParams(order.TradeNo, order.Money))
	require.NoError(t, err)
	payload, err := json.Marshal(verified)
	require.NoError(t, err)
	for _, err := range commercialConcurrentCalls(24, func(int) error {
		return CompleteBlindBoxOrder(order.TradeNo, string(payload), commerceschema.PaymentProviderEpay, "alipay")
	}) {
		require.NoError(t, err)
	}
	var batches, inventory int64
	require.NoError(t, db.Model(&commerceschema.BalanceBlindBoxPurchase{}).Where("user_id = ?", user.Id).Count(&batches).Error)
	require.NoError(t, db.Model(&commerceschema.BalanceBlindBoxItem{}).Where("owner_user_id = ?", user.Id).Count(&inventory).Error)
	require.EqualValues(t, 1, batches)
	require.EqualValues(t, 3, inventory)
	t.Log("PostgreSQL: 24 native concurrent blind-box callbacks yielded one inventory batch and exactly three sealed boxes")
}

func TestCommercialPostgresSubscriptionExpiryAndPaidCallbackRemainAtomic(t *testing.T) {
	db := setupEpayPostgresTestDB(t)
	configureEpayLateTest(t)
	resetCommercialWorkflowClient(t)
	t.Setenv("TEMPORAL_HOSTPORT", "")
	require.NoError(t, db.AutoMigrate(&commerceschema.SubscriptionOrder{}))
	user, _, _ := seedEpayLateOrder(t, db, false)
	for i := 0; i < 12; i++ {
		order := &commerceschema.SubscriptionOrder{UserId: user.Id, TradeNo: fmt.Sprintf("pg-expiry-sub-%d-%d", user.Id, i), Money: 57.6, PaymentProvider: commerceschema.PaymentProviderEpay, PaymentMethod: "alipay", Status: constant.TopUpStatusPending}
		prop := &commerceschema.BlindBoxProp{UserId: user.Id, PropType: commerceschema.BlindBoxPropTypeSubscriptionDiscount90, Status: commerceschema.BlindBoxPropStatusReserved, ReservedOrderType: commerceschema.BlindBoxPropOrderTypeSubscription, ReservedOrderTradeNo: order.TradeNo, DiscountRate: 0.1}
		require.NoError(t, db.Create(prop).Error)
		require.NoError(t, snapshotSubscriptionEpayCheckout(order, prop.Id))
		require.NoError(t, db.Create(order).Error)
		verified, err := VerifySubscriptionEpay(signedCommercialParams(order.TradeNo, order.Money))
		require.NoError(t, err)
		payload, err := json.Marshal(verified)
		require.NoError(t, err)
		results := commercialConcurrentCalls(2, func(index int) error {
			if index == 0 {
				return ExpireSubscriptionOrder(order.TradeNo, commerceschema.PaymentProviderEpay)
			}
			return CompleteSubscriptionOrder(order.TradeNo, string(payload), commerceschema.PaymentProviderEpay, "alipay")
		})
		require.NoError(t, results[0])
		require.NoError(t, db.First(order, order.Id).Error)
		require.NoError(t, db.First(prop, prop.Id).Error)
		if order.Status == constant.TopUpStatusSuccess {
			require.NoError(t, results[1])
			require.Equal(t, commerceschema.BlindBoxPropStatusUsed, prop.Status)
		} else {
			require.Equal(t, constant.TopUpStatusExpired, order.Status)
			require.Error(t, results[1])
			require.Equal(t, commerceschema.BlindBoxPropStatusAvailable, prop.Status)
			require.Empty(t, prop.ReservedOrderTradeNo)
		}
	}
	t.Log("PostgreSQL: 12 expiry/payment races preserved paid+used or expired+released, with no original-card reoccupation")
}
