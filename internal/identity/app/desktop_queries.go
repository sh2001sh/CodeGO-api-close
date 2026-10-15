package app

import (
	"errors"
	"fmt"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
	"gorm.io/gorm"
	"sort"
	"strings"
	"time"
)

type DesktopAccountSnapshot struct {
	ID                 int      `json:"id"`
	Username           string   `json:"username"`
	DisplayName        string   `json:"display_name"`
	Group              string   `json:"group"`
	Quota              int      `json:"quota"`
	UsedQuota          int      `json:"used_quota"`
	RequestCount       int      `json:"request_count"`
	QuotaUSD           float64  `json:"quota_usd"`
	UsedQuotaUSD       float64  `json:"used_quota_usd"`
	BillingPreference  string   `json:"billing_preference"`
	FundingSourceOrder []string `json:"funding_source_order"`
}

type DesktopTokenSummary struct {
	Total        int                   `json:"total"`
	DesktopToken *identityschema.Token `json:"desktop_token,omitempty"`
}

// DesktopSubscriptionSnapshot is the compact active-plan view used by the desktop console.
type DesktopSubscriptionSnapshot struct {
	ID                 int     `json:"id"`
	PlanID             int     `json:"plan_id"`
	PlanTitle          string  `json:"plan_title"`
	AmountTotalUSD     float64 `json:"amount_total_usd"`
	AmountUsedUSD      float64 `json:"amount_used_usd"`
	RemainingUSD       float64 `json:"remaining_usd"`
	Unlimited          bool    `json:"unlimited"`
	PeriodAmountUSD    float64 `json:"period_amount_usd"`
	PeriodUsedUSD      float64 `json:"period_used_usd"`
	PeriodRemainingUSD float64 `json:"period_remaining_usd"`
	StartTime          int64   `json:"start_time"`
	EndTime            int64   `json:"end_time"`
	NextResetTime      int64   `json:"next_reset_time"`
}

type DesktopUsageSummary struct {
	AvailableModels []string `json:"available_models"`
	TodayUSD        float64  `json:"today_usd"`
	Last7DaysUSD    float64  `json:"last_7_days_usd"`
	LastRequestAt   int64    `json:"last_request_at"`
}

type DesktopServiceSummary struct {
	Status            string   `json:"status"`
	Notice            string   `json:"notice"`
	Maintenance       bool     `json:"maintenance"`
	RecommendedAction string   `json:"recommended_action"`
	AffectedScopes    []string `json:"affected_scopes"`
}

type DesktopQuickActions struct {
	ServerAddress string `json:"server_address"`
	TopUpLink     string `json:"topup_link"`
	TokensPath    string `json:"tokens_path"`
	LogsPath      string `json:"logs_path"`
}

type DesktopAccountSummaryResponse struct {
	Account       DesktopAccountSnapshot        `json:"account"`
	Subscriptions []DesktopSubscriptionSnapshot `json:"subscriptions"`
	Tokens        DesktopTokenSummary           `json:"tokens"`
	Usage         DesktopUsageSummary           `json:"usage"`
	Service       DesktopServiceSummary         `json:"service"`
	RecentLogs    []*auditschema.Log            `json:"recent_logs"`
	Actions       DesktopQuickActions           `json:"actions"`
}

type DesktopUsageTrendItem struct {
	Date      string  `json:"date"`
	Timestamp int64   `json:"timestamp"`
	Requests  int64   `json:"requests"`
	Quota     int64   `json:"quota"`
	TokenUsed int64   `json:"token_used"`
	QuotaUSD  float64 `json:"quota_usd"`
}

type DesktopUsageTrendResponse struct {
	Days  int                     `json:"days"`
	Trend []DesktopUsageTrendItem `json:"trend"`
}

