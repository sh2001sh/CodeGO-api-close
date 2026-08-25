package runtime

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/internal/billing/domain/billingexpr"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformmath "github.com/sh2001sh/new-api/internal/platform/mathx"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
)

func modelPriceNotConfiguredError(modelName string, userID int) error {
	if identitystore.IsUserAdmin(userID) {
		return fmt.Errorf(
			"模型 %s 的价格未配置。请前往「系统设置 → 运营设置」开启自用模式，或在「系统设置 → 分组与模型定价设置」中为该模型配置价格；"+
				"Model %s price not configured. Go to System Settings → Operation Settings to enable self-use mode, or configure the model price in System Settings → Group & Model Pricing.",
			modelName, modelName,
		)
	}
	return fmt.Errorf(
		"模型 %s 的价格尚未由管理员配置，暂时无法使用，请联系站点管理员开启该模型；"+
			"Model %s has not been priced by the administrator yet. Please contact the site administrator to enable this model.",
		modelName, modelName,
	)
}

const claudeCacheCreation1hMultiplier = 6 / 3.75

func HandleGroupRatio(ctx *gin.Context, relayInfo *RelayInfo) types.GroupRatioInfo {
	groupRatioInfo := types.GroupRatioInfo{
		GroupRatio:        1.0,
		GroupSpecialRatio: -1,
	}
	if httpctx.GetContextKeyBool(ctx, constant.ContextKeyZeroHourActive) {
		groupRatioInfo.GroupRatio = 0
		groupRatioInfo.GroupSpecialRatio = 0
		groupRatioInfo.HasSpecialRatio = true
		return groupRatioInfo
	}
	autoGroup, exists := ctx.Get("auto_group")
	if exists {
		logger.LogDebug(ctx, fmt.Sprintf("final group: %s", autoGroup))
		relayInfo.UsingGroup = autoGroup.(string)
	}
	if httpctx.GetContextKeyString(ctx, constant.ContextKeyMarketplaceGroupID) != "" {
		// Marketplace groups are dynamic and intentionally do not populate the
		// global GroupRatio map. Their persisted multiplier is bound to this
		// request during token or auto-group resolution.
		groupRatioInfo.GroupRatio = httpctx.GetContextKeyFloat64(ctx, constant.ContextKeyMarketplaceMultiplier)
		return groupRatioInfo
	}

	userGroupRatio, ok := gatewaystore.GetGroupGroupRatio(relayInfo.UserGroup, relayInfo.UsingGroup)
	if ok {
		groupRatioInfo.GroupSpecialRatio = userGroupRatio
		groupRatioInfo.GroupRatio = userGroupRatio
		groupRatioInfo.HasSpecialRatio = true
	} else {
		groupRatioInfo.GroupRatio = gatewaystore.GetGroupRatio(relayInfo.UsingGroup)
	}

	return groupRatioInfo
}

