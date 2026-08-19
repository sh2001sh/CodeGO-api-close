package app

import (
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"time"

	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
)

type MiniProgramDailyUsageItem struct {
	Date      string `json:"date"`
	Timestamp int64  `json:"timestamp"`
	Requests  int64  `json:"requests"`
	Quota     int64  `json:"quota"`
	TokenUsed int64  `json:"token_used"`
}

// BuildMiniProgramDashboard returns the mini program dashboard aggregate.
func BuildMiniProgramDashboard(userID int, days int) (map[string]any, error) {
	user, err := getWorkflowUserByID(userID, false)
	if err != nil {
		return nil, err
	}

	trend, err := getMiniProgramTrend(userID, days)
	if err != nil {
		return nil, err
	}

	startTimestamp := time.Now().AddDate(0, 0, -days).Unix()
	usageStat, err := auditapp.GetUserLogStats(userID, auditapp.LogListQuery{
		LogType:        auditschema.LogTypeConsume,
		StartTimestamp: startTimestamp,
		EndTimestamp:   time.Now().Unix(),
	})
	if err != nil {
		return nil, err
	}

	subscriptions, err := commerceapp.GetAllActiveUserSubscriptions(userID)
	if err != nil {
		return nil, err
	}
	blindBoxOverview, err := commerceapp.GetBlindBoxOverview(userID, 5)
	if err != nil {
		return nil, err
	}
	resetOpportunity, err := commerceapp.GetUserSubscriptionResetOpportunity(userID)
	if err != nil {
		return nil, err
	}

	website, _ := identityapp.BuildMiniProgramMeResponse("", 0)
	websiteLinks, _ := website["website"].(map[string]any)
	campaigns := []map[string]any{
		{
			"id":              "invite-month-card-reset",
			"title":           "邀请新用户购买月卡，送 1 次额度重置机会",
			"subtitle":        "机会可长期保存，每个自然月最多使用 1 次，去官网订阅页执行。",
			"badge":           "拉新活动",
			"page_path":       "/pages/campaign-reset/index",
			"website_url":     websiteLinks["packages_url"],
			"available_count": resetOpportunity.AvailableCount,
			"used_this_month": resetOpportunity.UsedThisMonth,
		},
	}

	return map[string]any{
		"user": map[string]any{
			"id":             user.Id,
			"display_name":   user.DisplayName,
			"account_masked": websiteLinksOrMaskUser(user),
			"quota":          user.Quota,
			"used_quota":     user.UsedQuota,
			"request_count":  user.RequestCount,
			"group":          user.Group,
		},
		"usage": map[string]any{
			"days":  days,
			"quota": usageStat.Quota,
			"rpm":   usageStat.Rpm,
			"tpm":   usageStat.Tpm,
			"trend": trend,
		},
		"subscriptions": subscriptions,
		"blind_box":     blindBoxOverview,
		"reset_opportunity": map[string]any{
			"available_count": resetOpportunity.AvailableCount,
			"earned_total":    resetOpportunity.EarnedTotal,
			"used_total":      resetOpportunity.UsedTotal,
			"used_this_month": resetOpportunity.UsedThisMonth,
			"current_month":   resetOpportunity.CurrentMonth,
			"last_used_month": resetOpportunity.LastUsedMonth,
		},
		"campaigns": campaigns,
	}, nil
}

// BuildMiniProgramStat returns aggregated recent usage stats and trend data.
func BuildMiniProgramStat(userID int, days int) (map[string]any, error) {
	_, err := getWorkflowUserByID(userID, false)
	if err != nil {
		return nil, err
	}

	endTimestamp := time.Now().Unix()
	startTimestamp := time.Now().AddDate(0, 0, -days).Unix()

	trend, err := getMiniProgramTrend(userID, days)
	if err != nil {
		return nil, err
	}
	stat, err := auditapp.GetUserLogStats(userID, auditapp.LogListQuery{
		LogType:        auditschema.LogTypeConsume,
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	})
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"days":  days,
		"quota": stat.Quota,
		"rpm":   stat.Rpm,
		"tpm":   stat.Tpm,
		"trend": trend,
	}, nil
}

// BuildMiniProgramGeneMap returns the existing gene-map snapshot for the bound account.
func BuildMiniProgramGeneMap(userID int, days int) (any, error) {
	return GenerateGeneMapSnapshot(userID, days)
}

// NormalizeMiniProgramWindowDays normalizes the requested mini program dashboard/stat window size.
func NormalizeMiniProgramWindowDays(raw string, fallback int) int {
	return identityapp.NormalizeMiniProgramWindowDays(raw, fallback)
}

func getMiniProgramTrend(userID int, days int) ([]MiniProgramDailyUsageItem, error) {
	now := time.Now().In(time.Local)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	start := dayStart.AddDate(0, 0, -(days - 1))

	rows, err := auditapp.ListUserQuotaDates(userID, start.Unix(), now.Unix())
	if err != nil {
		return nil, err
	}

	result := make([]MiniProgramDailyUsageItem, 0, days)
	indexByDate := make(map[string]int, days)
	for i := 0; i < days; i++ {
		current := start.AddDate(0, 0, i)
		key := current.Format("2006-01-02")
		indexByDate[key] = len(result)
		result = append(result, MiniProgramDailyUsageItem{
			Date:      key,
			Timestamp: current.Unix(),
		})
	}

	for _, row := range rows {
		if row == nil {
			continue
		}
		key := time.Unix(row.CreatedAt, 0).In(time.Local).Format("2006-01-02")
		index, ok := indexByDate[key]
		if !ok {
			continue
		}
		result[index].Requests += int64(row.Count)
		result[index].Quota += int64(row.Quota)
		result[index].TokenUsed += int64(row.TokenUsed)
	}

	return result, nil
}

func websiteLinksOrMaskUser(user *identityschema.User) string {
	if user == nil {
		return ""
	}
	if user.Email != "" {
		return user.Email
	}
	if user.DisplayName != "" {
		return user.DisplayName
	}
	return user.Username
}
