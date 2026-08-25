package store

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"golang.org/x/sync/singleflight"
)

type groupStatusCacheEntry struct {
	refreshAt time.Time
	expiresAt time.Time
	rows      []GroupModelRequestBucket
}

type groupStatusCacheState uint8

const (
	groupStatusCacheMiss groupStatusCacheState = iota
	groupStatusCacheFresh
	groupStatusCacheStale
)

var groupStatusCache = struct {
	sync.Mutex
	items map[string]groupStatusCacheEntry
}{items: make(map[string]groupStatusCacheEntry)}

var groupStatusLoads singleflight.Group
var loadGroupModelRequestBuckets = queryGroupModelRequestBuckets

func ListGroupStatusGroups() ([]string, error) {
	groupColumn := abilityGroupColumn()

	var groups []string
	err := platformdb.DB.Table("abilities").
		Select(groupColumn).
		Distinct().
		Where(groupColumn+" <> ''").
		Order(groupColumn+" ASC").
		Pluck(groupColumn, &groups).Error
	if err != nil {
		return nil, err
	}

	filtered := make([]string, 0, len(groups))
	for _, groupName := range groups {
		if strings.TrimSpace(groupName) == "" || groupName == "auto" {
			continue
		}
		filtered = append(filtered, groupName)
	}
	return filtered, nil
}

func LoadGroupModelRequestBuckets(startTime int64, endTime int64, bucketSize int64, groups []string) ([]GroupModelRequestBucket, error) {
	if endTime <= startTime {
		return []GroupModelRequestBucket{}, nil
	}
	if bucketSize <= 0 {
		bucketSize = 60
	}

	filteredGroups := normalizeGroupStatusGroups(groups)
	cacheTTL := time.Duration(platformconfig.GroupStatusCacheSeconds) * time.Second
	if cacheTTL <= 0 {
		cacheTTL = time.Minute
	}
	cacheKey := fmt.Sprintf("%d:%d:%d:%s", startTime, endTime, bucketSize, strings.Join(filteredGroups, "\x00"))
	if rows, state := loadGroupStatusCache(cacheKey, time.Now()); state == groupStatusCacheFresh {
		return rows, nil
	} else if state == groupStatusCacheStale {
		refreshGroupStatusCacheAsync(cacheKey, startTime, endTime, bucketSize, filteredGroups, cacheTTL)
		return rows, nil
	}
	value, err, _ := groupStatusLoads.Do(cacheKey, func() (any, error) {
		now := time.Now()
		if rows, state := loadGroupStatusCache(cacheKey, now); state != groupStatusCacheMiss {
			return rows, nil
		}
		return refreshGroupStatusCache(cacheKey, startTime, endTime, bucketSize, filteredGroups, cacheTTL)
	})
	if err != nil {
		return nil, err
	}
	return append([]GroupModelRequestBucket(nil), value.([]GroupModelRequestBucket)...), nil
}

func refreshGroupStatusCacheAsync(cacheKey string, startTime, endTime, bucketSize int64, groups []string, cacheTTL time.Duration) {
	go func() {
		_, err, _ := groupStatusLoads.Do(cacheKey, func() (any, error) {
			return refreshGroupStatusCache(cacheKey, startTime, endTime, bucketSize, groups, cacheTTL)
		})
		if err != nil {
			platformobservability.SysError("refresh group status cache: " + err.Error())
		}
	}()
}

func refreshGroupStatusCache(cacheKey string, startTime, endTime, bucketSize int64, groups []string, cacheTTL time.Duration) ([]GroupModelRequestBucket, error) {
	rows, err := loadGroupModelRequestBuckets(startTime, endTime, bucketSize, groups)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	storeGroupStatusCache(cacheKey, rows, now.Add(cacheTTL), now.Add(groupStatusCacheMaxAge(cacheTTL)), now)
	return rows, nil
}

func groupStatusCacheMaxAge(cacheTTL time.Duration) time.Duration {
	maxAge := cacheTTL * 5
	if maxAge < 5*time.Minute {
		return 5 * time.Minute
	}
	if maxAge > 30*time.Minute {
		return 30 * time.Minute
	}
	return maxAge
}

func normalizeGroupStatusGroups(groups []string) []string {
	filtered := make([]string, 0, len(groups))
	for _, groupName := range groups {
		groupName = strings.TrimSpace(groupName)
		if groupName != "" && groupName != "auto" {
			filtered = append(filtered, groupName)
		}
	}
	sort.Strings(filtered)
	return filtered
}

func loadGroupStatusCache(cacheKey string, now time.Time) ([]GroupModelRequestBucket, groupStatusCacheState) {
	groupStatusCache.Lock()
	defer groupStatusCache.Unlock()
	cached, ok := groupStatusCache.items[cacheKey]
	if !ok || !now.Before(cached.expiresAt) {
		if ok {
			delete(groupStatusCache.items, cacheKey)
		}
		return nil, groupStatusCacheMiss
	}
	rows := append([]GroupModelRequestBucket(nil), cached.rows...)
	if now.Before(cached.refreshAt) {
		return rows, groupStatusCacheFresh
	}
	return rows, groupStatusCacheStale
}