func ModelPriceHelper(c *gin.Context, info *RelayInfo, promptTokens int, meta *types.TokenCountMeta) (types.PriceData, error) {
	modelPrice, usePrice := gatewaystore.GetModelPrice(info.OriginModelName, false)
	groupRatioInfo := HandleGroupRatio(c, info)
	marketplaceImagePrice, marketplaceImage, err := requiredMarketplaceImagePrice(c, info.OriginModelName)
	if err != nil {
		return types.PriceData{}, err
	}
	if marketplaceImage {
		modelPrice = marketplaceImagePrice
		usePrice = true
	}

	if !marketplaceImage && gatewaystore.GetBillingMode(info.OriginModelName) == gatewaystore.BillingModeTieredExpr {
		return modelPriceHelperTiered(c, info, promptTokens, meta, groupRatioInfo)
	}

	var preConsumedQuota int
	var modelRatio float64
	var completionRatio float64
	var cacheRatio float64
	var imageRatio float64
	var cacheCreationRatio float64
	var cacheCreationRatio5m float64
	var cacheCreationRatio1h float64
	var audioRatio float64
	var audioCompletionRatio float64
	var freeModel bool
	if !usePrice {
		var success bool
		var matchName string
		var marketplaceTokenPrice *marketplacedomain.ChannelModelPrice
		modelRatio, success, matchName = gatewaystore.GetModelRatio(info.OriginModelName)
		if !success {
			if channelPrice, ok := marketplaceChannelModelPrice(c, info.OriginModelName); ok {
				if channelPrice.EffectiveBillingMode() == marketplacedomain.ChannelBillingModePerCall {
					modelPrice = channelPrice.PricePerCall
					usePrice = true
				} else {
					modelRatio = channelPrice.InputPricePerMillion / 2
					completionRatio = channelPrice.OutputPricePerMillion / channelPrice.InputPricePerMillion
					marketplaceTokenPrice = &channelPrice
					success = true
				}
			}
		}
		if !success && !usePrice {
			acceptUnsetRatio := false
			if info.UserSetting.AcceptUnsetRatioModel {
				acceptUnsetRatio = true
			}
			if !acceptUnsetRatio {
				return types.PriceData{}, modelPriceNotConfiguredError(matchName, info.UserId)
			}
		}
		if !usePrice {
			preConsumedTokens := platformmath.MaxInt(promptTokens, platformconfig.PreConsumedQuota)
			if meta.MaxTokens != 0 {
				preConsumedTokens += meta.MaxTokens
			}
			if completionRatio == 0 {
				completionRatio = gatewaystore.GetCompletionRatio(info.OriginModelName)
			}
			cacheRatio, _ = gatewaystore.GetCacheRatio(info.OriginModelName)
			cacheCreationRatio, _ = gatewaystore.GetCreateCacheRatio(info.OriginModelName)
			if marketplaceTokenPrice != nil {
				if marketplaceTokenPrice.CacheReadPricePerMillion != nil {
					cacheRatio = *marketplaceTokenPrice.CacheReadPricePerMillion / marketplaceTokenPrice.InputPricePerMillion
				}
				if marketplaceTokenPrice.CacheWritePricePerMillion != nil {
					cacheCreationRatio = *marketplaceTokenPrice.CacheWritePricePerMillion / marketplaceTokenPrice.InputPricePerMillion
				}
			}
			cacheCreationRatio5m = cacheCreationRatio
			cacheCreationRatio1h = cacheCreationRatio * claudeCacheCreation1hMultiplier
			imageRatio, _ = gatewaystore.GetImageRatio(info.OriginModelName)
			audioRatio = gatewaystore.GetAudioRatio(info.OriginModelName)
			audioCompletionRatio = gatewaystore.GetAudioCompletionRatio(info.OriginModelName)
			preConsumedQuota = platformmath.SaturatingMulToInt(
				float64(preConsumedTokens), modelRatio, groupRatioInfo.GroupRatio,
			)
		}
	}
	if usePrice {
		if meta.ImagePriceRatio != 0 {
			modelPrice = modelPrice * meta.ImagePriceRatio
		}
		preConsumedQuota = platformmath.SaturatingMulToInt(
			modelPrice, platformruntime.QuotaPerUnit, groupRatioInfo.GroupRatio,
		)
	}

	if groupRatioInfo.GroupRatio == 0 {
		preConsumedQuota = 0
		freeModel = true
	} else if !gatewaystore.GetQuotaSetting().EnableFreeModelPreConsume {
		if usePrice {
			if modelPrice == 0 {
				preConsumedQuota = 0
				freeModel = true
			}
		} else if modelRatio == 0 {
			preConsumedQuota = 0
			freeModel = true
		}
	}
	priceData := types.PriceData{
		FreeModel:            freeModel,
		ModelPrice:           modelPrice,
		ModelRatio:           modelRatio,
		CompletionRatio:      completionRatio,
		GroupRatioInfo:       groupRatioInfo,
		UsePrice:             usePrice,
		CacheRatio:           cacheRatio,
		ImageRatio:           imageRatio,
		AudioRatio:           audioRatio,
		AudioCompletionRatio: audioCompletionRatio,
		CacheCreationRatio:   cacheCreationRatio,
		CacheCreation5mRatio: cacheCreationRatio5m,
		CacheCreation1hRatio: cacheCreationRatio1h,
		QuotaToPreConsume:    preConsumedQuota,
	}

	if platformconfig.DebugEnabled {
		println(fmt.Sprintf("model_price_helper result: %s", priceData.ToSetting()))
	}
	info.PriceData = priceData
	return priceData, nil
}

func marketplaceChannelModelPrice(c *gin.Context, modelName string) (marketplacedomain.ChannelModelPrice, bool) {
	if httpctx.GetContextKeyString(c, constant.ContextKeyMarketplaceGroupID) == "" {
		return marketplacedomain.ChannelModelPrice{}, false
	}
	prices, ok := httpctx.GetContextKeyType[map[string]marketplacedomain.ChannelModelPrice](c, constant.ContextKeyMarketplaceModelPrices)
	if !ok {
		return marketplacedomain.ChannelModelPrice{}, false
	}
	for configuredModel, price := range prices {
		if strings.EqualFold(configuredModel, strings.TrimSpace(modelName)) {
			return price, true
		}
	}
	return marketplacedomain.ChannelModelPrice{}, false
}

