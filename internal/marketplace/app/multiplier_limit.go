package app

import (
	"errors"
	"fmt"
	"math"
	"strconv"
)

const MaxMarketplaceMultiplierLimit = 1_000_000

func ValidateMultiplierLimitValue(limit float64) error {
	if math.IsNaN(limit) || math.IsInf(limit, 0) || limit < 0 || limit > MaxMarketplaceMultiplierLimit {
		return fmt.Errorf("第三方倍率上限必须在 0 到 %d 之间，0 表示不限制", MaxMarketplaceMultiplierLimit)
	}
	return nil
}

func MultiplierWithinLimit(multiplier, limit float64) bool {
	return limit <= 0 || multiplier <= limit+1e-9
}

func EnforceMultiplierLimit(multiplier, limit float64) error {
	if err := ValidateMultiplierLimitValue(limit); err != nil {
		return err
	}
	if MultiplierWithinLimit(multiplier, limit) {
		return nil
	}
	return errors.New("当前第三方渠道倍率 " + formatLimitMultiplier(multiplier) + "x 超过 API Key 上限 " + formatLimitMultiplier(limit) + "x，请调整分组或倍率上限")
}

func multiplierLimitExceededError(limit float64) error {
	return errors.New("Auto 路由池中支持该模型的分组倍率均超过 API Key 上限 " + formatLimitMultiplier(limit) + "x")
}

func formatLimitMultiplier(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
