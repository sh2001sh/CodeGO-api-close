package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/constant"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	paymentsettings "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type refundTransport func(*http.Request) (*http.Response, error)

func (f refundTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mockRefundProvider(t *testing.T, f refundTransport) {
	t.Helper()
	oldTransport := http.DefaultTransport
	oldAddress, oldID, oldKey := paymentsettings.PayAddress, paymentsettings.EpayId, paymentsettings.EpayKey
	http.DefaultTransport = f
	paymentsettings.PayAddress, paymentsettings.EpayId, paymentsettings.EpayKey = "https://refund.invalid", "test-client", "test-only-key"
	t.Cleanup(func() {
		http.DefaultTransport = oldTransport
		paymentsettings.PayAddress, paymentsettings.EpayId, paymentsettings.EpayKey = oldAddress, oldID, oldKey
	})
}

func refundProviderResponse(t *testing.T, r *http.Request, providerOrder string, status int) *http.Response {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
	data := map[string]any{"refundNo": body["refundNo"], "refundAmount": int64(9800), "refundId": "provider-refund", "orderId": providerOrder, "status": status}
	encoded, err := json.Marshal(map[string]any{"code": 1000, "data": data})
	require.NoError(t, err)
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(encoded))), Header: make(http.Header)}
}

func seedRefundWallet(t *testing.T, db *gorm.DB) (*commerceschema.TopUp, string) {
	t.Helper()
	const userID = 84210
	require.NoError(t, db.Create(&identityschema.User{Id: userID, Username: "refund-safety", AffCode: "refund-safety", ClaudeQuota: 1000}).Error)
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "claude_wallet", OwnerType: "user", OwnerID: userID, QuotaUnit: "quota"})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 1000, IdempotencyKey: "seed-refund-wallet", ReasonCode: "topup", ReferenceType: "topup", ReferenceID: "refund-safety"})
	require.NoError(t, err)
	// CreditAccount does not create this immutable top-up attribution by itself.
	require.NoError(t, db.Create(&billingschema.FundingLot{AccountID: account.AccountID, Source: billingschema.FundingSourceTopup, IdempotencyKey: "topup:refund-safety:unified", OriginalAmount: 1000, PaidQuota: 1000, RemainingAmount: 1000}).Error)
	order := &commerceschema.TopUp{UserId: userID, TradeNo: "refund-safety", Money: 100, Amount: 100, Status: constant.TopUpStatusSuccess, PaymentProvider: commerceschema.PaymentProviderEpay, PaymentMethod: "alipay", ProviderPayload: `{"trade_no":"provider-paid","out_trade_no":"refund-safety","money":"100.00","currency":"CNY","trade_status":"TRADE_SUCCESS"}`}
	require.NoError(t, db.Create(order).Error)
	return order, account.AccountID
}

func assertRefundWallet(t *testing.T, db *gorm.DB, account string, available, reserved, lotRemaining int64) {
	t.Helper()
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account).First(&snapshot).Error)
	require.Equal(t, available, snapshot.AvailableBalance)
	require.Equal(t, reserved, snapshot.ReservedBalance)
	var user identityschema.User
	require.NoError(t, db.First(&user, 84210).Error)
	require.EqualValues(t, available, user.ClaudeQuota)
	var lot billingschema.FundingLot
	require.NoError(t, db.Where("idempotency_key = ?", "topup:refund-safety:unified").First(&lot).Error)
	require.Equal(t, lotRemaining, lot.RemainingAmount)
}

