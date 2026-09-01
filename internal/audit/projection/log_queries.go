package projection

import (
	"context"
	"errors"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"time"

	auditdomain "github.com/sh2001sh/new-api/internal/audit/domain"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/types"
)

const logSearchCountLimit = 10000
const logGroupOptionLimit = 200

func GetLogByTokenID(tokenID int) ([]*auditschema.Log, error) {
	var logs []*auditschema.Log
	err := platformdb.LogDB.Model(&auditschema.Log{}).
		Where("token_id = ?", tokenID).
		Order("id desc").
		Limit(platformconfig.MaxRecentItems).
		Find(&logs).
		Error
	formatUserLogs(logs, 0)
	return logs, err
}

func ListAdminLogs(query auditdomain.LogListQuery) ([]*auditschema.Log, int64, error) {
	var (
		logs  []*auditschema.Log
		total int64
	)

	tx := platformdb.LogDB
	if query.LogType != auditschema.LogTypeUnknown {
		tx = tx.Where("logs.type = ?", query.LogType)
	}
	if query.UserID > 0 {
		tx = tx.Where("logs.user_id = ?", query.UserID)
	}

	tx = applyLogContainsFilter(tx, "logs.model_name", query.ModelName)
	tx = applyLogExactFilter(tx, "logs.username", query.Username)
	tx = applyLogContainsFilter(tx, "logs.token_name", query.TokenName)
	if query.RequestID != "" {
		tx = tx.Where("logs.request_id = ?", query.RequestID)
	}
	if query.UpstreamRequestID != "" {
		tx = tx.Where("logs.upstream_request_id = ?", query.UpstreamRequestID)
	}
	if query.StartTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", query.StartTimestamp)
	}
	if query.EndTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", query.EndTimestamp)
	}
	if query.Channel != 0 {
		tx = tx.Where("logs.channel_id = ?", query.Channel)
	}
	tx = applyLogContainsFilter(tx, "logs."+logGroupColumn(), query.Group)
	if err := tx.Model(&auditschema.Log{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := tx.Order("logs.id desc").Limit(query.PageSize).Offset(query.StartIdx).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	if err := attachChannelNames(logs); err != nil {
		return logs, total, err
	}
	return logs, total, nil
}

func ListUserLogs(userID int, query auditdomain.LogListQuery) ([]*auditschema.Log, int64, error) {
	var (
		logs  []*auditschema.Log
		total int64
	)

	tx := platformdb.LogDB.Where("logs.user_id = ?", userID)
	if query.LogType != auditschema.LogTypeUnknown {
		tx = tx.Where("logs.type = ?", query.LogType)
	}

	tx = applyLogContainsFilter(tx, "logs.model_name", query.ModelName)
	tx = applyLogContainsFilter(tx, "logs.token_name", query.TokenName)
	if query.RequestID != "" {
		tx = tx.Where("logs.request_id = ?", query.RequestID)
	}
	if query.UpstreamRequestID != "" {
		tx = tx.Where("logs.upstream_request_id = ?", query.UpstreamRequestID)
	}
	if query.StartTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", query.StartTimestamp)
	}
	if query.EndTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", query.EndTimestamp)
	}
	tx = applyLogContainsFilter(tx, "logs."+logGroupColumn(), query.Group)
	if err := tx.Model(&auditschema.Log{}).Limit(logSearchCountLimit).Count(&total).Error; err != nil {
		platformobservability.SysError("failed to count user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}
	if err := tx.Order("logs.id desc").Limit(query.PageSize).Offset(query.StartIdx).Find(&logs).Error; err != nil {
		platformobservability.SysError("failed to search user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}

	formatUserLogs(logs, query.StartIdx)
	return logs, total, nil
}

func SumUsedQuota(query auditdomain.LogListQuery) (auditschema.Stat, error) {
	stat := auditschema.Stat{}
	tx := platformdb.LogDB.Table("logs").Select("sum(quota) quota")
	rpmTpmQuery := platformdb.LogDB.Table("logs").Select("count(*) rpm, sum(prompt_tokens) + sum(completion_tokens) tpm")

	if query.UserID > 0 {
		tx = tx.Where("user_id = ?", query.UserID)
		rpmTpmQuery = rpmTpmQuery.Where("user_id = ?", query.UserID)
	}
	tx = applyLogExactFilter(tx, "username", query.Username)
	rpmTpmQuery = applyLogExactFilter(rpmTpmQuery, "username", query.Username)
	tx = applyLogContainsFilter(tx, "token_name", query.TokenName)
	rpmTpmQuery = applyLogContainsFilter(rpmTpmQuery, "token_name", query.TokenName)
	if query.StartTimestamp != 0 {
		tx = tx.Where("created_at >= ?", query.StartTimestamp)
	}
	if query.EndTimestamp != 0 {
		tx = tx.Where("created_at <= ?", query.EndTimestamp)
	}
	tx = applyLogContainsFilter(tx, "model_name", query.ModelName)
	rpmTpmQuery = applyLogContainsFilter(rpmTpmQuery, "model_name", query.ModelName)
	if query.Channel != 0 {
		tx = tx.Where("channel_id = ?", query.Channel)
		rpmTpmQuery = rpmTpmQuery.Where("channel_id = ?", query.Channel)
	}
	groupCol := logGroupColumn()
	tx = applyLogContainsFilter(tx, groupCol, query.Group)
	rpmTpmQuery = applyLogContainsFilter(rpmTpmQuery, groupCol, query.Group)

	tx = tx.Where("type = ?", auditschema.LogTypeConsume)
	rpmTpmQuery = rpmTpmQuery.Where("type = ?", auditschema.LogTypeConsume)
	rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())

	if err := tx.Scan(&stat).Error; err != nil {
		platformobservability.SysError("failed to query log stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	var realtimeStat struct {
		Rpm int `gorm:"column:rpm"`
		Tpm int `gorm:"column:tpm"`
	}
	if err := rpmTpmQuery.Scan(&realtimeStat).Error; err != nil {
		platformobservability.SysError("failed to query rpm/tpm stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	stat.Rpm = realtimeStat.Rpm
	stat.Tpm = realtimeStat.Tpm
	return stat, nil
}

// ListUsedLogGroups returns distinct groups visible to an administrator or user.
func ListUsedLogGroups(userID int) ([]string, error) {
	groups := make([]string, 0)
	groupCol := logGroupColumn()
	query := platformdb.LogDB.Table("logs").
		Distinct(groupCol).
		Where(groupCol + " <> ''")
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	err := query.Order(groupCol+" ASC").
		Limit(logGroupOptionLimit).
		Pluck(groupCol, &groups).Error
	return groups, err
}

func DeleteOldLog(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	var total int64
	for {
		if ctx.Err() != nil {
			return total, ctx.Err()
		}
		result := platformdb.LogDB.Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&auditschema.Log{})
		if result.Error != nil {
			return total, result.Error
		}
		total += result.RowsAffected
		if result.RowsAffected < int64(limit) {
			break
		}
	}
	return total, nil
}

func attachChannelNames(logs []*auditschema.Log) error {
	channelIDs := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIDs.Add(log.ChannelId)
		}
	}
	if channelIDs.Len() == 0 {
		return nil
	}

	type channelRow struct {
		ID   int    `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	channels := make([]channelRow, 0, channelIDs.Len())
	if platformconfig.MemoryCacheEnabled {
		for _, channelID := range channelIDs.Items() {
			cacheChannel, err := gatewaystore.GetCachedChannel(channelID)
			if err != nil {
				continue
			}
			channels = append(channels, channelRow{ID: channelID, Name: cacheChannel.Name})
		}
	} else {
		if err := platformdb.DB.Table("channels").Select("id, name").Where("id IN ?", channelIDs.Items()).Find(&channels).Error; err != nil {
			return err
		}
	}

	channelMap := make(map[int]string, len(channels))
	for _, channel := range channels {
		channelMap[channel.ID] = channel.Name
	}
	for i := range logs {
		logs[i].ChannelName = channelMap[logs[i].ChannelId]
	}
	return nil
}
