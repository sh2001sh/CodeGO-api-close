package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/sh2001sh/new-api/constant"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformtext "github.com/sh2001sh/new-api/internal/platform/textx"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type EpayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
	WalletType    string `json:"wallet_type,omitempty"`
}

type AmountRequest struct {
	Amount     int64  `json:"amount"`
	WalletType string `json:"wallet_type,omitempty"`
}

type AdminCompleteTopupRequest struct {
	TradeNo string `json:"trade_no"`
}

type EpayCheckoutResponse struct {
	URL  string
	Data any
}

func BuildTopUpInfo(userID int) map[string]any {
	complianceConfirmed := commercestore.IsPaymentComplianceConfirmed()
	payMethods := CloneDisplayedPayMethods(commercestore.PayMethods, "")
	if !complianceConfirmed {
		payMethods = []map[string]string{}
	}

	if IsStripeTopUpEnabled() && !containsPayMethod(payMethods, commerceschema.PaymentMethodStripe) {
		payMethods = append(payMethods, BuildStripePayMethod())
	}
	enableWaffo := IsWaffoTopUpEnabled()
	if enableWaffo && !containsPayMethod(payMethods, commerceschema.PaymentMethodWaffo) {
		payMethods = append(payMethods, BuildWaffoPayMethod())
	}
	enableWaffoPancake := IsWaffoPancakeTopUpEnabled()
	if enableWaffoPancake && !containsPayMethod(payMethods, commerceschema.PaymentMethodWaffoPancake) {
		payMethods = append(payMethods, BuildWaffoPancakePayMethod())
	}

	enableXunhu := IsXunhuTopUpEnabled()
	if enableXunhu {
		payMethods = CloneDisplayedPayMethods(payMethods, "wxpay")
		userGroup := ""
		if userID > 0 {
			if group, err := loadCommerceUserGroup(userID, true); err == nil {
				userGroup = group
			}
		}
		if !containsPayMethod(payMethods, commerceschema.PaymentMethodXunhu) {
			payMethods = append(payMethods, BuildXunhuPayMethod(GetXunhuMinTopupAmount(userGroup)))
		}
	}

	return map[string]any{
		"enable_online_topup":              IsEpayTopUpEnabled() || enableXunhu,
		"enable_stripe_topup":              IsStripeTopUpEnabled(),
		"enable_creem_topup":               IsCreemTopUpEnabled(),
		"enable_waffo_topup":               enableWaffo,
		"enable_waffo_pancake_topup":       enableWaffoPancake,
		"enable_redemption":                complianceConfirmed,
		"payment_compliance_confirmed":     complianceConfirmed,
		"payment_compliance_terms_version": commercestore.CurrentComplianceTermsVersion,
		"waffo_pay_methods": func() any {
			if enableWaffo {
				return commercestore.GetWaffoPayMethods()
			}
			return nil
		}(),
		"creem_products":          commercestore.CreemProducts,
		"pay_methods":             payMethods,
		"min_topup":               commercestore.MinTopUp,
		"stripe_min_topup":        commercestore.StripeMinTopUp,
		"waffo_min_topup":         commercestore.WaffoMinTopUp,
		"waffo_pancake_min_topup": commercestore.WaffoPancakeMinTopUp,
		"amount_options":          commercestore.GetPaymentSetting().AmountOptions,
		"discount":                commercestore.GetPaymentSetting().AmountDiscount,
		"topup_link":              platformconfig.TopUpLink,
	}
}

func QuoteTopUpAmount(userID int, req AmountRequest) (string, error) {
	walletType := NormalizeTopupWalletType(req.WalletType)
	minTopup := GetTopupMinAmount(walletType)
	if req.Amount < minTopup {
		return "", fmt.Errorf("充值数量不能小于 %d", GetMinTopup())
	}

	group, err := loadCommerceUserGroup(userID, true)
	if err != nil {
		return "", errors.New("获取用户分组失败")
	}

	payMoney := GetTopupPayMoney(req.Amount, group, walletType)
	payMoney = ApplyTopupBlindBoxDiscount(userID, payMoney)
	if payMoney <= 0.01 {
		return "", errors.New("充值金额过低")
	}
	return strconv.FormatFloat(payMoney, 'f', 2, 64), nil
}