// BuildDesktopAccountSummary aggregates desktop dashboard account data.
func BuildDesktopAccountSummary(userID int) (*DesktopAccountSummaryResponse, error) {
	user, err := LoadUserByID(userID, false)
	if err != nil {
		return nil, err
	}
	walletQuota, err := loadDisplayWalletQuota(user)
	if err != nil {
		return nil, err
	}

	recentLogs, _, err := auditapp.ListUserLogs(userID, auditapp.LogListQuery{
		LogType:  auditschema.LogTypeUnknown,
		PageSize: 10,
	})
	if err != nil {
		return nil, err
	}

	tokenCount, err := identitystore.CountUserTokens(userID)
	if err != nil {
		return nil, err
	}

	defaultDesktopToken, err := FindDesktopToken(userID, BuildDesktopTokenName("Default"))
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		defaultDesktopToken = nil
	}

	setting := identitydomain.GetSetting(user)
	serverAddress := NormalizeDesktopServerAddress("")
	usageSummary := summarizeDesktopUsage(recentLogs)
	usageSummary.AvailableModels = ListDesktopAvailableModels(user.Group)
	subscriptions, err := buildDesktopSubscriptionSnapshots(userID)
	if err != nil {
		return nil, err
	}

	return &DesktopAccountSummaryResponse{
		Account: DesktopAccountSnapshot{
			ID:                 user.Id,
			Username:           user.Username,
			DisplayName:        user.DisplayName,
			Group:              user.Group,
			Quota:              walletQuota,
			UsedQuota:          user.UsedQuota,
			RequestCount:       user.RequestCount,
			QuotaUSD:           quotaToUSD(walletQuota),
			UsedQuotaUSD:       quotaToUSD(user.UsedQuota),
			BillingPreference:  setting.BillingPreference,
			FundingSourceOrder: setting.FundingSourceOrder,
		},
		Subscriptions: subscriptions,
		Tokens: DesktopTokenSummary{
			Total:        int(tokenCount),
			DesktopToken: BuildMaskedTokenResponse(defaultDesktopToken),
		},
		Usage:      usageSummary,
		Service:    DesktopServiceStatusSummary(),
		RecentLogs: recentLogs,
		Actions: DesktopQuickActions{
			ServerAddress: serverAddress,
			TopUpLink:     platformconfig.TopUpLink,
			TokensPath:    "/tokens",
			LogsPath:      "/usage-logs",
		},
	}, nil
}

func buildDesktopSubscriptionSnapshots(userID int) ([]DesktopSubscriptionSnapshot, error) {
	activeSubscriptions, err := commerceapp.GetAllActiveUserSubscriptions(userID)
	if err != nil {
		return nil, err
	}

	result := make([]DesktopSubscriptionSnapshot, 0, len(activeSubscriptions))
	for _, item := range activeSubscriptions {
		subscription := item.Subscription
		if subscription == nil {
			continue
		}

		planTitle := fmt.Sprintf("Plan #%d", subscription.PlanId)
		if plan, planErr := commerceapp.GetSubscriptionPlanByID(subscription.PlanId); planErr == nil && plan != nil {
			if trimmedTitle := strings.TrimSpace(plan.Title); trimmedTitle != "" {
				planTitle = trimmedTitle
			}
		}

		amountTotal := subscription.AmountTotal
		amountUsed := subscription.AmountUsed
		remaining := amountTotal - amountUsed
		if remaining < 0 {
			remaining = 0
		}
		periodAmount := subscription.PeriodAmount
		periodUsed := subscription.PeriodUsed
		periodRemaining := periodAmount - periodUsed
		if periodRemaining < 0 {
			periodRemaining = 0
		}

		result = append(result, DesktopSubscriptionSnapshot{
			ID:                 subscription.Id,
			PlanID:             subscription.PlanId,
			PlanTitle:          planTitle,
			AmountTotalUSD:     quotaToUSD(int(amountTotal)),
			AmountUsedUSD:      quotaToUSD(int(amountUsed)),
			RemainingUSD:       quotaToUSD(int(remaining)),
			Unlimited:          amountTotal <= 0,
			PeriodAmountUSD:    quotaToUSD(int(periodAmount)),
			PeriodUsedUSD:      quotaToUSD(int(periodUsed)),
			PeriodRemainingUSD: quotaToUSD(int(periodRemaining)),
			StartTime:          subscription.StartTime,
			EndTime:            subscription.EndTime,
			NextResetTime:      subscription.NextResetTime,
		})
	}

	return result, nil
}