func ModelPriceHelperPerCall(c *gin.Context, info *RelayInfo) (types.PriceData, error) {
	groupRatioInfo := HandleGroupRatio(c, info)

	modelPrice, success := gatewaystore.GetModelPrice(info.OriginModelName, true)
	usePrice := success
	var modelRatio float64
	marketplaceImagePrice, marketplaceImage, err := requiredMarketplaceImagePrice(c, info.OriginModelName)
	if err != nil {
		return types.PriceData{}, err
	}
	if marketplaceImage {
		modelPrice = marketplaceImagePrice
		success = true
		usePrice = true
	}

	if !success {
		defaultPrice, ok := gatewaystore.GetDefaultModelPriceMap()[info.OriginModelName]
		if ok {
			modelPrice = defaultPrice
			usePrice = true
		} else {
			var ratioSuccess bool
			var matchName string
			modelRatio, ratioSuccess, matchName = gatewaystore.GetModelRatio(info.OriginModelName)
			if !ratioSuccess {
				if channelPrice, ok := marketplaceChannelModelPrice(c, info.OriginModelName); ok {
					if channelPrice.EffectiveBillingMode() == marketplacedomain.ChannelBillingModePerCall {
						modelPrice = channelPrice.PricePerCall
						usePrice = true
					} else {
						modelRatio = channelPrice.InputPricePerMillion / 2
						ratioSuccess = true
					}
				}
			}
			acceptUnsetRatio := false
			if info.UserSetting.AcceptUnsetRatioModel {
				acceptUnsetRatio = true
			}
			if !ratioSuccess && !usePrice && !acceptUnsetRatio {
				return types.PriceData{}, modelPriceNotConfiguredError(matchName, info.UserId)
			}
		}
	}

	var quota int
	freeModel := false

	if usePrice {
		quota = platformmath.SaturatingMulToInt(
			modelPrice, platformruntime.QuotaPerUnit, groupRatioInfo.GroupRatio,
		)
		if groupRatioInfo.GroupRatio == 0 || (!gatewaystore.GetQuotaSetting().EnableFreeModelPreConsume && modelPrice == 0) {
			quota = 0
			freeModel = true
		}
	} else {
		quota = platformmath.SaturatingMulToInt(
			modelRatio/2, platformruntime.QuotaPerUnit, groupRatioInfo.GroupRatio,
		)
		modelPrice = -1
		if groupRatioInfo.GroupRatio == 0 || (!gatewaystore.GetQuotaSetting().EnableFreeModelPreConsume && modelRatio == 0) {
			quota = 0
			freeModel = true
		}
	}
	priceData := types.PriceData{
		FreeModel:      freeModel,
		ModelPrice:     modelPrice,
		ModelRatio:     modelRatio,
		UsePrice:       usePrice,
		Quota:          quota,
		GroupRatioInfo: groupRatioInfo,
	}
	return priceData, nil
}

func HasModelBillingConfig(modelName string) bool {
	return gatewaystore.HasModelBillingConfig(modelName)
}

func modelPriceHelperTiered(c *gin.Context, info *RelayInfo, promptTokens int, meta *types.TokenCountMeta, groupRatioInfo types.GroupRatioInfo) (types.PriceData, error) {
	exprStr, ok := gatewaystore.GetBillingExpr(info.OriginModelName)
	if !ok {
		return types.PriceData{}, fmt.Errorf("model %s is configured as tiered_expr but has no billing expression", info.OriginModelName)
	}

	estimatedCompletionTokens := 0
	if meta.MaxTokens != 0 {
		estimatedCompletionTokens = meta.MaxTokens
	}

	requestInput, err := ResolveIncomingBillingExprRequestInput(c, info)
	if err != nil {
		return types.PriceData{}, err
	}

	rawCost, trace, err := billingexpr.RunExprWithRequest(exprStr, billingexpr.TokenParams{
		P:   float64(promptTokens),
		C:   float64(estimatedCompletionTokens),
		Len: float64(promptTokens),
	}, requestInput)
	if err != nil {
		return types.PriceData{}, fmt.Errorf("model %s tiered expr run failed: %w", info.OriginModelName, err)
	}

	quotaBeforeGroup := rawCost / 1_000_000 * platformruntime.QuotaPerUnit
	preConsumedQuota := platformmath.SaturatingMulToInt(quotaBeforeGroup, groupRatioInfo.GroupRatio)

	freeModel := groupRatioInfo.GroupRatio == 0
	if freeModel {
		preConsumedQuota = 0
	}
	exprHash := billingexpr.ExprHashString(exprStr)
	snapshot := &billingexpr.BillingSnapshot{
		BillingMode:               gatewaystore.BillingModeTieredExpr,
		ModelName:                 info.OriginModelName,
		ExprString:                exprStr,
		ExprHash:                  exprHash,
		GroupRatio:                groupRatioInfo.GroupRatio,
		EstimatedPromptTokens:     promptTokens,
		EstimatedCompletionTokens: estimatedCompletionTokens,
		EstimatedQuotaBeforeGroup: quotaBeforeGroup,
		EstimatedQuotaAfterGroup:  preConsumedQuota,
		EstimatedTier:             trace.MatchedTier,
		QuotaPerUnit:              platformruntime.QuotaPerUnit,
		ExprVersion:               billingexpr.ExprVersion(exprStr),
	}
	info.TieredBillingSnapshot = snapshot
	info.BillingRequestInput = &requestInput

	priceData := types.PriceData{
		FreeModel:         freeModel,
		GroupRatioInfo:    groupRatioInfo,
		QuotaToPreConsume: preConsumedQuota,
	}

	if platformconfig.DebugEnabled {
		println(fmt.Sprintf("model_price_helper_tiered result: model=%s preConsume=%d quotaBeforeGroup=%.2f groupRatio=%.2f tier=%s", info.OriginModelName, preConsumedQuota, quotaBeforeGroup, groupRatioInfo.GroupRatio, trace.MatchedTier))
	}

	info.PriceData = priceData
	return priceData, nil
}
