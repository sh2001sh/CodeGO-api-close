package app

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/constant"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
	waffo "github.com/waffo-com/waffo-go"
	waffoconfig "github.com/waffo-com/waffo-go/config"
	waffocore "github.com/waffo-com/waffo-go/core"
	waffonet "github.com/waffo-com/waffo-go/net"
)

type commercialWaffoTransport func(context.Context, *waffonet.HttpRequest) (*waffonet.HttpResponse, error)

func (f commercialWaffoTransport) Send(ctx context.Context, req *waffonet.HttpRequest) (*waffonet.HttpResponse, error) {
	return f(ctx, req)
}

type commercialHTTPTransport func(*http.Request) (*http.Response, error)

func (f commercialHTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestCommercialWaffoIntermediateEventsPreservePendingCardAndEventuallySettle(t *testing.T) {
	for _, scenario := range []string{"progress-to-success", "closed", "foreign-provider"} {
		t.Run(scenario, func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			user, order, prop := seedEpayLateOrder(t, db, true)
			provider := commerceschema.PaymentProviderWaffo
			if scenario == "foreign-provider" {
				provider = commerceschema.PaymentProviderStripe
			}
			require.NoError(t, db.Model(order).Updates(map[string]any{"payment_provider": provider, "payment_method": commerceschema.PaymentMethodWaffo}).Error)
			before := epayLateSnapshot(t, db)
			for _, state := range []string{waffocore.OrderStatusPayInProgress, waffocore.OrderStatusAuthorizationRequired, waffocore.OrderStatusAuthedWaitingCapture, "UNKNOWN_PROVIDER_EVENT", ""} {
				require.NoError(t, HandleWaffoPaymentStatus(context.Background(), order.TradeNo, state, "127.0.0.1"))
				require.Equal(t, before, epayLateSnapshot(t, db))
			}
			if scenario == "progress-to-success" {
				require.NoError(t, HandleWaffoPaymentStatus(context.Background(), order.TradeNo, waffocore.OrderStatusPaySuccess, "127.0.0.1"))
				require.NoError(t, db.First(order, order.Id).Error)
				require.Equal(t, constant.TopUpStatusSuccess, order.Status)
				require.NoError(t, db.First(prop, prop.Id).Error)
				require.Equal(t, commerceschema.BlindBoxPropStatusUsed, prop.Status)
				var saved identityschema.User
				require.NoError(t, db.First(&saved, user.Id).Error)
				require.Equal(t, user.ClaudeQuota+calculateTopUpQuotaToAdd(order), saved.ClaudeQuota)
				settled := epayLateSnapshot(t, db)
				require.NoError(t, HandleWaffoPaymentStatus(context.Background(), order.TradeNo, waffocore.OrderStatusPaySuccess, "127.0.0.1"))
				require.NoError(t, HandleWaffoPaymentStatus(context.Background(), order.TradeNo, waffocore.OrderStatusOrderClose, "127.0.0.1"))
				require.Equal(t, settled, epayLateSnapshot(t, db))
			} else if scenario == "closed" {
				require.NoError(t, HandleWaffoPaymentStatus(context.Background(), order.TradeNo, waffocore.OrderStatusOrderClose, "127.0.0.1"))
				require.NoError(t, db.First(order, order.Id).Error)
				require.Equal(t, constant.TopUpStatusFailed, order.Status)
				require.NoError(t, db.First(prop, prop.Id).Error)
				require.Equal(t, commerceschema.BlindBoxPropStatusAvailable, prop.Status)
				require.Error(t, HandleWaffoPaymentStatus(context.Background(), order.TradeNo, waffocore.OrderStatusPaySuccess, "127.0.0.1"))
			} else {
				require.Error(t, HandleWaffoPaymentStatus(context.Background(), order.TradeNo, waffocore.OrderStatusOrderClose, "127.0.0.1"))
				require.Equal(t, before, epayLateSnapshot(t, db))
			}
		})
	}
}