func CreateEpayTopUp(userID int, req EpayRequest) (*EpayCheckoutResponse, error) {
	walletType := NormalizeTopupWalletType(req.WalletType)
	minTopup := GetTopupMinAmount(walletType)
	if req.Amount < minTopup {
		return nil, fmt.Errorf("充值数量不能小于 %d", GetMinTopup())
	}

	group, err := loadCommerceUserGroup(userID, true)
	if err != nil {
		return nil, errors.New("获取用户分组失败")
	}

	payMoney := GetTopupPayMoney(req.Amount, group, walletType)
	if payMoney < 0.01 {
		return nil, errors.New("充值金额过低")
	}
	if !commercestore.ContainsPayMethod(req.PaymentMethod) {
		return nil, errors.New("支付方式不存在")
	}

	callBackAddress := CallbackAddress()
	returnURL, _ := url.Parse(buildPaymentReturnPath("/console/log"))
	notifyURL, _ := url.Parse(callBackAddress + "/api/user/epay/notify")
	tradeNo := fmt.Sprintf("USR%dNO%s%d", userID, platformruntime.GetRandomString(6), time.Now().Unix())
	client := GetEpayClient()
	if client == nil {
		return nil, errors.New("当前管理员未配置支付信息")
	}

	topUp := &commerceschema.TopUp{
		UserId:          userID,
		Amount:          NormalizeStoredTopupAmount(req.Amount, walletType),
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   req.PaymentMethod,
		PaymentProvider: commerceschema.PaymentProviderEpay,
		WalletType:      walletType,
		CreateTime:      time.Now().Unix(),
		Status:          constant.TopUpStatusPending,
	}
	if _, err := CreatePendingTopUpOrderWithBlindBoxDiscount(topUp); err != nil {
		logger.LogError(context.Background(), fmt.Sprintf("易支付 创建充值订单失败 user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", userID, tradeNo, req.PaymentMethod, req.Amount, err.Error()))
		return nil, errors.New("创建订单失败")
	}

	uri, params, err := client.Purchase(&epay.PurchaseArgs{
		Type:           req.PaymentMethod,
		ServiceTradeNo: tradeNo,
		Name:           fmt.Sprintf("TUC%d", req.Amount),
		Money:          strconv.FormatFloat(topUp.Money, 'f', 2, 64),
		Device:         epay.PC,
		NotifyUrl:      notifyURL,
		ReturnUrl:      returnURL,
	})
	if err != nil {
		_ = UpdatePendingTopUpStatus(tradeNo, commerceschema.PaymentProviderEpay, constant.TopUpStatusFailed)
		logger.LogError(context.Background(), fmt.Sprintf("易支付 拉起支付失败 user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", userID, tradeNo, req.PaymentMethod, req.Amount, err.Error()))
		return nil, errors.New("拉起支付失败")
	}

	logger.LogInfo(context.Background(), fmt.Sprintf("易支付 充值订单创建成功 user_id=%d trade_no=%s payment_method=%s amount=%d money=%.2f uri=%q params=%q", userID, tradeNo, req.PaymentMethod, req.Amount, topUp.Money, uri, platformtext.GetJsonString(params)))
	return &EpayCheckoutResponse{URL: uri, Data: params}, nil
}

