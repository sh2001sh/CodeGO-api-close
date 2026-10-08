package adminops

import "strings"

func convertPricingItemsToRatioData(items []pricingItem) map[string]any {
	modelRatioMap := make(map[string]float64)
	completionRatioMap := make(map[string]float64)
	cacheRatioMap := make(map[string]float64)
	createCacheRatioMap := make(map[string]float64)
	imageRatioMap := make(map[string]float64)
	audioRatioMap := make(map[string]float64)
	audioCompletionRatioMap := make(map[string]float64)
	modelPriceMap := make(map[string]float64)
	billingModeMap := make(map[string]string)
	billingExprMap := make(map[string]string)

	for _, item := range items {
		if item.ModelName == "" {
			continue
		}
		if item.BillingMode == "tiered_expr" && strings.TrimSpace(item.BillingExpr) != "" {
			billingModeMap[item.ModelName] = "tiered_expr"
			billingExprMap[item.ModelName] = item.BillingExpr
		}
		if item.QuotaType == 1 {
			modelPriceMap[item.ModelName] = item.ModelPrice
		} else {
			modelRatioMap[item.ModelName] = item.ModelRatio
			completionRatioMap[item.ModelName] = item.CompletionRatio
		}
		if item.CacheRatio != nil {
			cacheRatioMap[item.ModelName] = *item.CacheRatio
		}
		if item.CreateCacheRatio != nil {
			createCacheRatioMap[item.ModelName] = *item.CreateCacheRatio
		}
		if item.ImageRatio != nil {
			imageRatioMap[item.ModelName] = *item.ImageRatio
		}
		if item.AudioRatio != nil {
			audioRatioMap[item.ModelName] = *item.AudioRatio
		}
		if item.AudioCompletionRatio != nil {
			audioCompletionRatioMap[item.ModelName] = *item.AudioCompletionRatio
		}
	}

	converted := make(map[string]any)
	if len(modelRatioMap) > 0 {
		ratioAny := make(map[string]any, len(modelRatioMap))
		for key, value := range modelRatioMap {
			ratioAny[key] = value
		}
		converted["model_ratio"] = ratioAny
	}
	if len(completionRatioMap) > 0 {
		completionAny := make(map[string]any, len(completionRatioMap))
		for key, value := range completionRatioMap {
			completionAny[key] = value
		}
		converted["completion_ratio"] = completionAny
	}
	if len(cacheRatioMap) > 0 {
		converted["cache_ratio"] = valueMap(cacheRatioMap)
	}
	if len(createCacheRatioMap) > 0 {
		converted["create_cache_ratio"] = valueMap(createCacheRatioMap)
	}
	if len(imageRatioMap) > 0 {
		converted["image_ratio"] = valueMap(imageRatioMap)
	}
	if len(audioRatioMap) > 0 {
		converted["audio_ratio"] = valueMap(audioRatioMap)
	}
	if len(audioCompletionRatioMap) > 0 {
		converted["audio_completion_ratio"] = valueMap(audioCompletionRatioMap)
	}
	if len(modelPriceMap) > 0 {
		priceAny := make(map[string]any, len(modelPriceMap))
		for key, value := range modelPriceMap {
			priceAny[key] = value
		}
		converted["model_price"] = priceAny
	}
	if len(billingModeMap) > 0 {
		converted["billing_mode"] = valueMap(billingModeMap)
	}
	if len(billingExprMap) > 0 {
		converted["billing_expr"] = valueMap(billingExprMap)
	}
	return converted
}
