package app

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/Calcium-Ion/go-epay/epay"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformgeneral "github.com/sh2001sh/new-api/internal/platform/general"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/shopspring/decimal"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func IsPaymentComplianceConfirmed() bool {
	return commercestore.IsPaymentComplianceConfirmed()
}

func IsStripeTopUpEnabled() bool {
	if !IsPaymentComplianceConfirmed() {
		return false
	}
	return strings.TrimSpace(commercestore.StripeApiSecret) != "" &&
		strings.TrimSpace(commercestore.StripeWebhookSecret) != "" &&
		strings.TrimSpace(commercestore.StripePriceId) != ""
}

func IsCreemTopUpEnabled() bool {
	if !IsPaymentComplianceConfirmed() {
		return false
	}
	products := strings.TrimSpace(commercestore.CreemProducts)
	return strings.TrimSpace(commercestore.CreemApiKey) != "" &&
		products != "" &&
		products != "[]"
}

func IsWaffoTopUpEnabled() bool {
	if !IsPaymentComplianceConfirmed() {
		return false
	}
	if !commercestore.WaffoEnabled {
		return false
	}
	return isWaffoWebhookConfigured()
}

func IsWaffoPancakeTopUpEnabled() bool {
	if !IsPaymentComplianceConfirmed() {
		return false
	}
	if !commercestore.WaffoPancakeEnabled {
		return false
	}
	return isWaffoPancakeWebhookConfigured() &&
		strings.TrimSpace(commercestore.WaffoPancakeMerchantID) != "" &&
		strings.TrimSpace(commercestore.WaffoPancakePrivateKey) != "" &&
		strings.TrimSpace(commercestore.WaffoPancakeStoreID) != "" &&
		strings.TrimSpace(commercestore.WaffoPancakeProductID) != ""
}

func IsEpayTopUpEnabled() bool {
	if !IsPaymentComplianceConfirmed() {
		return false
	}
	return isEpayWebhookConfigured() && len(commercestore.PayMethods) > 0
}

func IsEpayWebhookEnabled() bool {
	return IsEpayTopUpEnabled()
}

func IsCreemWebhookEnabled() bool {
	return IsCreemTopUpEnabled() && strings.TrimSpace(commercestore.CreemWebhookSecret) != ""
}

func IsWaffoWebhookEnabled() bool {
	return IsWaffoTopUpEnabled()
}

func IsWaffoPancakeWebhookEnabled() bool {
	return IsWaffoPancakeTopUpEnabled()
}

func IsXunhuTopUpEnabled() bool {
	if !IsPaymentComplianceConfirmed() {
		return false
	}
	if !commercestore.XunhuEnabled {
		return false
	}
	return strings.TrimSpace(commercestore.XunhuAppID) != "" &&
		strings.TrimSpace(commercestore.XunhuSecret) != "" &&
		strings.TrimSpace(commercestore.XunhuGateway) != ""
}

func IsXunhuWebhookEnabled() bool {
	return IsXunhuTopUpEnabled()
}

func CloneDisplayedPayMethods(methods []map[string]string, skipType string) []map[string]string {
	cloned := make([]map[string]string, 0, len(methods))
	for _, method := range methods {
		if len(method) == 0 {
			continue
		}
		if skipType != "" && strings.TrimSpace(method["type"]) == skipType {
			continue
		}
		next := make(map[string]string, len(method))
		for key, value := range method {
			next[key] = value
		}
		switch strings.TrimSpace(next["type"]) {
		case commerceschema.PaymentMethodXunhu, "wxpay":
			next["name"] = "微信支付"
		}
		cloned = append(cloned, next)
	}
	return cloned
}

func GetEpayClient() *epay.Client {
	if commercestore.PayAddress == "" || commercestore.EpayId == "" || commercestore.EpayKey == "" {
		return nil
	}
	withURL, err := epay.NewClient(&epay.Config{
		PartnerID: commercestore.EpayId,
		Key:       commercestore.EpayKey,
	}, commercestore.PayAddress)
	if err != nil {
		return nil
	}
	return withURL
}

func GetPayMoney(amount int64, group string) float64 {
	dAmount := decimal.NewFromInt(amount)
	if platformgeneral.GetQuotaDisplayType() == platformgeneral.QuotaDisplayTypeTokens {
		dAmount = dAmount.Div(decimal.NewFromFloat(platformruntime.QuotaPerUnit))
	}

	topupGroupRatio := commercedomain.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}

	discount := 1.0
	if ds, ok := commercestore.GetPaymentSetting().AmountDiscount[int(amount)]; ok && ds > 0 {
		discount = ds
	}

	return dAmount.
		Mul(decimal.NewFromFloat(commercestore.Price)).
		Mul(decimal.NewFromFloat(topupGroupRatio)).
		Mul(decimal.NewFromFloat(discount)).
		InexactFloat64()
}

