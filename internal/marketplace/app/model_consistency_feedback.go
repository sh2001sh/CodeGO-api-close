package app

import (
	"errors"
	"strings"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm/clause"
)

func SubmitModelConsistencyFeedback(userID int, groupID string, req ModelConsistencyFeedbackRequest) (*ModelConsistencyFeedbackSummary, error) {
	if userID <= 0 || strings.TrimSpace(groupID) == "" {
		return nil, errors.New("模型一致性反馈参数无效")
	}
	status, err := normalizeFeedbackStatus(req.Status)
	if err != nil {
		return nil, err
	}

	var group marketplaceschema.Group
	if err := platformdb.DB.Where("id = ?", strings.TrimSpace(groupID)).First(&group).Error; err != nil {
		return nil, err
	}
	if group.Visibility != marketplacedomain.VisibilityPublic || !marketplacedomain.AcceptsTraffic(group.LifecycleStatus) {
		return nil, errors.New("当前分组暂不接受模型一致性反馈")
	}
	var channel marketplaceschema.Channel
	if err := platformdb.DB.Where("id = ?", group.ChannelID).First(&channel).Error; err != nil {
		return nil, err
	}
	if channel.OwnerUserID == userID {
		return nil, errors.New("渠道主不能评价自己渠道的模型一致性")
	}
	model, ok := canonicalDeclaredModel(channel.DeclaredModels, req.Model)
	if !ok {
		return nil, errors.New("该模型不在当前分组的可用模型列表中")
	}

	feedback := marketplaceschema.ModelConsistencyFeedback{
		ChannelID: channel.ID, UserID: userID, Model: model, Status: status,
	}
	err = platformdb.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}, {Name: "user_id"}, {Name: "model"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "updated_at"}),
	}).Create(&feedback).Error
	if err != nil {
		return nil, err
	}
	return loadModelFeedbackSummary(channel.ID, model, userID)
}

func attachModelConsistencyFeedback(items []GroupListItem, channels map[string]marketplaceschema.Channel, viewerUserID int) error {
	channelIDs := make([]string, 0, len(items))
	for index := range items {
		channelIDs = append(channelIDs, items[index].ChannelID)
	}
	summaries, err := loadModelFeedbackSummaries(channelIDs, viewerUserID)
	if err != nil {
		return err
	}
	for index := range items {
		channel := channels[items[index].ChannelID]
		items[index].CanSubmitModelFeedback = viewerUserID > 0 && channel.OwnerUserID != viewerUserID
		switch {
		case viewerUserID <= 0:
			items[index].ModelFeedbackPermission = "login_required"
		case channel.OwnerUserID == viewerUserID:
			items[index].ModelFeedbackPermission = "owner"
		default:
			items[index].ModelFeedbackPermission = "allowed"
		}
		items[index].ModelConsistencyFeedback = modelFeedbackForModels(
			items[index].Models, summaries[items[index].ChannelID],
		)
	}
	return nil
}

func loadModelFeedbackSummaries(channelIDs []string, viewerUserID int) (map[string]map[string]ModelConsistencyFeedbackSummary, error) {
	result := make(map[string]map[string]ModelConsistencyFeedbackSummary, len(channelIDs))
	if len(channelIDs) == 0 {
		return result, nil
	}
	type countRow struct {
		ChannelID string
		Model     string
		Status    string
		Count     int64
	}
	var counts []countRow
	if err := platformdb.DB.Model(&marketplaceschema.ModelConsistencyFeedback{}).
		Select("channel_id, model, status, COUNT(*) AS count").
		Where("channel_id IN ?", channelIDs).
		Group("channel_id, model, status").Scan(&counts).Error; err != nil {
		return nil, err
	}
	for _, row := range counts {
		key := strings.ToLower(row.Model)
		if result[row.ChannelID] == nil {
			result[row.ChannelID] = make(map[string]ModelConsistencyFeedbackSummary)
		}
		summary := result[row.ChannelID][key]
		summary.Model, summary.Total = row.Model, summary.Total+row.Count
		applyFeedbackCount(&summary, row.Status, row.Count)
		result[row.ChannelID][key] = summary
	}
	if viewerUserID <= 0 {
		return result, nil
	}
	var viewerFeedback []marketplaceschema.ModelConsistencyFeedback
	if err := platformdb.DB.Select("channel_id", "model", "status").
		Where("channel_id IN ? AND user_id = ?", channelIDs, viewerUserID).
		Find(&viewerFeedback).Error; err != nil {
		return nil, err
	}
	for _, item := range viewerFeedback {
		key := strings.ToLower(item.Model)
		if result[item.ChannelID] == nil {
			result[item.ChannelID] = make(map[string]ModelConsistencyFeedbackSummary)
		}
		summary := result[item.ChannelID][key]
		summary.Model, summary.ViewerStatus = item.Model, item.Status
		result[item.ChannelID][key] = summary
	}
	return result, nil
}

func loadModelFeedbackSummary(channelID, model string, viewerUserID int) (*ModelConsistencyFeedbackSummary, error) {
	summaries, err := loadModelFeedbackSummaries([]string{channelID}, viewerUserID)
	if err != nil {
		return nil, err
	}
	result := modelFeedbackForModels([]string{model}, summaries[channelID])
	return &result[0], nil
}

func modelFeedbackForModels(models []string, summaries map[string]ModelConsistencyFeedbackSummary) []ModelConsistencyFeedbackSummary {
	result := make([]ModelConsistencyFeedbackSummary, 0, len(models))
	for _, model := range models {
		summary := summaries[strings.ToLower(model)]
		summary.Model = model
		result = append(result, summary)
	}
	return result
}

func applyFeedbackCount(summary *ModelConsistencyFeedbackSummary, status string, count int64) {
	switch status {
	case marketplacedomain.ModelConsistencyPassed:
		summary.Passed += count
	case marketplacedomain.ModelConsistencyFailed:
		summary.Failed += count
	case marketplacedomain.ModelConsistencyQuestioned:
		summary.Questionable += count
	}
}

func canonicalDeclaredModel(raw, requested string) (string, bool) {
	requested = strings.TrimSpace(requested)
	for _, model := range decodeModels(raw) {
		if strings.EqualFold(model, requested) {
			return model, true
		}
	}
	return "", false
}

func normalizeFeedbackStatus(status string) (string, error) {
	status = strings.TrimSpace(status)
	switch status {
	case marketplacedomain.ModelConsistencyPassed,
		marketplacedomain.ModelConsistencyFailed,
		marketplacedomain.ModelConsistencyQuestioned:
		return status, nil
	default:
		return "", errors.New("模型一致性反馈只能选择通过、不通过或存疑")
	}
}