func TestRefundFreezesFundsBeforeProviderAndUnknownResultNeverCreatesAgain(t *testing.T) {
	db := setupRedemptionTestDB(t)
	order, account := seedRefundWallet(t, db)
	creates, queries := 0, 0
	mockRefundProvider(t, func(r *http.Request) (*http.Response, error) {
		assertRefundWallet(t, db, account, 0, 1000, 0)
		switch r.URL.Path {
		case "/open/payment/refund/create":
			creates++
			return nil, errors.New("simulated lost provider response")
		case "/open/payment/refund/query":
			queries++
			return refundProviderResponse(t, r, "provider-paid", 2), nil
		default:
			t.Fatal("unexpected provider endpoint")
			return nil, errors.New("unexpected endpoint")
		}
	})
	result, err := createRefund(order.UserId, commerceschema.RefundRequest{OrderType: commerceschema.RefundOrderTypeBalance, TradeNo: order.TradeNo})
	require.Error(t, err)
	require.Equal(t, commerceschema.RefundStatusProcessing, result.Status)
	require.NotEmpty(t, result.RefundNo)
	assertRefundWallet(t, db, account, 0, 1000, 0)
	candidates, err := listRefundableOrders(order.UserId)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, 98.0, candidates[0].RefundAmount)
	require.Equal(t, result.RefundNo, candidates[0].RefundNo)
	for _, field := range []string{"refund_no", "order_id", "amount"} {
		invalid := &JianPayRefundResult{RefundNo: result.RefundNo, OrderID: "provider-paid", RefundID: "provider-refund", RefundAmount: 9800, Status: 2}
		switch field {
		case "refund_no":
			invalid.RefundNo = "different-refund"
		case "order_id":
			invalid.OrderID = "different-order"
		case "amount":
			invalid.RefundAmount++
		}
		require.Error(t, saveRefundProviderState(order.UserId, commerceschema.RefundOrderTypeBalance, order.TradeNo, result.RefundNo, invalid))
		assertRefundWallet(t, db, account, 0, 1000, 0)
	}
	var hold billingschema.BillingReservation
	require.NoError(t, db.Where("idempotency_key = ?", "payment-refund:"+result.RefundNo+":reserve").First(&hold).Error)
	require.Empty(t, hold.RequestID)
	require.Equal(t, result.RefundNo, hold.WorkflowID)
	require.Nil(t, hold.ExpiresAt)
	_, err = createRefund(order.UserId, commerceschema.RefundRequest{OrderType: commerceschema.RefundOrderTypeBalance, TradeNo: order.TradeNo})
	require.Error(t, err)
	require.Equal(t, 1, creates)
	require.Error(t, db.Transaction(func(tx *gorm.DB) error {
		return billingapp.DebitClaudeWalletQuotaTx(tx, order.UserId, 1, "try-spend-refund")
	}))
	result, err = syncRefund(order.UserId, result.RefundNo)
	require.NoError(t, err)
	require.Equal(t, commerceschema.RefundStatusSuccess, result.Status)
	assertRefundWallet(t, db, account, 0, 0, 0)
	_, err = syncRefund(order.UserId, result.RefundNo)
	require.NoError(t, err)
	require.Equal(t, 1, queries)
}

func TestRefundExplicitFailureRestoresExactLotAndRejectsLatePreviousAttempt(t *testing.T) {
	db := setupRedemptionTestDB(t)
	order, account := seedRefundWallet(t, db)
	mockRefundProvider(t, func(r *http.Request) (*http.Response, error) {
		return refundProviderResponse(t, r, "provider-paid", 3), nil
	})
	first, err := createRefund(order.UserId, commerceschema.RefundRequest{OrderType: commerceschema.RefundOrderTypeBalance, TradeNo: order.TradeNo})
	require.NoError(t, err)
	require.Equal(t, commerceschema.RefundStatusFailed, first.Status)
	assertRefundWallet(t, db, account, 1000, 0, 1000)
	var lots int64
	require.NoError(t, db.Model(&billingschema.FundingLot{}).Count(&lots).Error)
	require.EqualValues(t, 1, lots)
	mockRefundProvider(t, func(r *http.Request) (*http.Response, error) {
		return refundProviderResponse(t, r, "provider-paid", 1), nil
	})
	second, err := createRefund(order.UserId, commerceschema.RefundRequest{OrderType: commerceschema.RefundOrderTypeBalance, TradeNo: order.TradeNo})
	require.NoError(t, err)
	require.NotEqual(t, first.RefundNo, second.RefundNo)
	late := &JianPayRefundResult{RefundNo: first.RefundNo, OrderID: "provider-paid", RefundID: "first-response", RefundAmount: 9800, Status: 2}
	require.Error(t, saveRefundProviderState(order.UserId, commerceschema.RefundOrderTypeBalance, order.TradeNo, first.RefundNo, late))
	assertRefundWallet(t, db, account, 0, 1000, 0)
	require.NoError(t, db.First(order, order.Id).Error)
	require.Equal(t, second.RefundNo, order.RefundNo)
	require.Equal(t, commerceschema.RefundStatusProcessing, order.RefundStatus)
}

