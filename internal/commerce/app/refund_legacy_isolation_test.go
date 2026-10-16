package app

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedRefundIsolationOrder(t *testing.T, db *gorm.DB, kind string) (int, commerceschema.RefundRequest) {
	t.Helper()
	if kind == "wallet" {
		order, _ := seedRefundWallet(t, db)
		return order.UserId, commerceschema.RefundRequest{OrderType: commerceschema.RefundOrderTypeBalance, TradeNo: order.TradeNo}
	}
	const userID = 84210
	const subID = 84212
	require.NoError(t, db.Create(&identityschema.User{Id: userID, Username: "refund-isolation", AffCode: "refund-isolation"}).Error)
	sub := &commerceschema.UserSubscription{Id: subID, UserId: userID, PlanId: 1, AmountTotal: 1000, PeriodAmount: 1000, Status: "active", EndTime: 4102444800}
	if kind == "credit-pack" {
		sub.CreditPackGrantedQuota = 1000
	}
	require.NoError(t, db.Create(sub).Error)
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "subscription", OwnerType: "user_subscription", OwnerID: subID, QuotaUnit: "quota"})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 1000, IdempotencyKey: "refund-isolation-sub-credit"})
	require.NoError(t, err)
	order := &commerceschema.SubscriptionOrder{UserId: userID, TargetSubscriptionId: subID, TradeNo: "refund-isolation-sub", Money: 100, Status: constant.TopUpStatusSuccess, PaymentProvider: commerceschema.PaymentProviderEpay, PaymentMethod: "alipay", FulfillmentStatus: commerceschema.SubscriptionOrderFulfillmentCompleted, ProviderPayload: `{"trade_no":"provider-paid"}`}
	require.NoError(t, db.Create(order).Error)
	return userID, commerceschema.RefundRequest{OrderType: commerceschema.RefundOrderTypeSubscription, TradeNo: order.TradeNo}
}

// Compare the complete payment, funding and balance rows, including absence of
// newly manufactured holds or lots, after each rejected historical transition.
func refundIsolationSnapshot(t *testing.T, db *gorm.DB) []any {
	t.Helper()
	var snapshot []any
	for _, model := range []any{&commerceschema.TopUp{}, &commerceschema.SubscriptionOrder{}, &commerceschema.UserSubscription{}, &identityschema.User{}, &billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingReservation{}, &billingschema.FundingLot{}, &billingschema.BillingLedgerEntry{}} {
		var rows []map[string]any
		require.NoError(t, db.Model(model).Find(&rows).Error)
		snapshot = append(snapshot, rows)
	}
	return snapshot
}

func TestHistoricalRefundWithoutNativeHoldIsQuarantined(t *testing.T) {
	for _, kind := range []string{"wallet", "legacy-subscription", "credit-pack"} {
		for _, status := range []string{commerceschema.RefundStatusFailed, commerceschema.RefundStatusProcessing} {
			for _, providerID := range []string{"", "historical-provider-refund"} {
				t.Run(kind+"/"+status+"/"+providerID, func(t *testing.T) {
					db := setupRedemptionTestDB(t)
					userID, request := seedRefundIsolationOrder(t, db, kind)
					state, err := loadRefundOrderTx(db, userID, request.OrderType, request.TradeNo)
					require.NoError(t, err)
					require.NoError(t, db.Model(state.record).Updates(map[string]any{"refund_status": status, "refund_no": "historical-original-no", "refund_provider_id": providerID, "refund_quota": 1000, "refund_amount": 98, "refund_updated_at": 7}).Error)
					before := refundIsolationSnapshot(t, db)
					calls := 0
					mockRefundProvider(t, func(*http.Request) (*http.Response, error) {
						calls++
						return nil, errors.New("historical attempt must not reach provider")
					})

					items, err := listRefundableOrders(userID)
					require.NoError(t, err)
					require.Len(t, items, 1)
					require.False(t, items[0].Refundable)
					require.Equal(t, "historical-original-no", items[0].RefundNo)
					require.Equal(t, errRefundHistoricalReview.Error(), items[0].UnavailableReason)
					require.Equal(t, before, refundIsolationSnapshot(t, db))

					result, err := createRefund(userID, request)
					require.Error(t, err)
					require.Nil(t, result)
					if status == commerceschema.RefundStatusFailed {
						require.ErrorIs(t, err, errRefundHistoricalReview)
					}
					require.Equal(t, before, refundIsolationSnapshot(t, db))

					result, err = syncRefund(userID, "historical-original-no")
					require.ErrorIs(t, err, errRefundHistoricalReview)
					require.Nil(t, result)
					require.Equal(t, before, refundIsolationSnapshot(t, db))
					for _, providerStatus := range []int{1, 2, 3} {
						responseID := providerID
						if responseID == "" {
							responseID = "late-provider-id"
						}
						err = saveRefundProviderState(userID, request.OrderType, request.TradeNo, "historical-original-no", &JianPayRefundResult{RefundNo: "historical-original-no", RefundID: responseID, OrderID: "provider-paid", RefundAmount: 9800, Status: providerStatus})
						require.Error(t, err)
						if status == commerceschema.RefundStatusProcessing {
							require.ErrorIs(t, err, errRefundHistoricalReview)
						}
						require.Equal(t, before, refundIsolationSnapshot(t, db))
					}
					require.Zero(t, calls)
				})
			}
		}
	}
}

