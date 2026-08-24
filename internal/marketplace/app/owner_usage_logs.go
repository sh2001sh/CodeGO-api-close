package app

import (
	"errors"
	"strings"
	"time"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

type ownerUsageChannel struct {
	ChannelID         string
	GroupID           string
	Name              string
	InternalChannelID int
}

func ListOwnerUsageLogs(ownerUserID int, query OwnerUsageLogQuery) (*OwnerUsageLogResult, error) {
	query = normalizeOwnerUsageLogQuery(query)
	query, err := resolveOwnerUsageLogUsers(query)
	if err != nil {
		return nil, err
	}
	channels, err := loadOwnerUsageChannels(ownerUserID, query.ChannelID)
	if err != nil {
		return nil, err
	}
	result := &OwnerUsageLogResult{
		Items: []OwnerUsageLogItem{}, Page: query.Page, PageSize: query.PageSize,
	}
	if len(channels) == 0 {
		return result, nil
	}

	channelIDs := make([]int, 0, len(channels))
	groupIDs := make([]string, 0, len(channels))
	channelByInternalID := make(map[int]ownerUsageChannel, len(channels))
	for _, channel := range channels {
		channelIDs = append(channelIDs, channel.InternalChannelID)
		if channel.GroupID != "" {
			groupIDs = append(groupIDs, channel.GroupID)
		}
		channelByInternalID[channel.InternalChannelID] = channel
	}

	if err := ownerUsageLogDBQuery(channelIDs, query).Count(&result.Total).Error; err != nil {
		return nil, err
	}
	result.Summary, err = loadOwnerUsageSummary(ownerUserID, channelIDs, groupIDs, query)
	if err != nil {
		return nil, err
	}
	var logs []auditschema.Log
	if err := ownerUsageLogDBQuery(channelIDs, query).Order("id desc").
		Limit(query.PageSize).
		Offset((query.Page - 1) * query.PageSize).
		Find(&logs).Error; err != nil {
		return nil, err
	}

	settlements, err := loadOwnerUsageSettlements(ownerUserID, logs)
	if err != nil {
		return nil, err
	}
	externalUserIDs, err := loadExternalUserIDs(logs)
	if err != nil {
		return nil, err
	}
	result.Items = make([]OwnerUsageLogItem, 0, len(logs))
	for i := range logs {
		log := logs[i]
		channel := channelByInternalID[log.ChannelId]
		item := ownerUsageLogItem(log, channel, settlements[log.RequestId], externalUserIDs[log.UserId])
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func loadExternalUserIDs(logs []auditschema.Log) (map[int]string, error) {
	internalIDs := make([]int, 0, len(logs))
	seen := make(map[int]struct{}, len(logs))
	for i := range logs {
		userID := logs[i].UserId
		if userID <= 0 {
			continue
		}
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}
		internalIDs = append(internalIDs, userID)
	}
	result := make(map[int]string, len(internalIDs))
	if len(internalIDs) == 0 {
		return result, nil
	}
	var users []identityschema.User
	if err := platformdb.DB.Unscoped().Select("id", "external_id").Where("id IN ?", internalIDs).Find(&users).Error; err != nil {
		return nil, err
	}
	for i := range users {
		result[users[i].Id] = users[i].ExternalId
	}
	return result, nil
}

func ownerUsageLogDBQuery(channelIDs []int, query OwnerUsageLogQuery) *gorm.DB {
	db := platformdb.LogDB.Model(&auditschema.Log{}).
		Where("channel_id IN ?", channelIDs).
		Where("type IN ?", []int{auditschema.LogTypeConsume, auditschema.LogTypeError})
	if query.StartTimestamp > 0 {
		db = db.Where("created_at >= ?", query.StartTimestamp)
	}
	if query.EndTimestamp > 0 {
		db = db.Where("created_at <= ?", query.EndTimestamp)
	}
	if query.Status == "success" {
		db = db.Where("type = ?", auditschema.LogTypeConsume)
	} else if query.Status == "failed" {
		db = db.Where("type = ?", auditschema.LogTypeError)
	}
	if query.ModelName != "" {
		db = db.Where("LOWER(model_name) LIKE ? ESCAPE '!'", ownerLogLike(query.ModelName))
	}
	if query.RequestID != "" {
		db = db.Where("request_id = ?", query.RequestID)
	}
	if query.UpstreamRequestID != "" {
		db = db.Where("upstream_request_id = ?", query.UpstreamRequestID)
	}
	if query.ExternalUserID != "" {
		db = db.Where("user_id IN ?", query.userFilterIDs)
	}
	if query.Search != "" {
		pattern := ownerLogLike(query.Search)
		if len(query.searchUserIDs) > 0 {
			db = db.Where(
				"(LOWER(model_name) LIKE ? ESCAPE '!' OR LOWER(request_id) LIKE ? ESCAPE '!' OR LOWER(upstream_request_id) LIKE ? ESCAPE '!' OR LOWER(content) LIKE ? ESCAPE '!' OR LOWER(other) LIKE ? ESCAPE '!' OR user_id IN ?)",
				pattern, pattern, pattern, pattern, pattern, query.searchUserIDs,
			)
		} else {
			db = db.Where(
				"(LOWER(model_name) LIKE ? ESCAPE '!' OR LOWER(request_id) LIKE ? ESCAPE '!' OR LOWER(upstream_request_id) LIKE ? ESCAPE '!' OR LOWER(content) LIKE ? ESCAPE '!' OR LOWER(other) LIKE ? ESCAPE '!')",
				pattern, pattern, pattern, pattern, pattern,
			)
		}
	}
	return db
}

func loadOwnerUsageSummary(ownerUserID int, channelIDs []int, groupIDs []string, query OwnerUsageLogQuery) (OwnerUsageLogSummary, error) {
	var summary OwnerUsageLogSummary
	if err := ownerUsageLogDBQuery(channelIDs, query).Select(
		"COUNT(*) AS request_count, "+
			"SUM(CASE WHEN type = ? THEN 1 ELSE 0 END) AS success_count, "+
			"SUM(CASE WHEN type = ? THEN 1 ELSE 0 END) AS failed_count",
		auditschema.LogTypeConsume, auditschema.LogTypeError,
	).Scan(&summary).Error; err != nil {
		return summary, err
	}
	if len(groupIDs) == 0 {
		return summary, nil
	}
	totals, err := loadOwnerUsageSettlementSummary(ownerUserID, channelIDs, groupIDs, query)
	if err != nil {
		return summary, err
	}
	summary.ConsumerAmount = totals.ConsumerAmount
	summary.OwnerIncome = totals.OwnerIncome
	summary.PendingIncome = totals.PendingIncome
	summary.ReleasedIncome = totals.ReleasedIncome
	return summary, nil
}

type ownerSettlementTotals struct {
	ConsumerAmount int64 `gorm:"column:consumer_amount"`
	OwnerIncome    int64 `gorm:"column:owner_income"`
	PendingIncome  int64 `gorm:"column:pending_income"`
	ReleasedIncome int64 `gorm:"column:released_income"`
}

func loadOwnerUsageSettlementSummary(ownerUserID int, channelIDs []int, groupIDs []string, query OwnerUsageLogQuery) (ownerSettlementTotals, error) {
	var totals ownerSettlementTotals
	settlementQuery := platformdb.DB.Model(&marketplaceschema.Settlement{}).
		Where("owner_user_id = ? AND group_id IN ?", ownerUserID, groupIDs).
		Select("COALESCE(SUM(consumer_amount), 0) AS consumer_amount, COALESCE(SUM(owner_net_amount), 0) AS owner_income, " +
			"COALESCE(SUM(CASE WHEN status = 'pending' THEN owner_net_amount ELSE 0 END), 0) AS pending_income, " +
			"COALESCE(SUM(CASE WHEN status = 'released' THEN owner_net_amount ELSE 0 END), 0) AS released_income")
	if hasOwnerUsageContentFilters(query) {
		var requestIDs []string
		if err := ownerUsageLogDBQuery(channelIDs, query).
			Where("type = ? AND request_id <> ''", auditschema.LogTypeConsume).
			Pluck("request_id", &requestIDs).Error; err != nil {
			return totals, err
		}
		if len(requestIDs) == 0 {
			return totals, nil
		}
		settlementQuery = settlementQuery.Where("request_id IN ?", requestIDs)
	}
	if query.StartTimestamp > 0 {
		settlementQuery = settlementQuery.Where("created_at >= ?", time.Unix(query.StartTimestamp, 0))
	}
	if query.EndTimestamp > 0 {
		settlementQuery = settlementQuery.Where("created_at < ?", time.Unix(query.EndTimestamp+1, 0))
	}
	return totals, settlementQuery.Scan(&totals).Error
}

func normalizeOwnerUsageLogQuery(query OwnerUsageLogQuery) OwnerUsageLogQuery {
	query.ChannelID = strings.TrimSpace(query.ChannelID)
	query.Status = strings.ToLower(strings.TrimSpace(query.Status))
	query.ModelName = strings.TrimSpace(query.ModelName)
	query.RequestID = strings.TrimSpace(query.RequestID)
	query.UpstreamRequestID = strings.TrimSpace(query.UpstreamRequestID)
	query.ExternalUserID = strings.TrimSpace(query.ExternalUserID)
	query.Search = strings.TrimSpace(query.Search)
	if query.Status != "success" && query.Status != "failed" {
		query.Status = ""
	}
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = 20
	} else if query.PageSize > 100 {
		query.PageSize = 100
	}
	if query.StartTimestamp > 0 && query.EndTimestamp > 0 && query.StartTimestamp > query.EndTimestamp {
		query.StartTimestamp, query.EndTimestamp = query.EndTimestamp, query.StartTimestamp
	}
	return query
}

func resolveOwnerUsageLogUsers(query OwnerUsageLogQuery) (OwnerUsageLogQuery, error) {
	load := func(value string) ([]int, error) {
		if value == "" {
			return nil, nil
		}
		var ids []int
		err := platformdb.DB.Unscoped().Model(&identityschema.User{}).
			Where("LOWER(external_id) LIKE ? ESCAPE '!'", ownerLogLike(value)).Limit(1000).Pluck("id", &ids).Error
		return ids, err
	}
	var err error
	query.userFilterIDs, err = load(query.ExternalUserID)
	if err != nil {
		return query, err
	}
	if query.ExternalUserID != "" && len(query.userFilterIDs) == 0 {
		query.userFilterIDs = []int{-1}
	}
	query.searchUserIDs, err = load(query.Search)
	return query, err
}

func ownerLogLike(value string) string {
	replacer := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")
	return "%" + replacer.Replace(strings.ToLower(strings.TrimSpace(value))) + "%"
}

func hasOwnerUsageContentFilters(query OwnerUsageLogQuery) bool {
	return query.Status != "" || query.ModelName != "" || query.RequestID != "" ||
		query.UpstreamRequestID != "" || query.ExternalUserID != "" || query.Search != ""
}

func loadOwnerUsageChannels(ownerUserID int, selectedChannelID string) ([]ownerUsageChannel, error) {
	var channels []marketplaceschema.Channel
	db := platformdb.DB.Where("owner_user_id = ?", ownerUserID)
	if selectedChannelID != "" {
		db = db.Where("id = ?", selectedChannelID)
	}
	if err := db.Find(&channels).Error; err != nil {
		return nil, err
	}
	if selectedChannelID != "" && len(channels) == 0 {
		return nil, errors.New("渠道不存在或无权访问")
	}

	channelIDs := make([]string, 0, len(channels))
	for i := range channels {
		channelIDs = append(channelIDs, channels[i].ID)
	}
	var groups []marketplaceschema.Group
	if len(channelIDs) > 0 {
		if err := platformdb.DB.Where("channel_id IN ?", channelIDs).Find(&groups).Error; err != nil {
			return nil, err
		}
	}
	groupByChannelID := make(map[string]marketplaceschema.Group, len(groups))
	for i := range groups {
		groupByChannelID[groups[i].ChannelID] = groups[i]
	}

	result := make([]ownerUsageChannel, 0, len(channels))
	for i := range channels {
		channel := channels[i]
		if channel.InternalChannelID == nil || *channel.InternalChannelID <= 0 {
			continue
		}
		group := groupByChannelID[channel.ID]
		result = append(result, ownerUsageChannel{
			ChannelID: channel.ID, GroupID: group.ID,
			Name:              marketplaceDisplayName(channel.SubmittedSourceLabel, group.Multiplier, channel.ID),
			InternalChannelID: *channel.InternalChannelID,
		})
	}
	return result, nil
}

func loadOwnerUsageSettlements(ownerUserID int, logs []auditschema.Log) (map[string]marketplaceschema.Settlement, error) {
	requestIDs := make([]string, 0, len(logs))
	for i := range logs {
		if logs[i].RequestId != "" {
			requestIDs = append(requestIDs, logs[i].RequestId)
		}
	}
	result := make(map[string]marketplaceschema.Settlement, len(requestIDs))
	if len(requestIDs) == 0 {
		return result, nil
	}
	var settlements []marketplaceschema.Settlement
	if err := platformdb.DB.Where("owner_user_id = ? AND request_id IN ?", ownerUserID, requestIDs).
		Find(&settlements).Error; err != nil {
		return nil, err
	}
	for i := range settlements {
		result[settlements[i].RequestID] = settlements[i]
	}
	return result, nil
}

func ownerUsageLogItem(log auditschema.Log, channel ownerUsageChannel, settlement marketplaceschema.Settlement, externalUserID string) OwnerUsageLogItem {
	status := "success"
	if log.Type == auditschema.LogTypeError {
		status = "failed"
	}
	item := OwnerUsageLogItem{
		ID: log.Id, ChannelID: channel.ChannelID, ChannelName: channel.Name, GroupID: channel.GroupID,
		UserID: externalUserID, CreatedAt: log.CreatedAt, Status: status, ModelName: log.ModelName,
		PromptTokens: log.PromptTokens, CompletionTokens: log.CompletionTokens, UseTime: log.UseTime,
		IsStream: log.IsStream, RequestID: log.RequestId, ConsumerAmount: int64(log.Quota),
		IncomeStatus: "none",
	}
	applyOwnerUsageLogDetails(&item, log)
	if settlement.ID != "" {
		item.ConsumerAmount = settlement.ConsumerAmount
		item.OwnerIncome = settlement.OwnerNetAmount
		item.PlatformCommission = settlement.PlatformCommission
		item.Multiplier = settlement.Multiplier
		item.IncomeStatus = settlement.Status
		item.AvailableAt = &settlement.AvailableAt
		item.ReleasedAt = settlement.ReleasedAt
	}
	return item
}
