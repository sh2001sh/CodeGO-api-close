package app

import (
	"errors"
	"time"

	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	luckysettings "github.com/sh2001sh/new-api/internal/commerce/luckysettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const luckyDrawDateLayout = "2006-01-02"

// BuildDailyLuckyNumberSelfPayload returns the current user's activity snapshot.
func BuildDailyLuckyNumberSelfPayload(userID int) (*commercedomain.LuckyNumberSelfPayload, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	setting := luckysettings.Get()
	location, err := setting.Location()
	if err != nil {
		return nil, err
	}
	now := time.Now().In(location)
	todayDate := now.Format(luckyDrawDateLayout)
	todayAt := time.Date(now.Year(), now.Month(), now.Day(), setting.DrawHour, setting.DrawMinute, 0, 0, location)
	nextDrawAt := todayAt
	if !now.Before(todayAt) {
		nextDrawAt = todayAt.AddDate(0, 0, 1)
	}

	var todayDraw *commercedomain.LuckyDrawView
	var previousDraw *commercedomain.LuckyDrawView
	if subscriptionLuckyNumberTableReady() {
		var today commerceschema.SubscriptionLuckyDraw
		if err := platformdb.DB.Where("draw_date = ?", todayDate).First(&today).Error; err == nil {
			todayDraw = luckyDrawView(&today)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		var previous commerceschema.SubscriptionLuckyDraw
		if err := platformdb.DB.Where("draw_date < ?", todayDate).Order("draw_date desc, id desc").First(&previous).Error; err == nil {
			previousDraw = luckyDrawView(&previous)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}

	subscriptions, err := buildLuckyNumberSubscriptions(userID)
	if err != nil {
		return nil, err
	}
	recentRewards, err := listLuckyRewardViews(userID, 20, 0)
	if err != nil && subscriptionLuckyNumberTableReady() {
		return nil, err
	}
	if recentRewards == nil {
		recentRewards = []commercedomain.LuckyRewardView{}
	}

	jackpot := setting.JackpotInitialUSD
	if todayDraw != nil {
		jackpot = todayDraw.JackpotAfter
	} else if previousDraw != nil {
		jackpot = previousDraw.JackpotAfter
	}
	return &commercedomain.LuckyNumberSelfPayload{
		Enabled:       setting.Enabled && subscriptionLuckyNumberTableReady(),
		Timezone:      setting.Timezone,
		DrawHour:      setting.DrawHour,
		DrawMinute:    setting.DrawMinute,
		NextDrawAt:    nextDrawAt.Unix(),
		TodayDraw:     todayDraw,
		PreviousDraw:  previousDraw,
		JackpotUSD:    jackpot,
		JackpotCapUSD: setting.JackpotCapUSD,
		Subscriptions: subscriptions,
		RecentRewards: recentRewards,
	}, nil
}

func buildLuckyNumberSubscriptions(userID int) ([]commercedomain.LuckyNumberSubscription, error) {
	active, err := GetAllActiveUserSubscriptions(userID)
	if err != nil {
		return nil, err
	}
	planIDs := make([]int, 0, len(active))
	for _, item := range active {
		if item.Subscription != nil && item.Subscription.LuckyNumber != nil {
			planIDs = append(planIDs, item.Subscription.PlanId)
		}
	}
	planMap := make(map[int]commerceschema.SubscriptionPlan, len(planIDs))
	if len(planIDs) > 0 {
		var plans []commerceschema.SubscriptionPlan
		if err := platformdb.DB.Where("id IN ?", planIDs).Find(&plans).Error; err != nil {
			return nil, err
		}
		for _, plan := range plans {
			planMap[plan.Id] = plan
		}
	}
	result := make([]commercedomain.LuckyNumberSubscription, 0, len(active))
	for _, item := range active {
		if item.Subscription == nil || item.Subscription.LuckyNumber == nil {
			continue
		}
		plan, ok := planMap[item.Subscription.PlanId]
		if !ok || commercedomain.NormalizeSubscriptionPlanType(plan.PlanType) != commerceschema.SubscriptionPlanTypeMonthly || !plan.LuckyDrawEnabled {
			continue
		}
		result = append(result, commercedomain.LuckyNumberSubscription{
			Subscription: *item.Subscription,
			Plan:         plan,
			Number:       item.Subscription.LuckyNumber,
		})
	}
	return result, nil
}

// ListDailyLuckyNumberHistory returns the user's participation and winning history.
func ListDailyLuckyNumberHistory(userID, page, pageSize int) (*commercedomain.LuckyRewardPage, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	page, pageSize = normalizeLuckyPage(page, pageSize)
	if !subscriptionLuckyNumberTableReady() {
		return &commercedomain.LuckyRewardPage{Page: page, PageSize: pageSize, Records: []commercedomain.LuckyRewardView{}}, nil
	}
	var total int64
	query := platformdb.DB.Model(&commerceschema.SubscriptionLuckyReward{}).
		Where("user_id = ? AND matched_digits > ?", userID, 0)
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var rewards []commerceschema.SubscriptionLuckyReward
	if err := query.Order("created_at desc, id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rewards).Error; err != nil {
		return nil, err
	}
	records, err := buildLuckyRewardViews(rewards)
	if err != nil {
		return nil, err
	}
	return &commercedomain.LuckyRewardPage{Page: page, PageSize: pageSize, Total: total, Records: records}, nil
}

// ListDailyLuckyNumberPublicWins returns masked high-value public wins.
func ListDailyLuckyNumberPublicWins(page, pageSize int) (*commercedomain.LuckyPublicWinPage, error) {
	page, pageSize = normalizeLuckyPage(page, pageSize)
	if !subscriptionLuckyNumberTableReady() {
		return &commercedomain.LuckyPublicWinPage{Page: page, PageSize: pageSize, Records: []commercedomain.LuckyPublicWin{}}, nil
	}
	query := platformdb.DB.Model(&commerceschema.SubscriptionLuckyReward{}).Where("matched_digits >= ? AND credit_status = ?", 3, commerceschema.SubscriptionLuckyRewardCreditCredited)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var rewards []commerceschema.SubscriptionLuckyReward
	if err := query.Order("created_at desc, id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rewards).Error; err != nil {
		return nil, err
	}
	views, err := buildLuckyRewardViews(rewards)
	if err != nil {
		return nil, err
	}
	records := make([]commercedomain.LuckyPublicWin, 0, len(views))
	for _, view := range views {
		records = append(records, commercedomain.LuckyPublicWin{
			DrawDate:       view.DrawDate,
			WinningNumber:  view.WinningNumber,
			MembershipTier: view.Reward.MembershipTier,
			LuckySuffix:    maskLuckySuffix(view.Reward.LuckyNumber),
			MatchedDigits:  view.Reward.MatchedDigits,
			RewardUSD:      view.RewardUSD,
		})
	}
	return &commercedomain.LuckyPublicWinPage{Page: page, PageSize: pageSize, Total: total, Records: records}, nil
}

func normalizeLuckyPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func listLuckyRewardViews(userID, limit, offset int) ([]commercedomain.LuckyRewardView, error) {
	var rewards []commerceschema.SubscriptionLuckyReward
	query := platformdb.DB.Where("user_id = ? AND matched_digits > ?", userID, 0).
		Order("created_at desc, id desc").Limit(limit).Offset(offset)
	if err := query.Find(&rewards).Error; err != nil {
		return nil, err
	}
	return buildLuckyRewardViews(rewards)
}

func buildLuckyRewardViews(rewards []commerceschema.SubscriptionLuckyReward) ([]commercedomain.LuckyRewardView, error) {
	if len(rewards) == 0 {
		return []commercedomain.LuckyRewardView{}, nil
	}
	drawIDs := make([]int, 0, len(rewards))
	seen := make(map[int]struct{}, len(rewards))
	for _, reward := range rewards {
		if _, ok := seen[reward.DrawId]; ok {
			continue
		}
		seen[reward.DrawId] = struct{}{}
		drawIDs = append(drawIDs, reward.DrawId)
	}
	var draws []commerceschema.SubscriptionLuckyDraw
	if err := platformdb.DB.Where("id IN ?", drawIDs).Find(&draws).Error; err != nil {
		return nil, err
	}
	drawMap := make(map[int]commerceschema.SubscriptionLuckyDraw, len(draws))
	for _, draw := range draws {
		drawMap[draw.Id] = draw
	}
	result := make([]commercedomain.LuckyRewardView, 0, len(rewards))
	for _, reward := range rewards {
		draw, ok := drawMap[reward.DrawId]
		if !ok {
			return nil, errors.New("lucky draw not found for reward")
		}
		result = append(result, commercedomain.LuckyRewardView{
			Reward:        luckyRewardRecord(&reward),
			DrawDate:      draw.DrawDate,
			WinningNumber: draw.WinningNumber,
			RewardUSD:     quotaToLuckyUSD(reward.FinalRewardQuota),
		})
	}
	return result, nil
}

func luckyDrawView(draw *commerceschema.SubscriptionLuckyDraw) *commercedomain.LuckyDrawView {
	if draw == nil {
		return nil
	}
	return &commercedomain.LuckyDrawView{
		Id:             draw.Id,
		DrawDate:       draw.DrawDate,
		WinningNumber:  draw.WinningNumber,
		JackpotBefore:  draw.JackpotBefore,
		JackpotAfter:   draw.JackpotAfter,
		FullMatchCount: draw.FullMatchCount,
		Status:         draw.Status,
		DrawnAt:        draw.DrawnAt,
		CompletedAt:    draw.CompletedAt,
	}
}

func luckyRewardRecord(reward *commerceschema.SubscriptionLuckyReward) commercedomain.LuckyRewardRecord {
	if reward == nil {
		return commercedomain.LuckyRewardRecord{}
	}
	return commercedomain.LuckyRewardRecord{
		Id:                 reward.Id,
		DrawId:             reward.DrawId,
		UserSubscriptionId: reward.UserSubscriptionId,
		LuckyNumber:        reward.LuckyNumber,
		MembershipTier:     reward.MembershipTier,
		MatchedDigits:      reward.MatchedDigits,
		BaseRewardUSD:      reward.BaseRewardUSD,
		TierMultiplier:     reward.TierMultiplier,
		JackpotRewardUSD:   reward.JackpotRewardUSD,
		FinalRewardQuota:   reward.FinalRewardQuota,
		CreditStatus:       reward.CreditStatus,
		CreditedAt:         reward.CreditedAt,
	}
}

func quotaToLuckyUSD(quota int64) float64 {
	if quota <= 0 || platformruntime.QuotaPerUnit <= 0 {
		return 0
	}
	return float64(quota) / platformruntime.QuotaPerUnit
}

func maskLuckySuffix(value string) string {
	if len(value) != 4 {
		return "****"
	}
	return "**" + value[2:]
}
