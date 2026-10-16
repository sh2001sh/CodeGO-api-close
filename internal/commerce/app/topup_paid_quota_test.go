package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTopupPaidQuotaFreezesCheckoutDiscountForWalletAndForeignCurrency(t *testing.T) {
	for _, provider := range []string{commerceschema.PaymentProviderEpay, PaymentProviderNowPayments} {
		t.Run(provider, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			user := &identityschema.User{Id: 8921, Username: "paid_snapshot_" + provider, Status: constant.UserStatusEnabled}
			require.NoError(t, db.Create(user).Error)
			var prop *commerceschema.BlindBoxProp
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
				var err error
				prop, err = createBlindBoxPropTx(tx, user.Id, 11, "九折充值卡")
				return err
			}))
			money := 100.0
			if provider == PaymentProviderNowPayments {
				money = 10 // USDT checkout price, not a CNY credit amount.
			}
			order := &commerceschema.TopUp{UserId: user.Id, Amount: 100, Money: money,
				TradeNo: "paid-quota-" + provider, PaymentProvider: provider, Status: constant.TopUpStatusPending,
				CreateTime: time.Now().Unix()}
			_, err := CreatePendingTopUpOrderWithBlindBoxDiscount(order)
			require.NoError(t, err)
			require.Equal(t, quotaUnitsFromUSD(90), order.PaidQuota)
			require.NoError(t, db.Model(prop).Update("discount_rate", .5).Error)
			oldPrice := commercestore.Price
			commercestore.Price = 999
			t.Cleanup(func() { commercestore.Price = oldPrice })

			paid, quota, err := CompleteTopUpByTradeNo(order.TradeNo, provider, "", "", "")
			require.NoError(t, err)
			require.Equal(t, quotaUnitsFromUSD(100), int64(quota))
			require.Equal(t, order.PaidQuota, paid.PaidQuota)
			var lot billingschema.FundingLot
			require.NoError(t, db.Where("idempotency_key = ?", "topup:"+order.TradeNo+":unified").First(&lot).Error)
			require.Equal(t, quotaUnitsFromUSD(90), lot.PaidQuota)
			require.Equal(t, int64(quota), lot.OriginalAmount)
			_, replayQuota, err := CompleteTopUpByTradeNo(order.TradeNo, provider, "", "", "")
			require.NoError(t, err)
			require.Zero(t, replayQuota)
			var count int64
			require.NoError(t, db.Model(&billingschema.FundingLot{}).Where("idempotency_key = ?", lot.IdempotencyKey).Count(&count).Error)
			require.EqualValues(t, 1, count)
		})
	}
}

type paidQuotaCreemTransport struct{}

func (paidQuotaCreemTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: request,
		Body: io.NopCloser(strings.NewReader(`{"checkout_url":"https://checkout.example.test/test"}`))}, nil
}

func TestCreemPaidQuotaExcludesProductBonusAndUnknownCurrency(t *testing.T) {
	for _, tc := range []struct {
		name        string
		currency    string
		price       float64
		paidCredits float64
	}{
		{name: "CNY_product_bonus", currency: "CNY", price: 100, paidCredits: 90},
		{name: "CNY_capped_to_grant", currency: "CNY", price: 200, paidCredits: 110},
		{name: "USD_unknown_rate", currency: "USD", price: 100},
		{name: "missing_currency", price: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			user := &identityschema.User{Id: 8922, Username: "creem_paid_" + tc.name, Email: "test@example.test", Status: constant.UserStatusEnabled}
			require.NoError(t, db.Create(user).Error)
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
				_, err := createBlindBoxPropTx(tx, user.Id, 12, "九折充值卡")
				return err
			}))
			products, err := json.Marshal([]CreemProduct{{ProductID: "prod", Price: tc.price, Currency: tc.currency, Quota: 110}})
			require.NoError(t, err)
			oldProducts, oldKey, oldTransport := commercestore.CreemProducts, commercestore.CreemApiKey, http.DefaultTransport
			commercestore.CreemProducts, commercestore.CreemApiKey = string(products), "test-creem-key"
			http.DefaultTransport = paidQuotaCreemTransport{}
			t.Cleanup(func() {
				commercestore.CreemProducts, commercestore.CreemApiKey, http.DefaultTransport = oldProducts, oldKey, oldTransport
			})
			checkout, err := CreateCreemTopUp(context.Background(), user.Id, CreemPayRequest{ProductID: "prod", PaymentMethod: commerceschema.PaymentMethodCreem})
			require.NoError(t, err)
			order := GetTopUpByTradeNo(checkout.OrderID)
			require.NotNil(t, order)
			expectedPaid := quotaUnitsFromUSD(tc.paidCredits)
			require.Equal(t, expectedPaid, order.PaidQuota)
			commercestore.CreemProducts = `[{"productId":"prod","price":1,"currency":"CNY","quota":999}]`
			paid, quota, err := CompleteTopUpByTradeNo(order.TradeNo, commerceschema.PaymentProviderCreem, "", "", "")
			require.NoError(t, err)
			require.Equal(t, quotaUnitsFromUSD(110), int64(quota))
			require.Equal(t, expectedPaid, paid.PaidQuota)
			var lot billingschema.FundingLot
			require.NoError(t, db.Where("idempotency_key = ?", "topup:"+order.TradeNo+":unified").First(&lot).Error)
			require.Equal(t, expectedPaid, lot.PaidQuota)
		})
	}
}

func TestTopupPaidQuotaLegacyOrderCreditsAndInvalidSnapshotRollsBack(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		name := "legacy_zero"
		if invalid {
			name = "invalid_snapshot"
		}
		t.Run(name, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			user := &identityschema.User{Id: 8923, Username: name, Status: constant.UserStatusEnabled}
			require.NoError(t, db.Create(user).Error)
			order := &commerceschema.TopUp{UserId: user.Id, Amount: 10, Money: 10,
				TradeNo: name, Status: constant.TopUpStatusPending, CreateTime: time.Now().Unix()}
			if invalid {
				order.PaidQuota = quotaUnitsFromUSD(11)
			}
			require.NoError(t, db.Create(order).Error)
			_, _, err := CompleteTopUpByTradeNo(order.TradeNo, "", "", "", "")
			if invalid {
				require.ErrorContains(t, err, "paid topup quota snapshot conflict")
				require.NoError(t, db.First(order, order.Id).Error)
				require.Equal(t, constant.TopUpStatusPending, order.Status)
				require.NoError(t, db.First(user, user.Id).Error)
				require.Zero(t, user.ClaudeQuota)
				var count int64
				require.NoError(t, db.Model(&billingschema.FundingLot{}).Count(&count).Error)
				require.Zero(t, count)
				return
			}
			require.NoError(t, err)
			var lot billingschema.FundingLot
			require.NoError(t, db.Where("idempotency_key = ?", "topup:"+order.TradeNo+":unified").First(&lot).Error)
			require.Zero(t, lot.PaidQuota)
			require.Equal(t, quotaUnitsFromUSD(10), lot.OriginalAmount)
		})
	}
}
