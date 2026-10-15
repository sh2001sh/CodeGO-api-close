package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

const maxChannelModelPricePerMillion = 1_000_000

func encodeChannelModelPrices(prices map[string]ChannelModelPrice, models []string) (string, error) {
	normalized, err := normalizeChannelModelPrices(prices, models)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(normalized)
	return string(data), err
}

func decodeChannelModelPrices(raw string) map[string]ChannelModelPrice {
	prices := make(map[string]ChannelModelPrice)
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &prices) != nil {
		return map[string]ChannelModelPrice{}
	}
	return prices
}

func normalizeChannelModelPrices(prices map[string]ChannelModelPrice, models []string) (map[string]ChannelModelPrice, error) {
	result := make(map[string]ChannelModelPrice)
	declared := make(map[string]string, len(models))
	for _, model := range normalizeModels(models) {
		declared[strings.ToLower(model)] = model
	}
	for requested, price := range prices {
		model, ok := declared[strings.ToLower(strings.TrimSpace(requested))]
		if !ok {
			return nil, fmt.Errorf("模型 %s 不在当前渠道的声明列表中", requested)
		}
		if err := validateChannelModelPrice(price); err != nil {
			return nil, fmt.Errorf("模型 %s 的渠道价格无效: %w", model, err)
		}
		price.BillingMode = price.EffectiveBillingMode()
		if price.BillingMode == ChannelBillingModePerCall {
			price.InputPricePerMillion = 0
			price.OutputPricePerMillion = 0
			price.CacheReadPricePerMillion = nil
			price.CacheWritePricePerMillion = nil
		} else {
			price.PricePerCall = 0
		}
		result[model] = price
	}
	return result, nil
}

func validateChannelModelPrice(price ChannelModelPrice) error {
	if price.BillingMode != "" &&
		price.BillingMode != ChannelBillingModeToken &&
		price.BillingMode != ChannelBillingModePerCall {
		return errors.New("计费模式必须是按量或按次")
	}
	if price.EffectiveBillingMode() == ChannelBillingModePerCall {
		return validatePositiveChannelPrice(price.PricePerCall, "每次请求价格")
	}

	values := []float64{price.InputPricePerMillion, price.OutputPricePerMillion}
	for _, value := range values {
		if err := validatePositiveChannelPrice(value, "输入和输出价格"); err != nil {
			return err
		}
	}
	for _, optional := range []struct {
		name  string
		value *float64
	}{
		{name: "缓存读取价格", value: price.CacheReadPricePerMillion},
		{name: "缓存写入价格", value: price.CacheWritePricePerMillion},
	} {
		if optional.value == nil {
			continue
		}
		if math.IsNaN(*optional.value) || math.IsInf(*optional.value, 0) || *optional.value < 0 {
			return fmt.Errorf("%s必须是大于或等于 0 的有效数字", optional.name)
		}
		if *optional.value > maxChannelModelPricePerMillion {
			return fmt.Errorf("%s不能超过 %d", optional.name, maxChannelModelPricePerMillion)
		}
	}
	return nil
}

func validatePositiveChannelPrice(value float64, name string) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return fmt.Errorf("%s必须是大于 0 的有效数字", name)
	}
	if value > maxChannelModelPricePerMillion {
		return fmt.Errorf("%s不能超过 %d", name, maxChannelModelPricePerMillion)
	}
	return nil
}

func channelModelPriceForModel(prices map[string]ChannelModelPrice, model string) (ChannelModelPrice, bool) {
	for configuredModel, price := range prices {
		if strings.EqualFold(configuredModel, strings.TrimSpace(model)) {
			return price, true
		}
	}
	return ChannelModelPrice{}, false
}

func retainChannelModelPrices(prices map[string]ChannelModelPrice, models []string) map[string]ChannelModelPrice {
	retained := make(map[string]ChannelModelPrice)
	for _, model := range normalizeModels(models) {
		if price, ok := channelModelPriceForModel(prices, model); ok {
			retained[model] = price
		}
	}
	return retained
}
