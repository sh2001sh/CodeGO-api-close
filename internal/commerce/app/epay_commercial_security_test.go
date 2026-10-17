package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func commercialEpayParams(tradeNo string, money float64) map[string]string {
	return map[string]string{"pid": "test-merchant", "out_trade_no": tradeNo, "trade_no": "commercial-provider-original", "type": "alipay", "money": fmt.Sprintf("%.2f", money), "trade_status": epay.StatusTradeSuccess}
}

func commercialSnapshot(t *testing.T, db *gorm.DB) []any {
	t.Helper()
	snapshot := epayLateSnapshot(t, db)
	for _, model := range []any{&commerceschema.SubscriptionOrder{}, &commerceschema.UserSubscription{}, &commerceschema.BlindBoxOrder{}, &commerceschema.BalanceBlindBoxItem{}} {
		var rows []map[string]any
		require.NoError(t, db.Model(model).Order("id").Find(&rows).Error)
		snapshot = append(snapshot, rows)
	}
	return snapshot
}

func seedCommercialSubscription(t *testing.T, db *gorm.DB, card bool) (*commerceschema.SubscriptionOrder, *commerceschema.BlindBoxProp) {
	t.Helper()
	user := &identityschema.User{Id: 39771, Username: "commercial-epay", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	order := &commerceschema.SubscriptionOrder{UserId: user.Id, PlanId: 1, Money: 64, OriginalMoney: 64, TradeNo: "commercial-subscription", Status: constant.TopUpStatusPending, PaymentMethod: "alipay", PaymentProvider: commerceschema.PaymentProviderEpay, FulfillmentStatus: commerceschema.SubscriptionOrderFulfillmentPending}
	var prop *commerceschema.BlindBoxProp
	if card {
		prop = &commerceschema.BlindBoxProp{UserId: user.Id, PropType: commerceschema.BlindBoxPropTypeSubscriptionDiscount90, Status: commerceschema.BlindBoxPropStatusReserved, DiscountRate: 0.1, ReservedOrderTradeNo: order.TradeNo, ReservedOrderType: commerceschema.BlindBoxPropOrderTypeSubscription}
		require.NoError(t, db.Create(prop).Error)
		order.Money = 57.6
	}
	propID := 0
	if prop != nil {
		propID = prop.Id
	}
	require.NoError(t, snapshotSubscriptionEpayCheckout(order, propID))
	require.NoError(t, db.Create(order).Error)
	return order, prop
}

func deliverCommercialSubscription(params map[string]string) error {
	verified, err := VerifySubscriptionEpay(epay.GenerateParams(params, "synthetic-epay-key"))
	if err != nil {
		return err
	}
	return CompleteSubscriptionEpayPayment(verified)
}

func TestCommercialEpayRejectsSignedMismatchWithoutMutation(t *testing.T) {
	for _, product := range []string{"subscription", "blind-box"} {
		for _, scenario := range []string{"underpaid", "overpaid", "subcent", "nan", "empty-money", "wrong-method", "wrong-merchant", "wrong-provider", "wrong-currency", "empty-provider-id", "wrong-order", "failed", "unverified"} {
			t.Run(product+"/"+scenario, func(t *testing.T) {
				db := setupRedemptionTestDB(t)
				configureEpayLateTest(t)
				var tradeNo string
				var model any
				if product == "subscription" {
					order, _ := seedCommercialSubscription(t, db, false)
					tradeNo, model = order.TradeNo, order
				} else {
					order := &commerceschema.BlindBoxOrder{UserId: 39772, Quantity: 3, Money: 64, TradeNo: "commercial-blind-box", PaymentMethod: "alipay", PaymentProvider: commerceschema.PaymentProviderEpay, Status: constant.TopUpStatusPending}
					require.NoError(t, db.Create(order).Error)
					tradeNo, model = order.TradeNo, order
				}
				params := commercialEpayParams(tradeNo, 64)
				switch scenario {
				case "underpaid":
					params["money"] = "0.01"
				case "overpaid":
					params["money"] = "65.00"
				case "subcent":
					params["money"] = "64.001"
				case "nan":
					params["money"] = "NaN"
				case "empty-money":
					params["money"] = ""
				case "wrong-method":
					params["type"] = "wxpay"
				case "wrong-merchant":
					params["pid"] = "foreign-merchant"
				case "wrong-provider":
					require.NoError(t, db.Model(model).Update("payment_provider", "stripe").Error)
				case "wrong-currency":
					params["currency"] = "USD"
				case "empty-provider-id":
					params["trade_no"] = ""
				case "wrong-order":
					params["out_trade_no"] = "unknown-order"
				case "failed":
					require.NoError(t, db.Model(model).Update("status", constant.TopUpStatusFailed).Error)
				}
				before := commercialSnapshot(t, db)
				verified, err := verifyConfiguredEpayOrder(epay.GenerateParams(params, "synthetic-epay-key"))
				if err == nil {
					if scenario == "unverified" {
						verified.VerifyStatus = false
					}
					if product == "subscription" {
						err = CompleteSubscriptionEpayPayment(verified)
					} else {
						err = CompleteBlindBoxEpayPayment(verified)
					}
				}
				require.Error(t, err)
				require.Equal(t, before, commercialSnapshot(t, db))
			})
		}
	}
}

func TestCommercialEpayVerifierRetainsRejectedSignatureAcrossRetries(t *testing.T) {
	configureEpayLateTest(t)
	params := epay.GenerateParams(commercialEpayParams("test", 64), "synthetic-epay-key")
	params["sign"] = "invalid"
	for _, verify := range []func(map[string]string) (*epay.VerifyRes, error){VerifySubscriptionEpay, VerifyBlindBoxEpay} {
		for i := 0; i < 2; i++ {
			_, err := verify(params)
			require.Error(t, err)
			require.Equal(t, "invalid", params["sign"])
			require.Equal(t, "MD5", params["sign_type"])
		}
	}
}

func TestCommercialEpaySubscriptionOriginalEvidenceAndReplay(t *testing.T) {
	for _, scenario := range []string{"pending", "expired-no-card", "reserved-card", "expired-reserved-card", "legacy-reserved-card", "legacy-permanent-pack"} {
		t.Run(scenario, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			configureEpayLateTest(t)
			order, prop := seedCommercialSubscription(t, db, strings.Contains(scenario, "card") && scenario != "expired-no-card")
			if strings.HasPrefix(scenario, "expired") {
				require.NoError(t, db.Model(order).Update("status", constant.TopUpStatusExpired).Error)
			}
			if strings.HasPrefix(scenario, "legacy") {
				require.NoError(t, db.Model(order).Update("provider_payload", "").Error)
			}
			if scenario == "legacy-permanent-pack" {
				require.NoError(t, db.Model(order).Updates(map[string]any{"credit_pack_quota": quotaUnitsFromUSD(67.2), "credit_pack_paid_quota": quotaUnitsFromUSD(64)}).Error)
			}
			require.NoError(t, deliverCommercialSubscription(commercialEpayParams(order.TradeNo, order.Money)))
			require.NoError(t, db.First(order, order.Id).Error)
			require.Equal(t, constant.TopUpStatusSuccess, order.Status)
			require.Equal(t, commerceschema.SubscriptionOrderFulfillmentPending, order.FulfillmentStatus)
			if prop != nil {
				require.NoError(t, db.First(prop, prop.Id).Error)
				require.Equal(t, commerceschema.BlindBoxPropStatusUsed, prop.Status)
			}
			for _, refund := range []string{"", "processing", "success"} {
				require.NoError(t, db.Model(order).Update("refund_status", refund).Error)
				before := commercialSnapshot(t, db)
				require.NoError(t, deliverCommercialSubscription(commercialEpayParams(order.TradeNo, order.Money)))
				require.Equal(t, before, commercialSnapshot(t, db))
				params := commercialEpayParams(order.TradeNo, order.Money)
				params["trade_no"] = "foreign-provider-order"
				require.Error(t, deliverCommercialSubscription(params))
				require.Equal(t, before, commercialSnapshot(t, db))
			}
		})
	}
}

