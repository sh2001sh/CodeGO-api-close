package store

import (
	toolpricing "github.com/sh2001sh/new-api/internal/billing/toolpricing"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	luckysettings "github.com/sh2001sh/new-api/internal/commerce/luckysettings"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	gatewaygroups "github.com/sh2001sh/new-api/internal/gateway/groupsettings"
	clientlinks "github.com/sh2001sh/new-api/internal/platform/clientlinks"
	platformops "github.com/sh2001sh/new-api/internal/platform/opssettings"
	requestsettings "github.com/sh2001sh/new-api/internal/platform/requestsettings"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"strconv"
	"strings"

	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	"github.com/sh2001sh/new-api/setting/config"
)

func applyOptionValue(key string, value string) (err error) {
	platformconfig.OptionMapRWMutex.Lock()
	defer platformconfig.OptionMapRWMutex.Unlock()
	if platformconfig.OptionMap == nil {
		platformconfig.OptionMap = make(map[string]string)
	}
	platformconfig.OptionMap[key] = value

	if handled, configErr := handleConfigUpdate(key, value); handled {
		return configErr
	}

	if strings.HasSuffix(key, "Permission") {
		intValue, _ := strconv.Atoi(value)
		switch key {
		case "FileUploadPermission":
			platformconfig.FileUploadPermission = intValue
		case "FileDownloadPermission":
			platformconfig.FileDownloadPermission = intValue
		case "ImageUploadPermission":
			platformconfig.ImageUploadPermission = intValue
		case "ImageDownloadPermission":
			platformconfig.ImageDownloadPermission = intValue
		}
	}

	if strings.HasSuffix(key, "Enabled") || key == "DefaultCollapseSidebar" || key == "DefaultUseAutoGroup" || key == "SMTPForceAuthLogin" {
		boolValue := value == "true"
		switch key {
		case "PasswordRegisterEnabled":
			platformconfig.PasswordRegisterEnabled = boolValue
		case "PasswordLoginEnabled":
			platformconfig.PasswordLoginEnabled = boolValue
		case "EmailVerificationEnabled":
			platformconfig.EmailVerificationEnabled = boolValue
		case "GitHubOAuthEnabled":
			platformconfig.GitHubOAuthEnabled = boolValue
		case "LinuxDOOAuthEnabled":
			platformconfig.LinuxDOOAuthEnabled = boolValue
		case "WeChatAuthEnabled":
			platformconfig.WeChatAuthEnabled = boolValue
		case "TelegramOAuthEnabled":
			platformconfig.TelegramOAuthEnabled = boolValue
		case "TurnstileCheckEnabled":
			platformconfig.TurnstileCheckEnabled = boolValue
		case "RegisterEnabled":
			platformconfig.RegisterEnabled = boolValue
		case "EmailDomainRestrictionEnabled":
			platformconfig.EmailDomainRestrictionEnabled = boolValue
		case "EmailAliasRestrictionEnabled":
			platformconfig.EmailAliasRestrictionEnabled = boolValue
		case "AutomaticDisableChannelEnabled":
			platformconfig.AutomaticDisableChannelEnabled = boolValue
		case "AutomaticEnableChannelEnabled":
			platformconfig.AutomaticEnableChannelEnabled = boolValue
		case "LogConsumeEnabled":
			platformconfig.LogConsumeEnabled = boolValue
		case "DisplayInCurrencyEnabled":
			quotaDisplayType := "USD"
			if !boolValue {
				quotaDisplayType = "TOKENS"
			}
			if cfg := config.GlobalConfig.Get("general_setting"); cfg != nil {
				_ = config.UpdateConfigFromMap(cfg, map[string]string{"quota_display_type": quotaDisplayType})
			}
		case "DisplayTokenStatEnabled":
			platformconfig.DisplayTokenStatEnabled = boolValue
		case "DrawingEnabled":
			platformconfig.DrawingEnabled = boolValue
		case "TaskEnabled":
			platformconfig.TaskEnabled = boolValue
		case "DataExportEnabled":
			platformconfig.DataExportEnabled = boolValue
		case "DefaultCollapseSidebar":
			platformconfig.DefaultCollapseSidebar = boolValue
		case "CheckSensitiveEnabled":
			requestsettings.CheckSensitiveEnabled = boolValue
		case "DemoSiteEnabled":
			platformops.SetDemoSiteEnabled(boolValue)
		case "SelfUseModeEnabled":
			platformops.SetSelfUseModeEnabled(boolValue)
		case "CheckSensitiveOnPromptEnabled":
			requestsettings.CheckSensitiveOnPromptEnabled = boolValue
		case "ModelRequestRateLimitEnabled":
			requestsettings.ModelRequestRateLimitEnabled = boolValue
		case "StopOnSensitiveEnabled":
			requestsettings.StopOnSensitiveEnabled = boolValue
		case "SMTPSSLEnabled":
			platformconfig.SMTPSSLEnabled = boolValue
		case "SMTPForceAuthLogin":
			platformconfig.SMTPForceAuthLogin = boolValue
		case "WorkerAllowHttpImageRequestEnabled":
			platformconfig.WorkerAllowHttpImageRequestEnabled = boolValue
		case "DefaultUseAutoGroup":
			gatewaygroups.DefaultUseAutoGroup = boolValue
		case "ExposeRatioEnabled":
			gatewaystore.SetExposeRatioEnabled(boolValue)
		}
	}

	switch key {
	case "EmailDomainWhitelist":
		platformconfig.EmailDomainWhitelist = strings.Split(value, ",")
	case "SMTPServer":
		platformconfig.SMTPServer = value
	case "SMTPPort":
		platformconfig.SMTPPort, _ = strconv.Atoi(value)
	case "SMTPAccount":
		platformconfig.SMTPAccount = value
	case "SMTPFrom":
		platformconfig.SMTPFrom = value
	case "SMTPToken":
		platformconfig.SMTPToken = value
	case "ServerAddress":
		platformconfig.ServerAddress = value
	case "WorkerUrl":
		platformconfig.WorkerUrl = value
	case "WorkerValidKey":
		platformconfig.WorkerValidKey = value
	case "PayAddress":
		commercestore.PayAddress = value
	case "Chats":
		err = clientlinks.UpdateChatsByJsonString(value)
	case "AutoGroups":
		err = gatewaygroups.UpdateAutoGroupsByJsonString(value)
	case "CustomCallbackAddress":
		commercestore.CustomCallbackAddress = value
	case "EpayId":
		commercestore.EpayId = value
	case "EpayKey":
		commercestore.EpayKey = value
	case "Price":
		commercestore.Price, _ = strconv.ParseFloat(value, 64)
	case "USDExchangeRate":
		commercestore.USDExchangeRate, _ = strconv.ParseFloat(value, 64)
	case "MinTopUp":
		commercestore.MinTopUp, _ = strconv.Atoi(value)
	case "StripeApiSecret":
		commercestore.StripeApiSecret = value
	case "StripeWebhookSecret":
		commercestore.StripeWebhookSecret = value
	case "StripePriceId":
		commercestore.StripePriceId = value
	case "StripeUnitPrice":
		commercestore.StripeUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "StripeMinTopUp":
		commercestore.StripeMinTopUp, _ = strconv.Atoi(value)
	case "StripePromotionCodesEnabled":
		commercestore.StripePromotionCodesEnabled = value == "true"
	case "CreemApiKey":
		commercestore.CreemApiKey = value
	case "CreemProducts":
		commercestore.CreemProducts = value
	case "CreemTestMode":
		commercestore.CreemTestMode = value == "true"
	case "CreemWebhookSecret":
		commercestore.CreemWebhookSecret = value
	case "XunhuEnabled":
		commercestore.XunhuEnabled = value == "true"
	case "XunhuAppID":
		commercestore.XunhuAppID = value
	case "XunhuSecret":
		commercestore.XunhuSecret = value
	case "XunhuGateway":
		commercestore.XunhuGateway = value
	case "XunhuMinTopUp":
		commercestore.XunhuMinTopUp, _ = strconv.Atoi(value)
	case "WaffoEnabled":
		commercestore.WaffoEnabled = value == "true"
	case "WaffoApiKey":
		commercestore.WaffoApiKey = value
	case "WaffoPrivateKey":
		commercestore.WaffoPrivateKey = value
	case "WaffoPublicCert":
		commercestore.WaffoPublicCert = value
	case "WaffoSandboxPublicCert":
		commercestore.WaffoSandboxPublicCert = value
	case "WaffoSandboxApiKey":
		commercestore.WaffoSandboxApiKey = value
	case "WaffoSandboxPrivateKey":
		commercestore.WaffoSandboxPrivateKey = value
	case "WaffoSandbox":
		commercestore.WaffoSandbox = value == "true"
	case "WaffoMerchantId":
		commercestore.WaffoMerchantId = value
	case "WaffoNotifyUrl":
		commercestore.WaffoNotifyUrl = value
	case "WaffoReturnUrl":
		commercestore.WaffoReturnUrl = value
	case "WaffoSubscriptionReturnUrl":
		commercestore.WaffoSubscriptionReturnUrl = value
	case "WaffoCurrency":
		commercestore.WaffoCurrency = value
	case "WaffoUnitPrice":
		commercestore.WaffoUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "WaffoMinTopUp":
		commercestore.WaffoMinTopUp, _ = strconv.Atoi(value)
	case "WaffoPancakeEnabled":
		commercestore.WaffoPancakeEnabled = value == "true"
	case "WaffoPancakeSandbox":
		commercestore.WaffoPancakeSandbox = value == "true"
	case "WaffoPancakeMerchantID":
		commercestore.WaffoPancakeMerchantID = value
	case "WaffoPancakePrivateKey":
		commercestore.WaffoPancakePrivateKey = value
	case "WaffoPancakeWebhookPublicKey":
		commercestore.WaffoPancakeWebhookPublicKey = value
	case "WaffoPancakeWebhookTestKey":
		commercestore.WaffoPancakeWebhookTestKey = value
	case "WaffoPancakeStoreID":
		commercestore.WaffoPancakeStoreID = value
	case "WaffoPancakeProductID":
		commercestore.WaffoPancakeProductID = value
	case "WaffoPancakeReturnURL":
		commercestore.WaffoPancakeReturnURL = value
	case "WaffoPancakeCurrency":
		commercestore.WaffoPancakeCurrency = value
	case "WaffoPancakeUnitPrice":
		commercestore.WaffoPancakeUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "WaffoPancakeMinTopUp":
		commercestore.WaffoPancakeMinTopUp, _ = strconv.Atoi(value)
	case "TopupGroupRatio":
		err = commercedomain.UpdateTopupGroupRatio(value)
	case "GitHubClientId":
		platformconfig.GitHubClientId = value
	case "GitHubClientSecret":
		platformconfig.GitHubClientSecret = value
	case "LinuxDOClientId":
		platformconfig.LinuxDOClientId = value
	case "LinuxDOClientSecret":
		platformconfig.LinuxDOClientSecret = value
	case "LinuxDOMinimumTrustLevel":
		platformconfig.LinuxDOMinimumTrustLevel, _ = strconv.Atoi(value)
	case "Footer":
		platformconfig.Footer = value
	case "SystemName":
		platformconfig.SystemName = value
	case "Logo":
		platformconfig.Logo = value
	case "WeChatServerAddress":
		platformconfig.WeChatServerAddress = value
	case "WeChatServerToken":
		platformconfig.WeChatServerToken = value
	case "WeChatAccountQRCodeImageURL":
		platformconfig.WeChatAccountQRCodeImageURL = value
	case "TelegramBotToken":
		platformconfig.TelegramBotToken = value
	case "TelegramBotName":
		platformconfig.TelegramBotName = value
	case "TurnstileSiteKey":
		platformconfig.TurnstileSiteKey = value
	case "TurnstileSecretKey":
		platformconfig.TurnstileSecretKey = value
	case "QuotaForNewUser":
		platformconfig.QuotaForNewUser, _ = strconv.Atoi(value)
	case "QuotaForInviter":
		platformconfig.QuotaForInviter, _ = strconv.Atoi(value)
	case "QuotaForInvitee":
		platformconfig.QuotaForInvitee, _ = strconv.Atoi(value)
	case "QuotaRemindThreshold":
		platformconfig.QuotaRemindThreshold, _ = strconv.Atoi(value)
	case "PreConsumedQuota":
		platformconfig.PreConsumedQuota, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitCount":
		requestsettings.ModelRequestRateLimitCount, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitDurationMinutes":
		requestsettings.ModelRequestRateLimitDurationMinutes, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitSuccessCount":
		requestsettings.ModelRequestRateLimitSuccessCount, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitGroup":
		err = requestsettings.UpdateModelRequestRateLimitGroupByJSONString(value)
	case "RetryTimes":
		platformconfig.RetryTimes, _ = strconv.Atoi(value)
	case "DataExportInterval":
		platformconfig.DataExportInterval, _ = strconv.Atoi(value)
	case "DataExportDefaultTime":
		platformconfig.DataExportDefaultTime = value
	case "ModelRatio":
		err = gatewaystore.UpdateModelRatioByJSONString(value)
	case "GroupRatio":
		err = gatewaystore.UpdateGroupRatioByJSONString(value)
	case "GroupGroupRatio":
		err = gatewaystore.UpdateGroupGroupRatioByJSONString(value)
	case gatewaystore.SubscriptionGroupPolicyOptionKey:
		err = gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(value)
	case commerceschema.SubscriptionClaudeConversionEnabledOptionKey:
		commerceschema.SubscriptionClaudeConversionEnabled = value == "true"
	case "UserUsableGroups":
		err = gatewaygroups.UpdateUserUsableGroupsByJSONString(value)
	case "CompletionRatio":
		err = gatewaystore.UpdateCompletionRatioByJSONString(value)
	case "ModelPrice":
		err = gatewaystore.UpdateModelPriceByJSONString(value)
	case "CacheRatio":
		err = gatewaystore.UpdateCacheRatioByJSONString(value)
	case "CreateCacheRatio":
		err = gatewaystore.UpdateCreateCacheRatioByJSONString(value)
	case "ImageRatio":
		err = gatewaystore.UpdateImageRatioByJSONString(value)
	case "AudioRatio":
		err = gatewaystore.UpdateAudioRatioByJSONString(value)
	case "AudioCompletionRatio":
		err = gatewaystore.UpdateAudioCompletionRatioByJSONString(value)
	case "TopUpLink":
		platformconfig.TopUpLink = value
	case "ChannelDisableThreshold":
		platformconfig.ChannelDisableThreshold, _ = strconv.ParseFloat(value, 64)
	case "QuotaPerUnit":
		platformruntime.QuotaPerUnit, _ = strconv.ParseFloat(value, 64)
	case "SensitiveWords":
		requestsettings.SensitiveWordsFromString(value)
	case "PromptAuditReviewRules":
		requestsettings.PromptAuditReviewRulesFromString(value)
	case "AutomaticDisableKeywords":
		platformops.SetAutomaticDisableKeywordsFromString(value)
	case "AutomaticDisableStatusCodes":
		err = gatewaystore.AutomaticDisableStatusCodesFromString(value)
	case "AutomaticRetryStatusCodes":
		err = gatewaystore.AutomaticRetryStatusCodesFromString(value)
	case "StreamCacheQueueLength":
		requestsettings.StreamCacheQueueLength, _ = strconv.Atoi(value)
	case "PayMethods":
		err = commercestore.UpdatePayMethodsByJsonString(value)
	}
	return err
}

func handleConfigUpdate(key, value string) (bool, error) {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return false, nil
	}

	configName := parts[0]
	configKey := parts[1]
	cfg := config.GlobalConfig.Get(configName)
	if cfg == nil {
		return false, nil
	}

	if configName == "daily_lucky_number_setting" {
		return true, luckysettings.ApplyField(configKey, value)
	}
	if err := config.UpdateConfigFromMap(cfg, map[string]string{configKey: value}); err != nil {
		return true, err
	}

	switch configName {
	case "performance_setting":
		updatePerformanceSettingAndSync()
	case "tool_price_setting":
		toolpricing.RebuildIndex()
	case "billing_setting":
		gatewaystore.RestoreMissingDefaultBillingRules()
		gatewaystore.InvalidatePricingCache()
		gatewaystore.InvalidateExposedDataCache()
	case "theme":
		UpdateAndSyncTheme()
	}

	return true, nil
}