func TestCommercialWaffoCheckoutSendsFrozenDiscountedAmount(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	for _, provider := range []string{"waffo", "pancake"} {
		for _, failed := range []bool{false, true} {
			name := provider + "/success"
			if failed {
				name = provider + "/provider-failure"
			}
			t.Run(name, func(t *testing.T) {
				db := setupRedemptionTestDB(t)
				confirmPaymentComplianceForTest(t)
				user := &identityschema.User{Id: 39781, Username: "waffo-checkout", Status: constant.UserStatusEnabled, Group: "default"}
				require.NoError(t, db.Create(user).Error)
				prop := &commerceschema.BlindBoxProp{UserId: user.Id, PropType: commerceschema.BlindBoxPropTypeTopupDiscount90, Status: commerceschema.BlindBoxPropStatusAvailable, DiscountRate: 0.1}
				require.NoError(t, db.Create(prop).Error)
				requests := 0
				var tradeNo string
				if provider == "waffo" {
					oldEnabled, oldSandbox, oldAPI, oldPrivate, oldPublic, oldCurrency, oldFactory := commercestore.WaffoEnabled, commercestore.WaffoSandbox, commercestore.WaffoSandboxApiKey, commercestore.WaffoSandboxPrivateKey, commercestore.WaffoSandboxPublicCert, commercestore.WaffoCurrency, newWaffoSDK
					t.Cleanup(func() {
						commercestore.WaffoEnabled, commercestore.WaffoSandbox, commercestore.WaffoSandboxApiKey, commercestore.WaffoSandboxPrivateKey, commercestore.WaffoSandboxPublicCert, commercestore.WaffoCurrency, newWaffoSDK = oldEnabled, oldSandbox, oldAPI, oldPrivate, oldPublic, oldCurrency, oldFactory
					})
					commercestore.WaffoEnabled, commercestore.WaffoSandbox, commercestore.WaffoCurrency = true, true, "CNY"
					commercestore.WaffoSandboxApiKey = "synthetic-api-key"
					commercestore.WaffoSandboxPrivateKey, commercestore.WaffoSandboxPublicCert = base64.StdEncoding.EncodeToString(privateDER), base64.StdEncoding.EncodeToString(publicDER)
					newWaffoSDK = func(cfg *waffoconfig.WaffoConfig) *waffo.Waffo {
						cfg.CustomTransport = commercialWaffoTransport(func(_ context.Context, req *waffonet.HttpRequest) (*waffonet.HttpResponse, error) {
							requests++
							var payload struct {
								Amount  string `json:"orderAmount"`
								TradeNo string `json:"merchantOrderId"`
							}
							require.NoError(t, json.Unmarshal(req.Body, &payload))
							require.Equal(t, "9.00", payload.Amount)
							tradeNo = payload.TradeNo
							var frozen commerceschema.TopUp
							require.NoError(t, db.First(&frozen, "trade_no = ?", tradeNo).Error)
							require.Equal(t, 9.0, frozen.Money)
							if failed {
								return nil, errors.New("synthetic offline provider")
							}
							return waffonet.NewHttpResponse(200, nil, []byte(`{"code":"0","data":{"orderAction":"https://checkout.example.test/waffo"}}`)), nil
						})
						return waffo.New(cfg)
					}
					_, err = CreateWaffoTopUp(context.Background(), user.Id, WaffoPayRequest{Amount: 10, WalletType: "claude"})
				} else {
					oldEnabled, oldSandbox, oldMerchant, oldPrivate, oldWebhook, oldStore, oldProduct, oldCurrency, oldHTTP := commercestore.WaffoPancakeEnabled, commercestore.WaffoPancakeSandbox, commercestore.WaffoPancakeMerchantID, commercestore.WaffoPancakePrivateKey, commercestore.WaffoPancakeWebhookTestKey, commercestore.WaffoPancakeStoreID, commercestore.WaffoPancakeProductID, commercestore.WaffoPancakeCurrency, http.DefaultClient
					t.Cleanup(func() {
						commercestore.WaffoPancakeEnabled, commercestore.WaffoPancakeSandbox, commercestore.WaffoPancakeMerchantID, commercestore.WaffoPancakePrivateKey, commercestore.WaffoPancakeWebhookTestKey, commercestore.WaffoPancakeStoreID, commercestore.WaffoPancakeProductID, commercestore.WaffoPancakeCurrency, http.DefaultClient = oldEnabled, oldSandbox, oldMerchant, oldPrivate, oldWebhook, oldStore, oldProduct, oldCurrency, oldHTTP
					})
					commercestore.WaffoPancakeEnabled, commercestore.WaffoPancakeSandbox, commercestore.WaffoPancakeMerchantID = true, true, "synthetic-merchant"
					commercestore.WaffoPancakePrivateKey = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}))
					commercestore.WaffoPancakeWebhookTestKey, commercestore.WaffoPancakeStoreID, commercestore.WaffoPancakeProductID, commercestore.WaffoPancakeCurrency = "synthetic-public-key", "store", "product", "CNY"
					http.DefaultClient = &http.Client{Transport: commercialHTTPTransport(func(req *http.Request) (*http.Response, error) {
						requests++
						require.Equal(t, waffoPancakeAuthBaseURL+waffoPancakeCheckoutPath, req.URL.String())
						require.NotEmpty(t, req.Header.Get("X-Signature"))
						var payload WaffoPancakeCreateSessionParams
						require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
						require.Equal(t, "9.00", payload.PriceSnapshot.Amount)
						var frozen commerceschema.TopUp
						require.NoError(t, db.Where("payment_provider = ?", commerceschema.PaymentProviderWaffoPancake).First(&frozen).Error)
						require.Equal(t, 9.0, frozen.Money)
						tradeNo = frozen.TradeNo
						if failed {
							return nil, errors.New("synthetic offline provider")
						}
						return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":{"sessionId":"synthetic-session","checkoutUrl":"https://checkout.example.test/pancake","orderId":"synthetic-provider-order"}}`))}, nil
					})}
					_, err = CreateWaffoPancakeTopUp(context.Background(), user.Id, WaffoPancakePayRequest{Amount: 10, WalletType: "claude"})
				}
				require.Equal(t, 1, requests, "checkout must use only the intercepted local transport")
				if failed {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				var frozen commerceschema.TopUp
				require.NoError(t, db.First(&frozen, "trade_no = ?", tradeNo).Error)
				require.Equal(t, 9.0, frozen.Money)
				require.NoError(t, db.First(prop, prop.Id).Error)
				if failed {
					require.Equal(t, constant.TopUpStatusFailed, frozen.Status)
					require.Equal(t, commerceschema.BlindBoxPropStatusAvailable, prop.Status)
				} else {
					require.Equal(t, constant.TopUpStatusPending, frozen.Status)
					require.Equal(t, commerceschema.BlindBoxPropStatusReserved, prop.Status)
					require.Equal(t, tradeNo, prop.ReservedOrderTradeNo)
				}
			})
		}
	}
}
