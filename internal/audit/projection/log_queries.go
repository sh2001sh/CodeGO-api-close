package projection

import (
	"context"
	"errors"
	"fmt"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"strings"
	"sync"
	"time"

	auditdomain "github.com/sh2001sh/new-api/internal/audit/domain"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/types"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

const logSearchCountLimit = 10000
const logGroupOptionLimit = 200

var usedLogGroupsCache struct {
	sync.RWMutex
	items map[int]cachedLogGroups
}

type cachedLogGroups struct {
	groups []string
	at     time.Time
	db     *gorm.DB
}

const usedLogGroupsCacheTTL = 5 * time.Minute

var usedLogGroupsLoads singleflight.Group

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
	startedAt := time.Now()
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
	countStartedAt := time.Now()
	countRows := tx.Session(&gorm.Session{}).Model(&auditschema.Log{}).Select("1").Limit(logSearchCountLimit)
	if err := platformdb.LogDB.Table("(?) AS limited_logs", countRows).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	countElapsed := time.Since(countStartedAt)
	pageStartedAt := time.Now()
	if err := tx.Order("logs.id desc").Limit(query.PageSize).Offset(query.StartIdx).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	pageElapsed := time.Since(pageStartedAt)
	enrichStartedAt := time.Now()
	if err := attachChannelNames(logs); err != nil {
		return logs, total, err
	}
	logSlowLogQuery("admin", query, len(logs), countElapsed, pageElapsed, time.Since(enrichStartedAt), time.Since(startedAt))
	return logs, total, nil
}

func ListUserLogs(userID int, query auditdomain.LogListQuery) ([]*auditschema.Log, int64, error) {
	startedAt := time.Now()
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
	// LIMIT on COUNT(*) limits the one aggregate row, not the scanned logs.
	// Bound the input relation instead, matching the existing search limit.
	countStartedAt := time.Now()
	countRows := tx.Session(&gorm.Session{}).Model(&auditschema.Log{}).Select("1").Limit(logSearchCountLimit)
	if err := platformdb.LogDB.Table("(?) AS limited_logs", countRows).Count(&total).Error; err != nil {
		platformobservability.SysError("failed to count user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}
	countElapsed := time.Since(countStartedAt)
	pageStartedAt := time.Now()
	if err := tx.Order("logs.id desc").Limit(query.PageSize).Offset(query.StartIdx).Find(&logs).Error; err != nil {
		platformobservability.SysError("failed to search user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}
	pageElapsed := time.Since(pageStartedAt)

	formatUserLogs(logs, query.StartIdx)
	logSlowLogQuery("user", query, len(logs), countElapsed, pageElapsed, 0, time.Since(startedAt))
	return logs, total, nil
}

func logSlowLogQuery(scope string, query auditdomain.LogListQuery, rows int, countElapsed, pageElapsed, enrichElapsed, totalElapsed time.Duration) {
	if totalElapsed < 500*time.Millisecond {
		return
	}
	filtersPresent := query.LogType != auditschema.LogTypeUnknown || query.UserID > 0 || strings.TrimSpace(query.ModelName) != "" ||
		strings.TrimSpace(query.Username) != "" || strings.TrimSpace(query.TokenName) != "" || strings.TrimSpace(query.RequestID) != "" ||
		strings.TrimSpace(query.UpstreamRequestID) != "" || query.StartTimestamp != 0 || query.EndTimestamp != 0 || query.Channel != 0 || strings.TrimSpace(query.Group) != ""
	platformobservability.SysLog(fmt.Sprintf(
		"slow log query scope=%s count_ms=%d page_ms=%d enrich_ms=%d rows=%d filters_present=%t total_ms=%d",
		scope, countElapsed.Milliseconds(), pageElapsed.Milliseconds(), enrichElapsed.Milliseconds(), rows, filtersPresent, totalElapsed.Milliseconds(),
	))
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
	usedLogGroupsCache.RLock()
	if item, ok := usedLogGroupsCache.items[userID]; ok && item.db == platformdb.LogDB && time.Since(item.at) < usedLogGroupsCacheTTL {
		groups := append([]string(nil), item.groups...)
		usedLogGroupsCache.RUnlock()
		return groups, nil
	}
	usedLogGroupsCache.RUnlock()
	value, err, _ := usedLogGroupsLoads.Do(fmt.Sprintf("%p:%d", platformdb.LogDB, userID), func() (any, error) {
		return loadUsedLogGroups(userID)
	})
	if err != nil {
		return nil, err
	}
	return append([]string(nil), value.([]string)...), nil
}

func loadUsedLogGroups(userID int) ([]string, error) {
	usedLogGroupsCache.RLock()
	item, ok := usedLogGroupsCache.items[userID]
	usedLogGroupsCache.RUnlock()
	if ok && item.db == platformdb.LogDB && time.Since(item.at) < usedLogGroupsCacheTTL {
		return append([]string(nil), item.groups...), nil
	}
	groups := make([]string, 0)
	groupCol := logGroupColumn()
	query := platformdb.LogDB.Table("logs").
		Distinct(groupCol).
		Where(groupCol + " <> ''")
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var err error
	if platformdb.UsingPostgreSQL {
		// Seek to the next distinct key in idx_logs_group rather than visiting
		// every historical log just to produce at most 200 filter options.
		// User-scoped seeks use idx_logs_user_group and constrain both branches.
		scope := ""
		args := make([]any, 0, 3)
		if userID > 0 {
			scope = "user_id = ? AND "
			args = append(args, userID, userID)
		}
		args = append(args, logGroupOptionLimit)
		err = platformdb.LogDB.WithContext(ctx).Raw(fmt.Sprintf(`WITH RECURSIVE used_groups AS (
			(SELECT "group" AS name, 1 AS n FROM logs WHERE %s"group" > '' ORDER BY "group" LIMIT 1)
			UNION ALL
			SELECT next.name, used_groups.n + 1 FROM used_groups
			CROSS JOIN LATERAL (SELECT "group" AS name FROM logs WHERE %s"group" > used_groups.name ORDER BY "group" LIMIT 1) AS next
			WHERE used_groups.n < ?
		) SELECT name FROM used_groups ORDER BY name`, scope, scope), args...).Scan(&groups).Error
	} else {
		err = query.WithContext(ctx).Order(groupCol+" ASC").Limit(logGroupOptionLimit).Pluck(groupCol, &groups).Error
	}
	if err == nil {
		usedLogGroupsCache.Lock()
		if usedLogGroupsCache.items == nil {
			usedLogGroupsCache.items = make(map[int]cachedLogGroups)
		}
		if len(usedLogGroupsCache.items) >= 1024 {
			for id, cached := range usedLogGroupsCache.items {
				if time.Since(cached.at) >= usedLogGroupsCacheTTL {
					delete(usedLogGroupsCache.items, id)
				}
			}
			if len(usedLogGroupsCache.items) >= 1024 {
				clear(usedLogGroupsCache.items)
			}
		}
		usedLogGroupsCache.items[userID] = cachedLogGroups{groups: append([]string(nil), groups...), at: time.Now(), db: platformdb.LogDB}
		usedLogGroupsCache.Unlock()
	}
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
