package paymentsettings

import (
	"github.com/sh2001sh/new-api/constant"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	settingconfig "github.com/sh2001sh/new-api/setting/config"
)

type PaymentSetting struct {
	AmountOptions  []int           `json:"amount_options"`
	AmountDiscount map[int]float64 `json:"amount_discount"`

	FirstPurchaseDiscountEnabled    bool    `json:"first_purchase_discount_enabled"`
	FirstPurchaseDiscountMultiplier float64 `json:"first_purchase_discount_multiplier"`
	FirstPurchaseDiscountStartAt    int64   `json:"first_purchase_discount_start_at"`
	FirstPurchaseDiscountEndAt      int64   `json:"first_purchase_discount_end_at"`

	ComplianceConfirmed    bool   `json:"compliance_confirmed"`
	ComplianceTermsVersion string `json:"compliance_terms_version"`
	ComplianceConfirmedAt  int64  `json:"compliance_confirmed_at"`
	ComplianceConfirmedBy  int    `json:"compliance_confirmed_by"`
	ComplianceConfirmedIP  string `json:"compliance_confirmed_ip"`
}

const CurrentComplianceTermsVersion = "v1"

var paymentSetting = PaymentSetting{
	AmountOptions:                   []int{10, 20, 50, 100, 200, 500},
	AmountDiscount:                  map[int]float64{},
	FirstPurchaseDiscountMultiplier: 0.8,
}

var PayAddress = ""
var CustomCallbackAddress = ""
var EpayId = ""
var EpayKey = ""
var Price = 7.3
var MinTopUp = 1
var USDExchangeRate = 7.3

var StripeApiSecret = ""
var StripeWebhookSecret = ""
var StripePriceId = ""
var StripeUnitPrice = 8.0
var StripeMinTopUp = 1
var StripePromotionCodesEnabled = false

var CreemApiKey = ""
var CreemProducts = "[]"
var CreemTestMode = false
var CreemWebhookSecret = ""

var XunhuEnabled = false
var XunhuAppID = ""
var XunhuSecret = ""
var XunhuGateway = "https://api.xunhupay.com/payment/do.html"
var XunhuMinTopUp = 10

var (
	WaffoEnabled               bool
	WaffoApiKey                string
	WaffoPrivateKey            string
	WaffoPublicCert            string
	WaffoSandboxPublicCert     string
	WaffoSandboxApiKey         string
	WaffoSandboxPrivateKey     string
	WaffoSandbox               bool
	WaffoMerchantId            string
	WaffoNotifyUrl             string
	WaffoReturnUrl             string
	WaffoSubscriptionReturnUrl string
	WaffoCurrency              string
	WaffoUnitPrice             float64 = 1.0
	WaffoMinTopUp              int     = 1
)

var (
	WaffoPancakeEnabled          bool
	WaffoPancakeSandbox          bool
	WaffoPancakeMerchantID       string
	WaffoPancakePrivateKey       string
	WaffoPancakeWebhookPublicKey string
	WaffoPancakeWebhookTestKey   string
	WaffoPancakeStoreID          string
	WaffoPancakeProductID        string
	WaffoPancakeReturnURL        string
	WaffoPancakeCurrency         string  = "USD"
	WaffoPancakeUnitPrice        float64 = 1.0
	WaffoPancakeMinTopUp         int     = 1
)

var PayMethods = []map[string]string{
	{
		"name":  "支付宝",
		"color": "rgba(var(--semi-blue-5), 1)",
		"type":  "alipay",
	},
	{
		"name":  "微信",
		"color": "rgba(var(--semi-green-5), 1)",
		"type":  "wxpay",
	},
	{
		"name":      "自定义1",
		"color":     "black",
		"type":      "custom1",
		"min_topup": "50",
	},
}

func init() {
	settingconfig.GlobalConfig.Register("payment_setting", &paymentSetting)
}

func GetPaymentSetting() *PaymentSetting {
	return &paymentSetting
}

func IsPaymentComplianceConfirmed() bool {
	return paymentSetting.ComplianceConfirmed &&
		paymentSetting.ComplianceTermsVersion == CurrentComplianceTermsVersion
}

func UpdatePayMethodsByJsonString(jsonString string) error {
	PayMethods = make([]map[string]string, 0)
	return platformencoding.Unmarshal([]byte(jsonString), &PayMethods)
}

func PayMethods2JsonString() string {
	jsonBytes, err := platformencoding.Marshal(PayMethods)
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}

func ContainsPayMethod(method string) bool {
	for _, payMethod := range PayMethods {
		if payMethod["type"] == method {
			return true
		}
	}
	return false
}

func GetWaffoPayMethods() []constant.WaffoPayMethod {
	platformconfig.OptionMapRWMutex.RLock()
	jsonStr := platformconfig.OptionMap["WaffoPayMethods"]
	platformconfig.OptionMapRWMutex.RUnlock()

	if jsonStr == "" {
		return copyDefaultWaffoPayMethods()
	}
	var methods []constant.WaffoPayMethod
	if err := platformencoding.UnmarshalString(jsonStr, &methods); err != nil {
		return copyDefaultWaffoPayMethods()
	}
	return methods
}

func SetWaffoPayMethods(methods []constant.WaffoPayMethod) error {
	jsonBytes, err := platformencoding.Marshal(methods)
	if err != nil {
		return err
	}
	platformconfig.OptionMapRWMutex.Lock()
	platformconfig.OptionMap["WaffoPayMethods"] = string(jsonBytes)
	platformconfig.OptionMapRWMutex.Unlock()
	return nil
}

func copyDefaultWaffoPayMethods() []constant.WaffoPayMethod {
	cp := make([]constant.WaffoPayMethod, len(constant.DefaultWaffoPayMethods))
	copy(cp, constant.DefaultWaffoPayMethods)
	return cp
}

func WaffoPayMethods2JsonString() string {
	jsonBytes, err := platformencoding.Marshal(constant.DefaultWaffoPayMethods)
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}