func TestRefundRejectsMismatchedProviderAndInvalidPaymentsBeforeMoneyMoves(t *testing.T) {
	for _, field := range []string{"status", "provider", "method", "currency", "money", "provider_order"} {
		t.Run(field, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			order, account := seedRefundWallet(t, db)
			switch field {
			case "status":
				order.Status = constant.TopUpStatusPending
			case "provider":
				order.PaymentProvider = "stripe"
			case "method":
				order.PaymentMethod = "internal"
			case "currency":
				order.ProviderPayload = `{"trade_no":"provider-paid","currency":"USD"}`
			case "money":
				order.ProviderPayload = `{"trade_no":"provider-paid","money":"999"}`
			case "provider_order":
				order.ProviderPayload = `{}`
			}
			require.NoError(t, db.Save(order).Error)
			mockRefundProvider(t, func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid payment reached provider")
				return nil, errors.New("forbidden")
			})
			_, err := createRefund(order.UserId, commerceschema.RefundRequest{OrderType: commerceschema.RefundOrderTypeBalance, TradeNo: order.TradeNo})
			require.Error(t, err)
			assertRefundWallet(t, db, account, 1000, 0, 1000)
		})
	}
	t.Run("provider-response-order-mismatch", func(t *testing.T) {
		db := setupRedemptionTestDB(t)
		order, account := seedRefundWallet(t, db)
		mockRefundProvider(t, func(r *http.Request) (*http.Response, error) {
			return refundProviderResponse(t, r, "wrong-order", 2), nil
		})
		_, err := createRefund(order.UserId, commerceschema.RefundRequest{OrderType: commerceschema.RefundOrderTypeBalance, TradeNo: order.TradeNo})
		require.Error(t, err)
		assertRefundWallet(t, db, account, 0, 1000, 0)
	})
}

func TestRefundBlocksStaleTransferredLotAndGenericDebitConsumesFundingFIFO(t *testing.T) {
	db := setupRedemptionTestDB(t)
	order, account := seedRefundWallet(t, db)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return billingapp.DebitClaudeWalletQuotaTxWithReason(tx, order.UserId, 900, "peer-transfer-before-refund", "wallet_peer_transfer_debit")
	}))
	assertRefundWallet(t, db, account, 100, 0, 100)
	// A historical debit with an unadjusted lot must fail closed even when the
	// requested lot amount itself does not exceed the canonical wallet balance.
	require.NoError(t, db.Create(&billingschema.FundingLot{AccountID: account, Source: billingschema.FundingSourceOther, OriginalAmount: 900, RemainingAmount: 900, IdempotencyKey: "stale-transfer-funding"}).Error)
	require.Error(t, db.Transaction(func(tx *gorm.DB) error {
		return billingapp.ReserveTopupRefundTx(tx, order.UserId, order.TradeNo, 100, "stale-lot-refund")
	}))
	assertRefundWallet(t, db, account, 100, 0, 100)
}

func TestRefundRejectsChangedProviderAttemptIdentityAndAmount(t *testing.T) {
	db := setupRedemptionTestDB(t)
	order, account := seedRefundWallet(t, db)
	mockRefundProvider(t, func(r *http.Request) (*http.Response, error) {
		return refundProviderResponse(t, r, "provider-paid", 1), nil
	})
	created, err := createRefund(order.UserId, commerceschema.RefundRequest{OrderType: commerceschema.RefundOrderTypeBalance, TradeNo: order.TradeNo})
	require.NoError(t, err)
	for _, field := range []string{"refund_id", "refund_no", "amount"} {
		t.Run(field, func(t *testing.T) {
			response := &JianPayRefundResult{RefundNo: created.RefundNo, RefundID: created.RefundID, OrderID: "provider-paid", RefundAmount: 9800, Status: 2}
			switch field {
			case "refund_id":
				response.RefundID = "different-provider-attempt"
			case "refund_no":
				response.RefundNo = "different-refund"
			case "amount":
				response.RefundAmount++
			}
			require.Error(t, saveRefundProviderState(order.UserId, commerceschema.RefundOrderTypeBalance, order.TradeNo, created.RefundNo, response))
			assertRefundWallet(t, db, account, 0, 1000, 0)
			require.NoError(t, db.First(order, order.Id).Error)
			require.Equal(t, commerceschema.RefundStatusProcessing, order.RefundStatus)
			require.Equal(t, created.RefundID, order.RefundProviderID)
		})
	}
}

