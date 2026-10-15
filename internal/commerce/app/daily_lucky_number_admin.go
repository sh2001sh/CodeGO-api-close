package app

import (
	"errors"
	"strconv"
	"strings"
	"time"

	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	luckysettings "github.com/sh2001sh/new-api/internal/commerce/luckysettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type LuckyNumberConfigUpdateRequest struct {
	Enabled             *bool    `json:"enabled"`
	Timezone            *string  `json:"timezone"`
	DrawHour            *int     `json:"draw_hour"`
	DrawMinute          *int     `json:"draw_minute"`
	BaseReward1USD      *float64 `json:"base_reward_1_usd"`
	BaseReward2USD      *float64 `json:"base_reward_2_usd"`
	BaseReward3USD      *float64 `json:"base_reward_3_usd"`
	BaseReward4USD      *float64 `json:"base_reward_4_usd"`
	MultiplierLite      *float64 `json:"multiplier_lite"`
	MultiplierStandard  *float64 `json:"multiplier_standard"`
	MultiplierPro       *float64 `json:"multiplier_pro"`
	MultiplierUltra     *float64 `json:"multiplier_ultra"`
	JackpotInitialUSD   *float64 `json:"jackpot_initial_usd"`
	JackpotIncrementUSD *float64 `json:"jackpot_increment_usd"`
	JackpotCapUSD       *float64 `json:"jackpot_cap_usd"`
	CostPerUSD          *float64 `json:"cost_per_usd"`
	MonthlyBudgetUSD    *float64 `json:"monthly_budget_usd"`
}

func GetDailyLuckyNumberConfig() luckysettings.Setting {
	return luckysettings.Get()
}

// UpdateDailyLuckyNumberConfig persists only the requested fields. Existing draws
// retain their configuration snapshot because they are never updated here.
func UpdateDailyLuckyNumberConfig(req LuckyNumberConfigUpdateRequest) (luckysettings.Setting, error) {
	candidate := luckysettings.Get()
	applyLuckyNumberConfigRequest(&candidate, req)
	if err := luckysettings.Validate(candidate); err != nil {
		return luckysettings.Setting{}, err
	}
	updates := luckyNumberConfigOptionValues(req, candidate)
	for _, update := range updates {
		if err := platformstore.UpdateOption("daily_lucky_number_setting."+update.key, update.value); err != nil {
			return luckysettings.Setting{}, err
		}
	}
	return luckysettings.Get(), nil
}

type luckyNumberConfigOptionValue struct {
	key   string
	value string
}

func applyLuckyNumberConfigRequest(setting *luckysettings.Setting, req LuckyNumberConfigUpdateRequest) {
	if req.Enabled != nil {
		setting.Enabled = *req.Enabled
	}
	if req.Timezone != nil {
		setting.Timezone = strings.TrimSpace(*req.Timezone)
	}
	if req.DrawHour != nil {
		setting.DrawHour = *req.DrawHour
	}
	if req.DrawMinute != nil {
		setting.DrawMinute = *req.DrawMinute
	}
	if req.BaseReward1USD != nil {
		setting.BaseReward1USD = *req.BaseReward1USD
	}
	if req.BaseReward2USD != nil {
		setting.BaseReward2USD = *req.BaseReward2USD
	}
	if req.BaseReward3USD != nil {
		setting.BaseReward3USD = *req.BaseReward3USD
	}
	if req.BaseReward4USD != nil {
		setting.BaseReward4USD = *req.BaseReward4USD
	}
	if req.MultiplierLite != nil {
		setting.MultiplierLite = *req.MultiplierLite
	}
	if req.MultiplierStandard != nil {
		setting.MultiplierStandard = *req.MultiplierStandard
	}
	if req.MultiplierPro != nil {
		setting.MultiplierPro = *req.MultiplierPro
	}
	if req.MultiplierUltra != nil {
		setting.MultiplierUltra = *req.MultiplierUltra
	}
	if req.JackpotInitialUSD != nil {
		setting.JackpotInitialUSD = *req.JackpotInitialUSD
	}
	if req.JackpotIncrementUSD != nil {
		setting.JackpotIncrementUSD = *req.JackpotIncrementUSD
	}
	if req.JackpotCapUSD != nil {
		setting.JackpotCapUSD = *req.JackpotCapUSD
	}
	if req.CostPerUSD != nil {
		setting.CostPerUSD = *req.CostPerUSD
	}
	if req.MonthlyBudgetUSD != nil {
		setting.MonthlyBudgetUSD = *req.MonthlyBudgetUSD
	}
}