func GetMinTopup() int64 {
	minTopup := commercestore.MinTopUp
	if platformgeneral.GetQuotaDisplayType() == platformgeneral.QuotaDisplayTypeTokens {
		minTopup = int(decimal.NewFromInt(int64(minTopup)).
			Mul(decimal.NewFromFloat(platformruntime.QuotaPerUnit)).
			IntPart())
	}
	return int64(minTopup)
}

func NormalizeTopupWalletType(walletType string) string {
	return commerceschema.WalletTypeClaude
}

func IsClaudeTopupWallet(walletType string) bool {
	return commercedomain.IsClaudeWalletType(walletType)
}

func GetTopupMinAmount(walletType string) int64 {
	if IsClaudeTopupWallet(walletType) {
		return 1
	}
	return GetMinTopup()
}

func GetTopupPayMoney(amount int64, group string, walletType string) float64 {
	if IsClaudeTopupWallet(walletType) {
		return float64(amount)
	}
	return GetPayMoney(amount, group)
}

func ApplyTopupBlindBoxDiscount(userID int, payMoney float64) float64 {
	if userID <= 0 || payMoney <= 0 {
		return payMoney
	}
	discountRate := GetUserBlindBoxTopupDiscountRate(userID)
	if discountRate <= 0 {
		return payMoney
	}
	return commercedomain.ApplyDiscountRateToMoney(payMoney, discountRate)
}

func NormalizeStoredTopupAmount(amount int64, walletType string) int64 {
	if IsClaudeTopupWallet(walletType) {
		return amount
	}
	if platformgeneral.GetQuotaDisplayType() != platformgeneral.QuotaDisplayTypeTokens {
		return amount
	}
	return decimal.NewFromInt(amount).
		Div(decimal.NewFromFloat(platformruntime.QuotaPerUnit)).
		IntPart()
}

func GetXunhuMinTopupAmount(group string) int64 {
	minTopup := GetMinTopup()
	minPayment := float64(commercestore.XunhuMinTopUp)
	if minPayment <= 0 {
		return minTopup
	}

	groupRatio := commercedomain.GetTopupGroupRatio(group)
	if groupRatio <= 0 {
		groupRatio = 1
	}

	effectiveUnitPrice := commercestore.Price * groupRatio
	if platformgeneral.GetQuotaDisplayType() == platformgeneral.QuotaDisplayTypeTokens {
		if platformruntime.QuotaPerUnit <= 0 {
			return minTopup
		}
		effectiveUnitPrice = effectiveUnitPrice / platformruntime.QuotaPerUnit
	}
	if effectiveUnitPrice <= 0 {
		return minTopup
	}

	required := int64(math.Ceil(minPayment / effectiveUnitPrice))
	if required < minTopup {
		required = minTopup
	}
	for attempts := 0; attempts < 1000; attempts++ {
		if GetPayMoney(required, group) >= minPayment {
			return required
		}
		required++
	}
	return required
}

var orderLocks sync.Map
var createLock sync.Mutex

type refCountedMutex struct {
	mu       sync.Mutex
	refCount int
}

func LockOrder(tradeNo string) {
	createLock.Lock()
	var rcm *refCountedMutex
	if value, ok := orderLocks.Load(tradeNo); ok {
		rcm = value.(*refCountedMutex)
	} else {
		rcm = &refCountedMutex{}
		orderLocks.Store(tradeNo, rcm)
	}
	rcm.refCount++
	createLock.Unlock()
	rcm.mu.Lock()
}

func UnlockOrder(tradeNo string) {
	value, ok := orderLocks.Load(tradeNo)
	if !ok {
		return
	}
	rcm := value.(*refCountedMutex)
	rcm.mu.Unlock()

	createLock.Lock()
	rcm.refCount--
	if rcm.refCount == 0 {
		orderLocks.Delete(tradeNo)
	}
	createLock.Unlock()
}

func BuildStripePayMethod() map[string]string {
	return map[string]string{
		"name":      "Stripe",
		"type":      commerceschema.PaymentMethodStripe,
		"color":     "rgba(var(--semi-purple-5), 1)",
		"min_topup": strconv.Itoa(commercestore.StripeMinTopUp),
	}
}

func BuildWaffoPayMethod() map[string]string {
	return map[string]string{
		"name":      "Waffo (Global Payment)",
		"type":      commerceschema.PaymentMethodWaffo,
		"color":     "rgba(var(--semi-blue-5), 1)",
		"min_topup": strconv.Itoa(commercestore.WaffoMinTopUp),
	}
}