// BuildDesktopUsageLogsPage loads desktop usage logs with standard paging.
func BuildDesktopUsageLogsPage(userID int, pageInfo *platformpagination.PageInfo, logType int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, group string, requestID string, upstreamRequestID string) (*platformpagination.PageInfo, error) {
	logs, total, err := auditapp.ListUserLogs(userID, auditapp.LogListQuery{
		LogType:           logType,
		StartTimestamp:    startTimestamp,
		EndTimestamp:      endTimestamp,
		ModelName:         modelName,
		TokenName:         tokenName,
		Group:             group,
		RequestID:         requestID,
		UpstreamRequestID: upstreamRequestID,
		StartIdx:          pageInfo.GetStartIdx(),
		PageSize:          pageInfo.GetPageSize(),
	})
	if err != nil {
		return nil, err
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	return pageInfo, nil
}

// BuildDesktopTokensPage returns the desktop token list with masked keys.
func BuildDesktopTokensPage(userID int, pageInfo *platformpagination.PageInfo) (*platformpagination.PageInfo, error) {
	tokens, _, err := ListUserTokens(userID, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		return nil, err
	}
	total, err := identitystore.CountUserTokens(userID)
	if err != nil {
		return nil, err
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(BuildMaskedTokenResponses(tokens))
	return pageInfo, nil
}

// GetDesktopGroupsForUser resolves desktop groups for the authenticated user.
func GetDesktopGroupsForUser(userID int) (DesktopGroupsResponse, error) {
	user, err := LoadUserByID(userID, false)
	if err != nil {
		return DesktopGroupsResponse{}, err
	}
	return ListDesktopGroups(user), nil
}

// BuildDesktopTokenConfigForUser resolves token config payload for a desktop user and token.
func BuildDesktopTokenConfigForUser(userID int, tokenID int) (*DesktopTokenConfigResponse, error) {
	token, err := FindDesktopTokenByID(userID, tokenID)
	if err != nil {
		return nil, err
	}
	user, err := LoadUserByID(userID, false)
	if err != nil {
		return nil, err
	}
	return BuildDesktopTokenConfigResponse(
		token,
		ListDesktopAvailableModelsForTokenGroup(user.Group, token.Group),
	)
}

// BuildDesktopUsageTrend builds a filled daily usage trend series.
func BuildDesktopUsageTrend(userID int, days int) (*DesktopUsageTrendResponse, error) {
	now := time.Now().In(time.Local)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	start := dayStart.AddDate(0, 0, -(days - 1))

	rows, err := auditapp.ListUserQuotaDates(userID, start.Unix(), now.Unix())
	if err != nil {
		return nil, err
	}

	result := make([]DesktopUsageTrendItem, 0, days)
	indexByDate := make(map[string]int, days)
	for i := 0; i < days; i++ {
		current := start.AddDate(0, 0, i)
		key := current.Format("2006-01-02")
		indexByDate[key] = len(result)
		result = append(result, DesktopUsageTrendItem{
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

	for index := range result {
		result[index].QuotaUSD = quotaToUSD(int(result[index].Quota))
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp < result[j].Timestamp
	})
	return &DesktopUsageTrendResponse{
		Days:  days,
		Trend: result,
	}, nil
}

// DesktopServiceStatusSummary builds the current desktop service status summary.
func DesktopServiceStatusSummary() DesktopServiceSummary {
	platformconfig.OptionMapRWMutex.RLock()
	defer platformconfig.OptionMapRWMutex.RUnlock()

	notice := strings.TrimSpace(platformconfig.OptionMap["Notice"])
	maintenance := DesktopServiceMaintenanceEnabled(platformconfig.OptionMap)
	status := "ok"
	recommendedAction := ""
	affectedScopes := []string{}

	if notice != "" {
		status = "notice"
		recommendedAction = "Review the latest service notice before retrying large or long-running coding sessions."
		affectedScopes = []string{"account", "logs", "tool-config"}
	}

	if maintenance {
		status = "maintenance"
		recommendedAction = "Wait for maintenance to finish or switch to another provider before continuing."
	}

	return DesktopServiceSummary{
		Status:            status,
		Notice:            notice,
		Maintenance:       maintenance,
		RecommendedAction: recommendedAction,
		AffectedScopes:    affectedScopes,
	}
}

func summarizeDesktopUsage(logs []*auditschema.Log) DesktopUsageSummary {
	summary := DesktopUsageSummary{
		AvailableModels: []string{},
	}
	if len(logs) == 0 {
		return summary
	}

	now := time.Now()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	startOfLast7Days := startOfToday - 6*24*60*60
	var todayQuota, last7Quota int64
	var lastRequestAt int64

	for _, item := range logs {
		if item == nil {
			continue
		}
		if item.CreatedAt > lastRequestAt {
			lastRequestAt = item.CreatedAt
		}
		if item.CreatedAt >= startOfToday {
			todayQuota += int64(item.Quota)
		}
		if item.CreatedAt >= startOfLast7Days {
			last7Quota += int64(item.Quota)
		}
	}

	summary.TodayUSD = quotaToUSD(int(todayQuota))
	summary.Last7DaysUSD = quotaToUSD(int(last7Quota))
	summary.LastRequestAt = lastRequestAt
	return summary
}