func ListUserTopUps(userID int, keyword string, pageInfo *platformpagination.PageInfo) (*platformpagination.PageInfo, error) {
	var (
		topups []*commerceschema.TopUp
		total  int64
		err    error
	)
	if keyword != "" {
		topups, total, err = SearchUserTopUps(userID, keyword, pageInfo)
	} else {
		topups, total, err = GetUserTopUps(userID, pageInfo)
	}
	if err != nil {
		return nil, err
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(topups)
	return pageInfo, nil
}

func ListAllTopUps(keyword string, pageInfo *platformpagination.PageInfo) (*platformpagination.PageInfo, error) {
	var (
		topups []*commerceschema.TopUp
		total  int64
		err    error
	)
	if keyword != "" {
		topups, total, err = SearchAllTopUps(keyword, pageInfo)
	} else {
		topups, total, err = GetAllTopUps(pageInfo)
	}
	if err != nil {
		return nil, err
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(topups)
	return pageInfo, nil
}

func CompleteTopUpByAdmin(tradeNo string, callerIP string) error {
	if tradeNo == "" {
		return errors.New("参数错误")
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	return ManualCompleteTopUp(tradeNo, callerIP)
}

func HandleEpayWebhook(params map[string]string, clientIP string) (bool, error) {
	if !IsEpayWebhookEnabled() {
		logger.LogWarn(context.Background(), fmt.Sprintf("易支付 webhook 被拒绝 reason=webhook_disabled client_ip=%s", clientIP))
		return false, nil
	}
	if len(params) == 0 {
		return false, nil
	}

	client := GetEpayClient()
	if client == nil {
		return false, nil
	}
	verifyInfo, err := client.Verify(params)
	if err != nil || !verifyInfo.VerifyStatus {
		return false, err
	}
	if verifyInfo.TradeStatus != epay.StatusTradeSuccess {
		return true, nil
	}

	LockOrder(verifyInfo.ServiceTradeNo)
	defer UnlockOrder(verifyInfo.ServiceTradeNo)

	topUp := GetTopUpByTradeNo(verifyInfo.ServiceTradeNo)
	if topUp == nil || topUp.PaymentProvider != commerceschema.PaymentProviderEpay {
		return false, nil
	}
	if topUp.Status != constant.TopUpStatusPending {
		return true, nil
	}

	completedTopUp, creditedQuota, completeErr := CompleteTopUpByTradeNo(
		verifyInfo.ServiceTradeNo,
		commerceschema.PaymentProviderEpay,
		verifyInfo.Type,
		"",
		"",
	)
	if completeErr != nil {
		logger.LogError(context.Background(), fmt.Sprintf("epay complete topup failed trade_no=%s user_id=%d client_ip=%s error=%q topup=%q", topUp.TradeNo, topUp.UserId, clientIP, completeErr.Error(), platformtext.GetJsonString(topUp)))
		return false, completeErr
	}

	logger.LogInfo(context.Background(), fmt.Sprintf("epay topup success trade_no=%s user_id=%d wallet_type=%s client_ip=%s quota_to_add=%d money=%.2f", completedTopUp.TradeNo, completedTopUp.UserId, completedTopUp.WalletType, clientIP, creditedQuota, completedTopUp.Money))
	auditapp.RecordTopupLog(completedTopUp.UserId, fmt.Sprintf("epay topup success, wallet: %s, quota: %v, paid: %.2f", completedTopUp.NormalizedWalletType(), logger.LogQuota(creditedQuota), completedTopUp.Money), clientIP, completedTopUp.PaymentMethod, "epay")
	return true, nil
}

func CollectEpayParams(r *http.Request) (map[string]string, error) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		params := make(map[string]string, len(r.PostForm))
		for key := range r.PostForm {
			params[key] = r.PostForm.Get(key)
		}
		return params, nil
	}

	params := make(map[string]string, len(r.URL.Query()))
	for key := range r.URL.Query() {
		params[key] = r.URL.Query().Get(key)
	}
	return params, nil
}

func buildPaymentReturnPath(suffix string) string {
	base := strings.TrimRight(platformconfig.ServerAddress, "/")
	return base + platformconfig.ThemeAwarePath(suffix)
}

func containsPayMethod(methods []map[string]string, methodType string) bool {
	for _, method := range methods {
		if method["type"] == methodType {
			return true
		}
	}
	return false
}