func BuildWaffoPancakePayMethod() map[string]string {
	return map[string]string{
		"name":      "Waffo Pancake",
		"type":      commerceschema.PaymentMethodWaffoPancake,
		"color":     "rgba(var(--semi-orange-5), 1)",
		"min_topup": strconv.Itoa(commercestore.WaffoPancakeMinTopUp),
	}
}

func BuildXunhuPayMethod(minTopup int64) map[string]string {
	return map[string]string{
		"name":      "微信支付",
		"type":      commerceschema.PaymentMethodXunhu,
		"color":     "rgba(var(--semi-orange-5), 1)",
		"min_topup": strconv.FormatInt(minTopup, 10),
	}
}

func BuildPaymentReturnPath(suffix string) string {
	base := strings.TrimRight(platformconfig.ServerAddress, "/")
	return base + platformconfig.ThemeAwarePath(suffix)
}

func BuildXunhuHash(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for key, value := range params {
		if key == "hash" || strings.TrimSpace(value) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	for index, key := range keys {
		if index > 0 {
			builder.WriteByte('&')
		}
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(params[key])
	}
	builder.WriteString(secret)

	sum := md5.Sum([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}

func VerifyXunhuHash(params map[string]string) bool {
	expected := BuildXunhuHash(params, commercestore.XunhuSecret)
	return strings.EqualFold(expected, params["hash"])
}

func IsXunhuPaymentMethod(method string) bool {
	trimmed := strings.TrimSpace(method)
	return trimmed == commerceschema.PaymentMethodXunhu || trimmed == "wxpay"
}

func FormatXunhuCreatePaymentError(err error) string {
	if err == nil {
		return "failed to create payment"
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "failed to create payment"
	}
	message = strings.ReplaceAll(message, "\r", " ")
	message = strings.ReplaceAll(message, "\n", " ")
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > 180 {
		message = message[:180] + "..."
	}
	return fmt.Sprintf("failed to create payment: %s", message)
}

func CreateXunhuOrder(tradeNo string, title string, totalFee float64, notifyURL string, returnURL string) (*XunhuCreateOrderResponse, error) {
	if err := validateXunhuConfig(); err != nil {
		return nil, err
	}
	nonce := platformruntime.GetRandomString(32)
	requestTime := strconv.FormatInt(time.Now().Unix(), 10)
	payload := map[string]string{
		"appid":          strings.TrimSpace(commercestore.XunhuAppID),
		"trade_order_id": tradeNo,
		"title":          title,
		"total_fee":      strconv.FormatFloat(totalFee, 'f', 2, 64),
		"notify_url":     notifyURL,
		"return_url":     returnURL,
		"time":           requestTime,
		"version":        xunhuAPIVersion,
		"nonce_str":      nonce,
	}
	payload["hash"] = BuildXunhuHash(payload, commercestore.XunhuSecret)

	form := url.Values{}
	for key, value := range payload {
		form.Set(key, value)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodPost, strings.TrimSpace(commercestore.XunhuGateway), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("xunhu create order failed, status=%d body=%s", resp.StatusCode, string(body))
	}

	var result XunhuCreateOrderResponse
	if err := platformencoding.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("invalid xunhu response: %s", string(body))
	}
	responsePayload := make(map[string]interface{})
	if err := platformencoding.Unmarshal(body, &responsePayload); err != nil {
		return nil, fmt.Errorf("invalid xunhu response payload: %s", string(body))
	}
	if result.ErrCode != 0 {
		errMsg := strings.TrimSpace(result.ErrMsg)
		if errMsg == "" {
			errMsg = string(body)
		}
		return nil, fmt.Errorf("xunhu create order failed: %s", errMsg)
	}
	if result.Hash != "" {
		responseVerify := buildXunhuResponseVerifyMap(responsePayload)
		responseVerify["hash"] = result.Hash
		if !VerifyXunhuHash(responseVerify) {
			return nil, fmt.Errorf("xunhu response hash verification failed")
		}
	}
	if strings.TrimSpace(result.PayURL) == "" && strings.TrimSpace(result.QRCodeURL) == "" {
		return nil, fmt.Errorf("xunhu response missing payment url")
	}
	return &result, nil
}

func GetStripeMinTopup() int64 {
	minTopup := commercestore.StripeMinTopUp
	if platformgeneral.GetQuotaDisplayType() == platformgeneral.QuotaDisplayTypeTokens {
		minTopup = minTopup * int(platformruntime.QuotaPerUnit)
	}
	return int64(minTopup)
}

func GetStripePayMoney(amount float64, group string) float64 {
	originalAmount := amount
	if platformgeneral.GetQuotaDisplayType() == platformgeneral.QuotaDisplayTypeTokens {
		amount = amount / platformruntime.QuotaPerUnit
	}
	topupGroupRatio := commercedomain.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}
	discount := 1.0
	if ds, ok := commercestore.GetPaymentSetting().AmountDiscount[int(originalAmount)]; ok && ds > 0 {
		discount = ds
	}
	return amount * commercestore.StripeUnitPrice * topupGroupRatio * discount
}

func GetWaffoPayMoney(amount float64, group string) float64 {
	originalAmount := amount
	if platformgeneral.GetQuotaDisplayType() == platformgeneral.QuotaDisplayTypeTokens {
		amount = amount / platformruntime.QuotaPerUnit
	}
	topupGroupRatio := commercedomain.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}
	discount := 1.0
	if ds, ok := commercestore.GetPaymentSetting().AmountDiscount[int(originalAmount)]; ok && ds > 0 {
		discount = ds
	}
	return amount * commercestore.WaffoUnitPrice * topupGroupRatio * discount
}