func luckyNumberConfigOptionValues(req LuckyNumberConfigUpdateRequest, setting luckysettings.Setting) []luckyNumberConfigOptionValue {
	values := make([]luckyNumberConfigOptionValue, 0, 17)
	appendValue := func(requested bool, key, value string) {
		if requested {
			values = append(values, luckyNumberConfigOptionValue{key: key, value: value})
		}
	}
	appendValue(req.Enabled != nil, "enabled", strconv.FormatBool(setting.Enabled))
	appendValue(req.Timezone != nil, "timezone", setting.Timezone)
	appendValue(req.DrawHour != nil, "draw_hour", strconv.Itoa(setting.DrawHour))
	appendValue(req.DrawMinute != nil, "draw_minute", strconv.Itoa(setting.DrawMinute))
	appendValue(req.BaseReward1USD != nil, "base_reward_1_usd", formatLuckyFloat(setting.BaseReward1USD))
	appendValue(req.BaseReward2USD != nil, "base_reward_2_usd", formatLuckyFloat(setting.BaseReward2USD))
	appendValue(req.BaseReward3USD != nil, "base_reward_3_usd", formatLuckyFloat(setting.BaseReward3USD))
	appendValue(req.BaseReward4USD != nil, "base_reward_4_usd", formatLuckyFloat(setting.BaseReward4USD))
	appendValue(req.MultiplierLite != nil, "multiplier_lite", formatLuckyFloat(setting.MultiplierLite))
	appendValue(req.MultiplierStandard != nil, "multiplier_standard", formatLuckyFloat(setting.MultiplierStandard))
	appendValue(req.MultiplierPro != nil, "multiplier_pro", formatLuckyFloat(setting.MultiplierPro))
	appendValue(req.MultiplierUltra != nil, "multiplier_ultra", formatLuckyFloat(setting.MultiplierUltra))
	appendValue(req.JackpotInitialUSD != nil, "jackpot_initial_usd", formatLuckyFloat(setting.JackpotInitialUSD))
	appendValue(req.JackpotIncrementUSD != nil, "jackpot_increment_usd", formatLuckyFloat(setting.JackpotIncrementUSD))
	appendValue(req.JackpotCapUSD != nil, "jackpot_cap_usd", formatLuckyFloat(setting.JackpotCapUSD))
	appendValue(req.CostPerUSD != nil, "cost_per_usd", formatLuckyFloat(setting.CostPerUSD))
	appendValue(req.MonthlyBudgetUSD != nil, "monthly_budget_usd", formatLuckyFloat(setting.MonthlyBudgetUSD))
	return values
}

func formatLuckyFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// BuildDailyLuckyNumberAdminPayload returns immutable draw snapshots and cost summaries.
func BuildDailyLuckyNumberAdminPayload(page, pageSize int) (*commercedomain.LuckyDrawAdminPayload, error) {
	page, pageSize = normalizeLuckyPage(page, pageSize)
	var total int64
	if err := platformdb.DB.Model(&commerceschema.SubscriptionLuckyDraw{}).Count(&total).Error; err != nil {
		return nil, err
	}
	var draws []commerceschema.SubscriptionLuckyDraw
	if err := platformdb.DB.Order("draw_date desc, id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&draws).Error; err != nil {
		return nil, err
	}
	stats, err := aggregateLuckyDrawStats(draws)
	if err != nil {
		return nil, err
	}
	views := make([]commercedomain.LuckyDrawAdminView, 0, len(draws))
	for _, draw := range draws {
		stat := stats[draw.Id]
		views = append(views, commercedomain.LuckyDrawAdminView{
			Draw:             draw,
			ParticipantCount: stat.ParticipantCount,
			RewardCount:      stat.RewardCount,
			CreditedCount:    stat.CreditedCount,
			NominalRewardUSD: stat.NominalRewardUSD,
			ActualCostCNY:    stat.NominalRewardUSD * draw.CostPerUSD,
		})
	}
	setting := luckysettings.Get()
	monthlyNominal, monthlyCost, err := monthlyLuckyDrawStats(setting)
	if err != nil {
		return nil, err
	}
	usage := float64(0)
	if setting.MonthlyBudgetUSD > 0 {
		usage = monthlyNominal / setting.MonthlyBudgetUSD * 100
	}
	return &commercedomain.LuckyDrawAdminPayload{
		Config:                    setting,
		Draws:                     views,
		Page:                      page,
		PageSize:                  pageSize,
		Total:                     total,
		MonthlyNominalRewardUSD:   monthlyNominal,
		MonthlyActualCostCNY:      monthlyCost,
		MonthlyBudgetUSD:          setting.MonthlyBudgetUSD,
		MonthlyBudgetUsagePercent: usage,
	}, nil
}

type luckyDrawAggregate struct {
	RewardCount      int64   `gorm:"column:reward_count"`
	ParticipantCount int64   `gorm:"column:participant_count"`
	CreditedCount    int64   `gorm:"column:credited_count"`
	NominalRewardUSD float64 `gorm:"column:nominal_reward_usd"`
}