func queryGroupModelRequestBuckets(startTime, endTime, bucketSize int64, groups []string) ([]GroupModelRequestBucket, error) {
	groupColumn := logGroupColumn()
	bucketExpr := logBucketIndexExpr(startTime, bucketSize)
	selectExpr := fmt.Sprintf(
		"%s as group_name, model_name, %s as bucket_index, COUNT(*) as request_count, SUM(CASE WHEN type = %d THEN 1 ELSE 0 END) as success_count",
		groupColumn,
		bucketExpr,
		auditschema.LogTypeConsume,
	)

	query := platformdb.LogDB.Table("logs").
		Select(selectExpr).
		Where("created_at >= ? AND created_at < ?", startTime, endTime).
		Where("type IN ?", []int{auditschema.LogTypeConsume, auditschema.LogTypeError}).
		Where(successRateLogFilter()).
		Where("model_name <> ''").
		Where(groupColumn + " <> ''")

	if len(groups) > 0 {
		query = query.Where(groupColumn+" IN ?", groups)
	}

	var rows []GroupModelRequestBucket
	err := query.
		Group(fmt.Sprintf("%s, model_name, %s", groupColumn, bucketExpr)).
		Order("group_name ASC, model_name ASC, bucket_index ASC").
		Find(&rows).Error
	return rows, err
}

func storeGroupStatusCache(cacheKey string, rows []GroupModelRequestBucket, refreshAt, expiresAt, now time.Time) {
	groupStatusCache.Lock()
	defer groupStatusCache.Unlock()
	if len(groupStatusCache.items) >= 128 {
		for key, item := range groupStatusCache.items {
			if now.After(item.expiresAt) {
				delete(groupStatusCache.items, key)
			}
		}
		if len(groupStatusCache.items) >= 128 {
			for key := range groupStatusCache.items {
				delete(groupStatusCache.items, key)
				break
			}
		}
	}
	groupStatusCache.items[cacheKey] = groupStatusCacheEntry{
		refreshAt: refreshAt,
		expiresAt: expiresAt,
		rows:      append([]GroupModelRequestBucket(nil), rows...),
	}
}

func successRateLogFilter() string {
	if os.Getenv("LOG_SQL_DSN") != "" {
		switch platformdb.LogSQLType {
		case platformdb.DatabaseTypePostgreSQL:
			return postgresSuccessRateLogFilter
		case platformdb.DatabaseTypeMySQL:
			return mysqlSuccessRateLogFilter
		default:
			return sqliteSuccessRateLogFilter
		}
	}
	if platformdb.UsingPostgreSQL {
		return postgresSuccessRateLogFilter
	}
	if platformdb.UsingMySQL {
		return mysqlSuccessRateLogFilter
	}
	return sqliteSuccessRateLogFilter
}

const (
	postgresSuccessRateLogFilter = `COALESCE(substring(other from '"counted_in_success_rate"[[:space:]]*:[[:space:]]*(true|false)') <> 'false', true) AND COALESCE(substring(other from '"error_code"[[:space:]]*:[[:space:]]*"([^"]+)"'), '') <> 'sensitive_words_detected'`
	mysqlSuccessRateLogFilter    = `CASE WHEN JSON_VALID(other) THEN COALESCE(JSON_UNQUOTE(JSON_EXTRACT(other, '$.counted_in_success_rate')), 'true') <> 'false' AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(other, '$.error_code')), '') <> 'sensitive_words_detected' ELSE true END`
	sqliteSuccessRateLogFilter   = `CASE WHEN json_valid(other) THEN COALESCE(json_extract(other, '$.counted_in_success_rate'), 1) <> 0 AND COALESCE(json_extract(other, '$.error_code'), '') <> 'sensitive_words_detected' ELSE 1 END`
)

func logGroupColumn() string {
	if os.Getenv("LOG_SQL_DSN") != "" {
		if platformdb.LogSQLType == platformdb.DatabaseTypePostgreSQL {
			return `"group"`
		}
		return "`group`"
	}
	return abilityGroupColumn()
}

func logBucketIndexExpr(startTime int64, bucketSize int64) string {
	if os.Getenv("LOG_SQL_DSN") != "" {
		switch platformdb.LogSQLType {
		case platformdb.DatabaseTypeMySQL:
			return fmt.Sprintf("FLOOR((created_at - %d) / %d)", startTime, bucketSize)
		case platformdb.DatabaseTypePostgreSQL:
			return fmt.Sprintf("CAST((created_at - %d) / %d AS BIGINT)", startTime, bucketSize)
		default:
			return fmt.Sprintf("CAST((created_at - %d) / %d AS INTEGER)", startTime, bucketSize)
		}
	}

	if platformdb.UsingMySQL {
		return fmt.Sprintf("FLOOR((created_at - %d) / %d)", startTime, bucketSize)
	}
	if platformdb.UsingPostgreSQL {
		return fmt.Sprintf("CAST((created_at - %d) / %d AS BIGINT)", startTime, bucketSize)
	}
	return fmt.Sprintf("CAST((created_at - %d) / %d AS INTEGER)", startTime, bucketSize)
}