func GetWaffoPancakePayMoney(amount int64, group string) float64 {
	dAmount := decimal.NewFromInt(amount)
	if platformgeneral.GetQuotaDisplayType() == platformgeneral.QuotaDisplayTypeTokens {
		dAmount = dAmount.Div(decimal.NewFromFloat(platformruntime.QuotaPerUnit))
	}

	topupGroupRatio := commercedomain.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}

	discount := 1.0
	if ds, ok := commercestore.GetPaymentSetting().AmountDiscount[int(amount)]; ok && ds > 0 {
		discount = ds
	}

	return dAmount.
		Mul(decimal.NewFromFloat(commercestore.WaffoPancakeUnitPrice)).
		Mul(decimal.NewFromFloat(topupGroupRatio)).
		Mul(decimal.NewFromFloat(discount)).
		InexactFloat64()
}

type XunhuCreateOrderResponse struct {
	ErrCode   int    `json:"errcode"`
	ErrMsg    string `json:"errmsg"`
	TradeNo   string `json:"trade_order_id"`
	PayURL    string `json:"url"`
	QRCodeURL string `json:"url_qrcode"`
	Hash      string `json:"hash"`
}

const xunhuAPIVersion = "1.1"

func stringifyXunhuValue(value interface{}) (string, bool) {
	switch v := value.(type) {
	case nil:
		return "", false
	case string:
		return v, true
	case json.Number:
		return v.String(), true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), true
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int16:
		return strconv.FormatInt(int64(v), 10), true
	case int8:
		return strconv.FormatInt(int64(v), 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint16:
		return strconv.FormatUint(uint64(v), 10), true
	case uint8:
		return strconv.FormatUint(uint64(v), 10), true
	case bool:
		return strconv.FormatBool(v), true
	default:
		return "", false
	}
}

func buildXunhuResponseVerifyMap(payload map[string]interface{}) map[string]string {
	verifyMap := make(map[string]string, len(payload))
	for key, value := range payload {
		stringValue, ok := stringifyXunhuValue(value)
		if !ok {
			continue
		}
		verifyMap[key] = stringValue
	}
	return verifyMap
}

func validateXunhuConfig() error {
	switch {
	case strings.TrimSpace(commercestore.XunhuAppID) == "":
		return fmt.Errorf("xunhu app id is empty")
	case strings.TrimSpace(commercestore.XunhuSecret) == "":
		return fmt.Errorf("xunhu secret is empty")
	case strings.TrimSpace(commercestore.XunhuGateway) == "":
		return fmt.Errorf("xunhu gateway is empty")
	default:
		return nil
	}
}

func isWaffoWebhookConfigured() bool {
	if commercestore.WaffoSandbox {
		return strings.TrimSpace(commercestore.WaffoSandboxApiKey) != "" &&
			strings.TrimSpace(commercestore.WaffoSandboxPrivateKey) != "" &&
			strings.TrimSpace(commercestore.WaffoSandboxPublicCert) != ""
	}
	return strings.TrimSpace(commercestore.WaffoApiKey) != "" &&
		strings.TrimSpace(commercestore.WaffoPrivateKey) != "" &&
		strings.TrimSpace(commercestore.WaffoPublicCert) != ""
}

func isWaffoPancakeWebhookConfigured() bool {
	currentWebhookKey := strings.TrimSpace(commercestore.WaffoPancakeWebhookPublicKey)
	if commercestore.WaffoPancakeSandbox {
		currentWebhookKey = strings.TrimSpace(commercestore.WaffoPancakeWebhookTestKey)
	}
	return currentWebhookKey != ""
}

func isEpayWebhookConfigured() bool {
	return strings.TrimSpace(commercestore.PayAddress) != "" &&
		strings.TrimSpace(commercestore.EpayId) != "" &&
		strings.TrimSpace(commercestore.EpayKey) != ""
}