func TestCommercialEpaySubscriptionRejectsReleasedCardAndRefundMarkers(t *testing.T) {
	for _, scenario := range []string{"released", "gifted", "reused", "used", "unknown-legacy", "refund-status", "refund-no", "refund-quota"} {
		t.Run(scenario, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			configureEpayLateTest(t)
			order, prop := seedCommercialSubscription(t, db, !strings.Contains(scenario, "legacy"))
			require.NoError(t, db.Model(order).Update("status", constant.TopUpStatusExpired).Error)
			switch scenario {
			case "released":
				require.NoError(t, db.Model(prop).Update("status", commerceschema.BlindBoxPropStatusAvailable).Error)
			case "gifted":
				require.NoError(t, db.Model(prop).Update("user_id", order.UserId+1).Error)
			case "reused":
				require.NoError(t, db.Model(prop).Update("reserved_order_trade_no", "another-order").Error)
			case "used":
				require.NoError(t, db.Model(prop).Update("status", commerceschema.BlindBoxPropStatusUsed).Error)
			case "unknown-legacy":
				require.NoError(t, db.Model(order).Update("provider_payload", "").Error)
			case "refund-status":
				require.NoError(t, db.Model(order).Update("refund_status", "processing").Error)
			case "refund-no":
				require.NoError(t, db.Model(order).Update("refund_no", "refund-original").Error)
			case "refund-quota":
				require.NoError(t, db.Model(order).Update("refund_quota", 1).Error)
			}
			before := commercialSnapshot(t, db)
			require.Error(t, deliverCommercialSubscription(commercialEpayParams(order.TradeNo, order.Money)))
			require.Equal(t, before, commercialSnapshot(t, db))
		})
	}
}

