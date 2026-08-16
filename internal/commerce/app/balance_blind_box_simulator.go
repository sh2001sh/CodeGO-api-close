package app

import (
	"errors"

	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
)

const maxBlindBoxSimulationBalanceUSD = int64(1_000_000)

type BalanceBlindBoxSimulationDraw struct {
	RewardType   string  `json:"reward_type"`
	RewardTier   string  `json:"reward_tier"`
	RewardUSD    float64 `json:"reward_usd"`
	CreditAmount int64   `json:"credit_amount"`
	RewardTitle  string  `json:"reward_title"`
}

type BalanceBlindBoxSimulationResult struct {
	PriceQuota    int64                           `json:"price_quota"`
	BalanceBefore int64                           `json:"balance_before"`
	CostQuota     int64                           `json:"cost_quota"`
	RewardQuota   int64                           `json:"reward_quota"`
	BalanceAfter  int64                           `json:"balance_after"`
	Draws         []BalanceBlindBoxSimulationDraw `json:"draws"`
}

// SimulateBalanceBlindBoxes draws from the live unified pool without writing assets or history.
func SimulateBalanceBlindBoxes(balanceQuota int64, count int) (*BalanceBlindBoxSimulationResult, error) {
	setting := blindboxsettings.Get()
	priceQuota := quotaUnitsFromBlindBoxUSD(setting.BalanceBlindBoxPriceUSD)
	maxBalance := maxBlindBoxSimulationBalanceUSD * int64(platformruntime.QuotaPerUnit)
	if !setting.Enabled || !setting.BalanceBlindBoxEnabled {
		return nil, errors.New("统一盲盒当前不可用")
	}
	if balanceQuota < priceQuota || balanceQuota > maxBalance || count < 1 || count > balanceBlindBoxMaxBatch {
		return nil, errors.New("模拟抽盒参数无效，初始额度最高为 1,000,000，单次最多抽取 100 个")
	}
	costQuota := priceQuota * int64(count)
	if balanceQuota < costQuota {
		return nil, errors.New("模拟额度不足")
	}

	result := &BalanceBlindBoxSimulationResult{
		PriceQuota: priceQuota, BalanceBefore: balanceQuota, CostQuota: costQuota,
		Draws: make([]BalanceBlindBoxSimulationDraw, 0, count),
	}
	pity := commerceschema.BalanceBlindBoxPityState{}
	for index := 0; index < count; index++ {
		item := issueSealedBalanceBlindBox(0, 0, setting, &pity, false)
		draw := BalanceBlindBoxSimulationDraw{
			RewardType: item.RewardType, RewardTier: item.RewardTier, RewardUSD: item.RewardUSD,
			CreditAmount: item.CreditAmount, RewardTitle: item.RewardTitle,
		}
		result.Draws = append(result.Draws, draw)
		result.RewardQuota += item.CreditAmount
	}
	result.BalanceAfter = balanceQuota - costQuota + result.RewardQuota
	return result, nil
}
