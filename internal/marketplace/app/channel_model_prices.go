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
		result[model] = price
	}
	return result, nil
}

func validateChannelModelPrice(price ChannelModelPrice) error {
	values := []float64{price.InputPricePerMillion, price.OutputPricePerMillion}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
			return errors.New("输入和输出价格必须是大于 0 的有效数字")
		}
		if value > maxChannelModelPricePerMillion {
			return fmt.Errorf("每百万 Token 价格不能超过 %d", maxChannelModelPricePerMillion)
		}
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
