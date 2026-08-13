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
	blindBoxNumbers, err := listTodayBlindBoxLuckyNumbers(userID, todayDate, now.Unix())
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
		Rules: commercedomain.LuckyNumberRules{
			BaseReward1USD:      setting.BaseReward1USD,
			BaseReward2USD:      setting.BaseReward2USD,
			BaseReward3USD:      setting.BaseReward3USD,
			BaseReward4USD:      setting.BaseReward4USD,
			MultiplierLite:      setting.MultiplierLite,
			MultiplierStandard:  setting.MultiplierStandard,
			MultiplierPro:       setting.MultiplierPro,
			MultiplierUltra:     setting.MultiplierUltra,
			JackpotInitialUSD:   setting.JackpotInitialUSD,
			JackpotIncrementUSD: setting.JackpotIncrementUSD,
			JackpotCapUSD:       setting.JackpotCapUSD,
		},
		Subscriptions:   subscriptions,
		BlindBoxNumbers: blindBoxNumbers,
		RecentRewards:   recentRewards,
	}, nil
}

func listTodayBlindBoxLuckyNumbers(userID int, drawDate string, nowUnix int64) ([]commercedomain.BlindBoxLuckyNumber, error) {
	if !platformdb.DB.Migrator().HasTable(&commerceschema.BlindBoxDailyLuckyNumber{}) {
		return []commercedomain.BlindBoxLuckyNumber{}, nil
	}
	var numbers []commerceschema.BlindBoxDailyLuckyNumber
	if err := platformdb.DB.Where(
		"user_id = ? AND draw_date = ? AND expires_at > ?",
		userID,
		drawDate,
		nowUnix,
	).Order("created_at desc, id desc").Find(&numbers).Error; err != nil {
		return nil, err
	}
	result := make([]commercedomain.BlindBoxLuckyNumber, 0, len(numbers))
	for _, number := range numbers {
		result = append(result, commercedomain.BlindBoxLuckyNumber{
			BlindBoxOpenRecordId: number.BlindBoxOpenRecordId,
			LuckySuffix:          number.LuckySuffix,
			DrawDate:             number.DrawDate,
			ExpiresAt:            number.ExpiresAt,
			CreatedAt:            number.CreatedAt,
		})
	}
	return result, nil
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

// ListDailyLuckyRewardNotifications returns recent user-visible reward notices.
func ListDailyLuckyRewardNotifications(userID, limit int) (*commercedomain.LuckyRewardNotificationPage, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	if !luckyRewardNotificationTableReady() {
		return &commercedomain.LuckyRewardNotificationPage{Items: []commercedomain.LuckyRewardNotification{}}, nil
	}
	if limit < 1 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	var unreadCount int64
	if err := platformdb.DB.Model(&commerceschema.SubscriptionLuckyRewardNotification{}).
		Where("user_id = ? AND read_at = ?", userID, 0).Count(&unreadCount).Error; err != nil {
		return nil, err
	}
	var notifications []commerceschema.SubscriptionLuckyRewardNotification
	if err := platformdb.DB.Where("user_id = ?", userID).
		Order("created_at desc, id desc").Limit(limit).Find(&notifications).Error; err != nil {
		return nil, err
	}
	if len(notifications) == 0 {
		return &commercedomain.LuckyRewardNotificationPage{
			UnreadCount: unreadCount,
			Items:       []commercedomain.LuckyRewardNotification{},
		}, nil
	}
	rewardIDs := make([]int, 0, len(notifications))
	for _, notification := range notifications {
		rewardIDs = append(rewardIDs, notification.RewardId)
	}
	var rewards []commerceschema.SubscriptionLuckyReward
	if err := platformdb.DB.Where("id IN ? AND user_id = ?", rewardIDs, userID).Find(&rewards).Error; err != nil {
		return nil, err
	}
	rewardViews, err := buildLuckyRewardViews(rewards)
	if err != nil {
		return nil, err
	}
	viewByRewardID := make(map[int]commercedomain.LuckyRewardView, len(rewardViews))
	for _, view := range rewardViews {
		viewByRewardID[view.Reward.Id] = view
	}
	items := make([]commercedomain.LuckyRewardNotification, 0, len(notifications))
	for _, notification := range notifications {
		reward, ok := viewByRewardID[notification.RewardId]
		if !ok {
			continue
		}
		items = append(items, commercedomain.LuckyRewardNotification{
			Id:        notification.Id,
			Reward:    reward,
			ReadAt:    notification.ReadAt,
			CreatedAt: notification.CreatedAt,
		})
	}
	return &commercedomain.LuckyRewardNotificationPage{UnreadCount: unreadCount, Items: items}, nil
}

func MarkDailyLuckyRewardNotificationRead(userID, notificationID int) error {
	if userID <= 0 || notificationID <= 0 {
		return errors.New("invalid lucky reward notification params")
	}
	return platformdb.DB.Model(&commerceschema.SubscriptionLuckyRewardNotification{}).
		Where("id = ? AND user_id = ? AND read_at = ?", notificationID, userID, 0).
		Update("read_at", platformruntime.GetTimestamp()).Error
}

func MarkAllDailyLuckyRewardNotificationsRead(userID int) error {
	if userID <= 0 {
		return errors.New("invalid user id")
	}
	return platformdb.DB.Model(&commerceschema.SubscriptionLuckyRewardNotification{}).
		Where("user_id = ? AND read_at = ?", userID, 0).
		Update("read_at", platformruntime.GetTimestamp()).Error
}

// ListDailyLuckyNumberPublicWins returns masked, settled public wins.
func ListDailyLuckyNumberPublicWins(page, pageSize int, drawDate string) (*commercedomain.LuckyPublicWinPage, error) {
	page, pageSize = normalizeLuckyPage(page, pageSize)
	if !luckyPublicWinsTablesReady() {
		return &commercedomain.LuckyPublicWinPage{Page: page, PageSize: pageSize, Records: []commercedomain.LuckyPublicWin{}}, nil
	}
	query := platformdb.DB.Model(&commerceschema.SubscriptionLuckyReward{}).Where("matched_digits > ? AND credit_status = ?", 0, commerceschema.SubscriptionLuckyRewardCreditCredited)
	if drawDate != "" {
		var draw commerceschema.SubscriptionLuckyDraw
		if err := platformdb.DB.Where("draw_date = ?", drawDate).First(&draw).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &commercedomain.LuckyPublicWinPage{Page: page, PageSize: pageSize, Records: []commercedomain.LuckyPublicWin{}}, nil
			}
			return nil, err
		}
		query = query.Where("draw_id = ?", draw.Id)
	}
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
			// Historical rewards can outlive a deleted or failed draw row. Keep
			// the notification/history endpoint readable and omit only that row.
			continue
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
		Id:                   reward.Id,
		DrawId:               reward.DrawId,
		UserSubscriptionId:   reward.UserSubscriptionId,
		BlindBoxOpenRecordId: reward.BlindBoxOpenRecordId,
		ParticipationType:    reward.ParticipationType,
		LuckyNumber:          reward.LuckyNumber,
		MembershipTier:       reward.MembershipTier,
		MatchedDigits:        reward.MatchedDigits,
		BaseRewardUSD:        reward.BaseRewardUSD,
		TierMultiplier:       reward.TierMultiplier,
		JackpotRewardUSD:     reward.JackpotRewardUSD,
		FinalRewardQuota:     reward.FinalRewardQuota,
		CreditStatus:         reward.CreditStatus,
		CreditedAt:           reward.CreditedAt,
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

func luckyRewardNotificationTableReady() bool {
	return platformdb.DB != nil && platformdb.DB.Migrator().HasTable(&commerceschema.SubscriptionLuckyRewardNotification{})
}

func luckyPublicWinsTablesReady() bool {
	return platformdb.DB != nil &&
		platformdb.DB.Migrator().HasTable(&commerceschema.SubscriptionLuckyDraw{}) &&
		platformdb.DB.Migrator().HasTable(&commerceschema.SubscriptionLuckyReward{})
}