func TestCommercialEpaySubscriptionPaymentRollbackIncludesOriginalCard(t *testing.T) {
	db := setupRedemptionTestDB(t)
	configureEpayLateTest(t)
	order, _ := seedCommercialSubscription(t, db, true)
	before := commercialSnapshot(t, db)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("commercial-order-failure", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "SubscriptionOrder" {
			tx.AddError(errors.New("synthetic order update failure"))
		}
	}))
	require.ErrorContains(t, deliverCommercialSubscription(commercialEpayParams(order.TradeNo, order.Money)), "synthetic order update failure")
	require.Equal(t, before, commercialSnapshot(t, db))
}

func TestCommercialEpayBlindBoxExpiredReplayIssuesInventoryOnce(t *testing.T) {
	db := setupRedemptionTestDB(t)
	configureEpayLateTest(t)
	user := &identityschema.User{Id: 39773, Username: "commercial-bb", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	order := &commerceschema.BlindBoxOrder{UserId: user.Id, Quantity: 3, Money: 64, TradeNo: "commercial-expired-bb", PaymentMethod: "alipay", PaymentProvider: commerceschema.PaymentProviderEpay, Status: constant.TopUpStatusExpired, CreateTime: time.Now().Unix()}
	require.NoError(t, db.Create(order).Error)
	verified, err := VerifyBlindBoxEpay(epay.GenerateParams(commercialEpayParams(order.TradeNo, order.Money), "synthetic-epay-key"))
	require.NoError(t, err)
	require.NoError(t, CompleteBlindBoxEpayPayment(verified))
	before := commercialSnapshot(t, db)
	require.NoError(t, CompleteBlindBoxEpayPayment(verified))
	require.Equal(t, before, commercialSnapshot(t, db))
	var count int64
	require.NoError(t, db.Model(&commerceschema.BalanceBlindBoxItem{}).Where("owner_user_id = ?", user.Id).Count(&count).Error)
	require.EqualValues(t, 3, count)
	verified.TradeNo = "foreign-provider-order"
	require.Error(t, CompleteBlindBoxEpayPayment(verified))
	require.Equal(t, before, commercialSnapshot(t, db))
}

func TestCommercialFulfillmentDoesNotGrantDuringRefund(t *testing.T) {
	db := setupRedemptionTestDB(t)
	order, _ := seedCommercialSubscription(t, db, false)
	require.NoError(t, db.Model(order).Updates(map[string]any{"status": constant.TopUpStatusSuccess, "refund_status": "processing"}).Error)
	before := commercialSnapshot(t, db)
	require.ErrorIs(t, FulfillPaidSubscriptionOrder(order.TradeNo), commerceschema.ErrSubscriptionOrderStatusInvalid)
	require.Equal(t, before, commercialSnapshot(t, db))
}

func TestCommercialEpayPayloadRejectsNonverifiedAndNonfiniteOriginal(t *testing.T) {
	paid := &epay.VerifyRes{VerifyStatus: true, ServiceTradeNo: "order", TradeNo: "provider", Type: "alipay", Money: "64.00", TradeStatus: epay.StatusTradeSuccess}
	payload, err := json.Marshal(paid)
	require.NoError(t, err)
	_, err = validateEpayOrderPayment(string(payload), "order", "alipay", 64)
	require.NoError(t, err)
	for _, money := range []float64{-1, 0, math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err = validateEpayOrderPayment(string(payload), "order", "alipay", money)
		require.Error(t, err)
	}
}