func TestRefundMoneyNeverExceedsOriginalCashAndRejectsNonFiniteValues(t *testing.T) {
	gross, _, net := refundMoney(10, 2000, 1000)
	require.Equal(t, 10.0, gross)
	require.Equal(t, 9.8, net)
	for _, value := range []string{"NaN", "+Inf"} {
		var paid float64
		_, err := fmt.Sscan(value, &paid)
		require.NoError(t, err)
		gross, _, net := refundMoney(paid, 1000, 1000)
		require.Zero(t, gross)
		require.Zero(t, net)
	}
}

func TestRefundableSubscriptionDoesNotSubtractReleasedPreauthorizationsFromActualUsage(t *testing.T) {
	db := setupRedemptionTestDB(t)
	sub := &commerceschema.UserSubscription{Id: 84211, UserId: 84210, AmountTotal: 1000, AmountUsed: 0, Status: "active"}
	require.NoError(t, db.Create(sub).Error)
	require.NoError(t, db.Create(&commerceschema.SubscriptionResetOpportunityLedger{UserId: sub.UserId, RelatedUserId: sub.Id, ChangeType: commerceschema.SubscriptionResetOpportunityChangeUse, Delta: -1, UsedMonth: "2026-10", SourceType: "user_subscription", SourceRef: "84211", EventKey: "refund-real-preauth-reset"}).Error)
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id), QuotaUnit: "quota"})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 1000, IdempotencyKey: "refund-preauth-credit"})
	require.NoError(t, err)
	hold, err := billingdomain.CreateReservation(billingdomain.CreateReservationParams{AccountID: account.AccountID, ReservedAmount: 100, IdempotencyKey: "refund-preauth-release"})
	require.NoError(t, err)
	_, err = billingdomain.ReleaseReservation(billingdomain.ReleaseReservationParams{ReservationID: hold.ReservationID, IdempotencyKey: "refund-preauth-release-confirmed"})
	require.NoError(t, err)
	hold, err = billingdomain.CreateReservation(billingdomain.CreateReservationParams{AccountID: account.AccountID, ReservedAmount: 500, IdempotencyKey: "refund-preauth-partial"})
	require.NoError(t, err)
	_, err = billingdomain.SettleReservation(billingdomain.SettleReservationParams{ReservationID: hold.ReservationID, ActualAmount: 200, IdempotencyKey: "refund-preauth-actual"})
	require.NoError(t, err)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	require.EqualValues(t, 200, snapshot.ConsumedTotal)
	require.EqualValues(t, 300, snapshot.RefundedTotal)
	order := &commerceschema.SubscriptionOrder{UserId: sub.UserId, TargetSubscriptionId: sub.Id, Money: 100, Status: constant.TopUpStatusSuccess, PaymentProvider: commerceschema.PaymentProviderEpay, PaymentMethod: "alipay", FulfillmentStatus: commerceschema.SubscriptionOrderFulfillmentCompleted, ProviderPayload: `{"trade_no":"paid-subscription"}`}
	item := refundableSubscriptionWithDB(db, sub.UserId, order)
	require.True(t, item.Refundable)
	require.EqualValues(t, 200, item.UsedQuota)
	require.EqualValues(t, 800, item.RemainingQuota)
}

