package app

import (
	"errors"
	"strings"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm/clause"
)

func SubmitChannelFeedback(userID int, groupID string, req ChannelFeedbackRequest) (*ChannelFeedbackSummary, error) {
	if userID <= 0 || strings.TrimSpace(groupID) == "" {
		return nil, errors.New("渠道反馈参数无效")
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
		return nil, errors.New("当前分组暂不接受渠道反馈")
	}
	if group.OwnerUserID == userID {
		return nil, errors.New("渠道主不能评价自己的渠道")
	}
	feedback := marketplaceschema.ChannelFeedback{ChannelID: group.ChannelID, UserID: userID, Status: status}
	if err := platformdb.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "updated_at"}),
	}).Create(&feedback).Error; err != nil {
		return nil, err
	}
	summary, err := loadChannelFeedbackSummaries([]string{group.ChannelID}, userID)
	if err != nil {
		return nil, err
	}
	result := summary[group.ChannelID]
	return &result, nil
}

func attachChannelFeedback(items []GroupListItem, channels map[string]marketplaceschema.Channel, viewerUserID int) error {
	ids := make([]string, 0, len(items))
	for index := range items {
		ids = append(ids, items[index].ChannelID)
	}
	summaries, err := loadChannelFeedbackSummaries(ids, viewerUserID)
	if err != nil {
		return err
	}
	for index := range items {
		channel := channels[items[index].ChannelID]
		items[index].ChannelFeedback = summaries[items[index].ChannelID]
		items[index].CanSubmitChannelFeedback = viewerUserID > 0 && channel.OwnerUserID != viewerUserID
		switch {
		case viewerUserID <= 0:
			items[index].ChannelFeedbackPermission = "login_required"
		case channel.OwnerUserID == viewerUserID:
			items[index].ChannelFeedbackPermission = "owner"
		default:
			items[index].ChannelFeedbackPermission = "allowed"
		}
	}
	return nil
}

func loadChannelFeedbackSummaries(channelIDs []string, viewerUserID int) (map[string]ChannelFeedbackSummary, error) {
	result := make(map[string]ChannelFeedbackSummary, len(channelIDs))
	if len(channelIDs) == 0 {
		return result, nil
	}
	type countRow struct {
		ChannelID, Status string
		Count             int64
	}
	var counts []countRow
	if err := platformdb.DB.Model(&marketplaceschema.ChannelFeedback{}).
		Select("channel_id, status, COUNT(*) AS count").Where("channel_id IN ?", channelIDs).
		Group("channel_id, status").Scan(&counts).Error; err != nil {
		return nil, err
	}
	for _, row := range counts {
		summary := result[row.ChannelID]
		summary.Total += row.Count
		applyFeedbackCount(&summary, row.Status, row.Count)
		result[row.ChannelID] = summary
	}
	if viewerUserID > 0 {
		var feedback []marketplaceschema.ChannelFeedback
		if err := platformdb.DB.Select("channel_id", "status").Where("channel_id IN ? AND user_id = ?", channelIDs, viewerUserID).Find(&feedback).Error; err != nil {
			return nil, err
		}
		for _, item := range feedback {
			summary := result[item.ChannelID]
			summary.ViewerStatus = item.Status
			result[item.ChannelID] = summary
		}
	}
	return result, nil
}

func applyFeedbackCount(summary *ChannelFeedbackSummary, status string, count int64) {
	switch status {
	case marketplacedomain.ModelConsistencyPassed:
		summary.Passed += count
	case marketplacedomain.ModelConsistencyFailed:
		summary.Failed += count
	case marketplacedomain.ModelConsistencyQuestioned:
		summary.Questionable += count
	}
}

func normalizeFeedbackStatus(status string) (string, error) {
	switch strings.TrimSpace(status) {
	case marketplacedomain.ModelConsistencyPassed, marketplacedomain.ModelConsistencyFailed, marketplacedomain.ModelConsistencyQuestioned:
		return strings.TrimSpace(status), nil
	default:
		return "", errors.New("渠道反馈只能选择通过、不通过或存疑")
	}
}