func aggregateLuckyDrawStats(draws []commerceschema.SubscriptionLuckyDraw) (map[int]luckyDrawAggregate, error) {
	result := make(map[int]luckyDrawAggregate, len(draws))
	if len(draws) == 0 {
		return result, nil
	}
	drawIDs := make([]int, 0, len(draws))
	for _, draw := range draws {
		drawIDs = append(drawIDs, draw.Id)
	}
	var rows []struct {
		DrawID           int     `gorm:"column:draw_id"`
		RewardCount      int64   `gorm:"column:reward_count"`
		ParticipantCount int64   `gorm:"column:participant_count"`
		CreditedCount    int64   `gorm:"column:credited_count"`
		NominalRewardUSD float64 `gorm:"column:nominal_reward_usd"`
	}
	if err := platformdb.DB.Model(&commerceschema.SubscriptionLuckyReward{}).
		Select("draw_id, COUNT(*) AS reward_count, COUNT(*) AS participant_count, SUM(CASE WHEN credit_status = 'credited' THEN 1 ELSE 0 END) AS credited_count, COALESCE(SUM(base_reward_usd * tier_multiplier + jackpot_reward_usd), 0) AS nominal_reward_usd").
		Where("draw_id IN ?", drawIDs).Group("draw_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.DrawID] = luckyDrawAggregate{
			RewardCount:      row.RewardCount,
			ParticipantCount: row.ParticipantCount,
			CreditedCount:    row.CreditedCount,
			NominalRewardUSD: row.NominalRewardUSD,
		}
	}
	return result, nil
}

func monthlyLuckyDrawStats(setting luckysettings.Setting) (float64, float64, error) {
	location, err := setting.Location()
	if err != nil {
		return 0, 0, err
	}
	prefix := time.Now().In(location).Format("2006-01") + "-%"
	var row struct {
		Nominal float64 `gorm:"column:nominal"`
		Cost    float64 `gorm:"column:cost"`
	}
	err = platformdb.DB.Table("subscription_lucky_rewards AS reward").
		Select("COALESCE(SUM(reward.base_reward_usd * reward.tier_multiplier + reward.jackpot_reward_usd), 0) AS nominal, COALESCE(SUM((reward.base_reward_usd * reward.tier_multiplier + reward.jackpot_reward_usd) * draw.cost_per_usd), 0) AS cost").
		Joins("JOIN subscription_lucky_draws AS draw ON draw.id = reward.draw_id").
		Where("draw.draw_date LIKE ?", prefix).Scan(&row).Error
	return row.Nominal, row.Cost, err
}

// BackfillDailyLuckyNumbers allocates missing identifiers for current eligible subscriptions.
func BackfillDailyLuckyNumbers() (*commercedomain.LuckyBackfillResult, error) {
	if !subscriptionLuckyNumberTableReady() {
		return nil, errors.New("daily lucky number migration is not applied")
	}
	now := platformruntime.GetTimestamp()
	var subscriptions []commerceschema.UserSubscription
	if err := platformdb.DB.Where("status = ? AND end_time > ?", "active", now).Order("id asc").Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	planIDs := make([]int, 0, len(subscriptions))
	for _, sub := range subscriptions {
		planIDs = append(planIDs, sub.PlanId)
	}
	var plans []commerceschema.SubscriptionPlan
	if len(planIDs) > 0 {
		if err := platformdb.DB.Where("id IN ?", planIDs).Find(&plans).Error; err != nil {
			return nil, err
		}
	}
	planMap := make(map[int]commerceschema.SubscriptionPlan, len(plans))
	for _, plan := range plans {
		planMap[plan.Id] = plan
	}
	result := &commercedomain.LuckyBackfillResult{FailedIDs: []int{}}
	for _, sub := range subscriptions {
		plan, ok := planMap[sub.PlanId]
		if !ok || commercedomain.NormalizeSubscriptionPlanType(plan.PlanType) != commerceschema.SubscriptionPlanTypeMonthly || !plan.LuckyDrawEnabled {
			continue
		}
		result.Scanned++
		var number commerceschema.SubscriptionLuckyNumber
		if err := platformdb.DB.Where("user_subscription_id = ?", sub.Id).First(&number).Error; err == nil {
			result.AlreadyExists++
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			result.Failed++
			result.FailedIDs = append(result.FailedIDs, sub.Id)
			continue
		}
		err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
			var locked commerceschema.UserSubscription
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", sub.Id).First(&locked).Error; err != nil {
				return err
			}
			_, err := ensureSubscriptionLuckyNumberTx(tx, &locked, &plan)
			return err
		})
		if err != nil {
			result.Failed++
			result.FailedIDs = append(result.FailedIDs, sub.Id)
			continue
		}
		result.Created++
	}
	return result, nil
}