func TestRefundFreezesBothLegacyAndCreditPackAndRestoresOnDefiniteFailure(t *testing.T) {
	for _, packQuota := range []int64{0, 1000} {
		t.Run(fmt.Sprint(packQuota), func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			ensureSubscriptionPreConsumeRecordSchema(t)
			sub := &commerceschema.UserSubscription{Id: 84212, UserId: 84210, PlanId: 1, AmountTotal: 1000, PeriodAmount: 1000, CreditPackGrantedQuota: packQuota, Status: "active", EndTime: 4102444800}
			require.NoError(t, db.Create(sub).Error)
			account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id), QuotaUnit: "quota"})
			require.NoError(t, err)
			_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 1000, IdempotencyKey: "refund-sub-credit"})
			require.NoError(t, err)
			order := &commerceschema.SubscriptionOrder{UserId: sub.UserId, TargetSubscriptionId: sub.Id, TradeNo: "refund-subscription", Money: 100, Status: constant.TopUpStatusSuccess, PaymentProvider: commerceschema.PaymentProviderEpay, PaymentMethod: "alipay", FulfillmentStatus: commerceschema.SubscriptionOrderFulfillmentCompleted, ProviderPayload: `{"trade_no":"paid-subscription"}`}
			require.NoError(t, db.Create(order).Error)
			mockRefundProvider(t, func(r *http.Request) (*http.Response, error) {
				var frozen commerceschema.UserSubscription
				require.NoError(t, db.First(&frozen, sub.Id).Error)
				require.Equal(t, "refund_pending", frozen.Status)
				require.Error(t, ReserveAdditionalSubscriptionQuota("refund-sub-try-spend", sub.Id, "gpt-5", 1))
				var snapshot billingschema.BillingBalanceSnapshot
				require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
				require.Zero(t, snapshot.AvailableBalance)
				require.EqualValues(t, 1000, snapshot.ReservedBalance)
				candidates, err := listRefundableOrders(sub.UserId)
				require.NoError(t, err)
				require.Len(t, candidates, 1)
				require.Equal(t, 98.0, candidates[0].RefundAmount)
				require.NotEmpty(t, candidates[0].RefundNo)
				return refundProviderResponse(t, r, "paid-subscription", 3), nil
			})
			result, err := createRefund(sub.UserId, commerceschema.RefundRequest{OrderType: commerceschema.RefundOrderTypeSubscription, TradeNo: order.TradeNo})
			require.NoError(t, err)
			require.Equal(t, commerceschema.RefundStatusFailed, result.Status)
			require.NoError(t, db.First(sub, sub.Id).Error)
			require.Equal(t, "active", sub.Status)
			require.EqualValues(t, 1000, sub.AmountTotal)
			var snapshot billingschema.BillingBalanceSnapshot
			require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
			require.EqualValues(t, 1000, snapshot.AvailableBalance)
			require.Zero(t, snapshot.ReservedBalance)
			// A live native request prevents any second provider attempt.
			hold, err := billingdomain.CreateReservation(billingdomain.CreateReservationParams{AccountID: account.AccountID, ReservedAmount: 100, IdempotencyKey: "refund-sub-active-usage"})
			require.NoError(t, err)
			require.NotNil(t, hold)
			_, err = createRefund(sub.UserId, commerceschema.RefundRequest{OrderType: commerceschema.RefundOrderTypeSubscription, TradeNo: order.TradeNo})
			require.Error(t, err)
		})
	}
}

func TestRefundRejectsAmbiguousMultiplePaidOrdersForOneSubscription(t *testing.T) {
	db := setupRedemptionTestDB(t)
	sub := &commerceschema.UserSubscription{Id: 84213, UserId: 84210, AmountTotal: 2000, Status: "active"}
	require.NoError(t, db.Create(sub).Error)
	for _, trade := range []string{"paid-period-one", "paid-period-two"} {
		order := &commerceschema.SubscriptionOrder{UserId: sub.UserId, TargetSubscriptionId: sub.Id, TradeNo: trade, Money: 100, Status: constant.TopUpStatusSuccess, FulfillmentStatus: commerceschema.SubscriptionOrderFulfillmentCompleted, PaymentProvider: commerceschema.PaymentProviderEpay, PaymentMethod: "alipay", ProviderPayload: `{"trade_no":"provider-paid-period"}`}
		require.NoError(t, db.Create(order).Error)
	}
	mockRefundProvider(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("ambiguous subscription reached provider")
		return nil, errors.New("forbidden")
	})
	_, err := createRefund(sub.UserId, commerceschema.RefundRequest{OrderType: commerceschema.RefundOrderTypeSubscription, TradeNo: "paid-period-one"})
	require.ErrorContains(t, err, "多笔付款")
	require.NoError(t, db.First(sub, sub.Id).Error)
	require.Equal(t, "active", sub.Status)
	require.EqualValues(t, 2000, sub.AmountTotal)
}