func TestRefundRetryRequiresReleasedMatchingNativeHold(t *testing.T) {
	for _, field := range []string{"missing", "open", "settled", "account", "quota", "workflow", "request", "expires", "provider-id", "refund-no"} {
		t.Run(field, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			userID, request := seedRefundIsolationOrder(t, db, "wallet")
			calls := 0
			mockRefundProvider(t, func(r *http.Request) (*http.Response, error) {
				calls++
				return refundProviderResponse(t, r, "provider-paid", 3), nil
			})
			first, err := createRefund(userID, request)
			require.NoError(t, err)
			require.Equal(t, commerceschema.RefundStatusFailed, first.Status)
			var hold billingschema.BillingReservation
			require.NoError(t, db.Where("idempotency_key = ?", "payment-refund:"+first.RefundNo+":reserve").First(&hold).Error)
			require.Equal(t, billingschema.BillingReservationStatusReleased, hold.Status)
			switch field {
			case "missing":
				require.NoError(t, db.Delete(&hold).Error)
			case "open", "settled":
				require.NoError(t, db.Model(&hold).Update("status", field).Error)
			case "account":
				other := &billingschema.BillingAccount{AccountID: "another-user-wallet", AccountType: "claude_wallet", OwnerType: "user", OwnerID: int64(userID + 1), QuotaUnit: "quota"}
				require.NoError(t, db.Create(other).Error)
				require.NoError(t, db.Model(&hold).Update("account_id", other.AccountID).Error)
			case "quota":
				require.NoError(t, db.Model(&hold).Update("reserved_amount", hold.ReservedAmount+1).Error)
			case "workflow":
				require.NoError(t, db.Model(&hold).Update("workflow_id", "another-refund").Error)
			case "request":
				require.NoError(t, db.Model(&hold).Update("request_id", "relay-request").Error)
			case "expires":
				require.NoError(t, db.Model(&hold).Update("expires_at", time.Now().Add(time.Hour)).Error)
			case "provider-id":
				require.NoError(t, db.Model(&commerceschema.TopUp{}).Where("trade_no = ?", request.TradeNo).Update("refund_provider_id", "").Error)
			case "refund-no":
				require.NoError(t, db.Model(&hold).Update("idempotency_key", "payment-refund:another-refund:reserve").Error)
			}
			before := refundIsolationSnapshot(t, db)
			items, err := listRefundableOrders(userID)
			require.NoError(t, err)
			require.Len(t, items, 1)
			require.False(t, items[0].Refundable)
			require.Equal(t, errRefundHistoricalReview.Error(), items[0].UnavailableReason)
			_, err = createRefund(userID, request)
			require.ErrorIs(t, err, errRefundHistoricalReview)
			_, err = syncRefund(userID, first.RefundNo)
			require.ErrorIs(t, err, errRefundHistoricalReview)
			require.Equal(t, 1, calls, "unsafe retry reached provider")
			require.Equal(t, before, refundIsolationSnapshot(t, db))
		})
	}
}

func TestRefundNativeReleasedAttemptStillAllowsRetry(t *testing.T) {
	for _, kind := range []string{"wallet", "legacy-subscription", "credit-pack"} {
		t.Run(kind, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			userID, request := seedRefundIsolationOrder(t, db, kind)
			calls := 0
			mockRefundProvider(t, func(r *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "/open/payment/refund/create", r.URL.Path)
				status := 3
				if calls == 2 {
					status = 1
				}
				return refundProviderResponse(t, r, "provider-paid", status), nil
			})
			first, err := createRefund(userID, request)
			require.NoError(t, err)
			require.Equal(t, commerceschema.RefundStatusFailed, first.Status)
			items, err := listRefundableOrders(userID)
			require.NoError(t, err)
			require.Len(t, items, 1)
			require.True(t, items[0].Refundable)
			terminal, err := syncRefund(userID, first.RefundNo)
			require.NoError(t, err)
			require.Equal(t, commerceschema.RefundStatusFailed, terminal.Status)
			require.Equal(t, 1, calls)
			second, err := createRefund(userID, request)
			require.NoError(t, err)
			require.Equal(t, commerceschema.RefundStatusProcessing, second.Status)
			require.NotEqual(t, first.RefundNo, second.RefundNo)
			require.Equal(t, 2, calls)
			state, err := loadRefundOrderTx(db, userID, request.OrderType, request.TradeNo)
			require.NoError(t, err)
			require.NoError(t, validateRefundNativeHoldTx(db, userID, state.record, billingschema.BillingReservationStatusOpen))
		})
	}
}
