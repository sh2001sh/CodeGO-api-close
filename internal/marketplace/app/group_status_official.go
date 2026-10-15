package app

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sh2001sh/new-api/constant"
	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

var officialWalletStatsCache struct {
	sync.Mutex
	entries map[string]officialWalletStatsCacheEntry
}

type officialWalletStatsCacheEntry struct {
	at     time.Time
	values map[string]channelConsumerStats
}

var officialWalletStatsRefreshes singleflight.Group

// Status discovery shares routing permissions, but never depends on an official
// group having a marketplace row. Third-party rows cannot bypass market review.
func officialStatusModels(viewerUserID int) (map[string][]string, error) {
	userGroup := ""
	if viewerUserID > 0 {
		var err error
		userGroup, err = identitystore.LoadUserGroup(viewerUserID, false)
		if err != nil {
			return nil, err
		}
	}
	usable := gatewayroutingapp.GetUserUsableGroups(userGroup)
	var thirdParty []marketplaceschema.Group
	if err := platformdb.DB.Select("internal_group_name").Where("source_type <> ?", marketplacedomain.SourceTypeOfficial).Find(&thirdParty).Error; err != nil {
		return nil, err
	}
	for _, group := range thirdParty {
		delete(usable, group.InternalGroupName)
	}
	names := make([]string, 0, len(usable))
	for name := range usable {
		if name != "auto" && name != "all" && strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}
	result := make(map[string][]string)
	if len(names) == 0 {
		return result, nil
	}
	groupColumn := "abilities.`group`"
	if platformdb.UsingPostgreSQL {
		groupColumn = `abilities."group"`
	}
	var abilities []gatewayschema.Ability
	if err := platformdb.DB.Table("abilities").Select(groupColumn+", abilities.model").Distinct().
		Joins("JOIN channels ON channels.id = abilities.channel_id").
		Where(groupColumn+" IN ? AND abilities.enabled = ? AND channels.status = ?", names, true, constant.ChannelStatusEnabled).
		Order("abilities.model").Scan(&abilities).Error; err != nil {
		return nil, err
	}
	for _, ability := range abilities {
		if strings.TrimSpace(ability.Model) != "" {
			result[ability.Group] = append(result[ability.Group], ability.Model)
		}
	}
	return result, nil
}

func listOfficialGroupStatus(viewerUserID int) ([]GroupListItem, error) {
	models, err := officialStatusModels(viewerUserID)
	if err != nil || len(models) == 0 {
		return nil, err
	}
	userGroup := ""
	if viewerUserID > 0 {
		userGroup, err = identitystore.LoadUserGroup(viewerUserID, false)
		if err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, len(models))
	groupIndices := make(map[string]int, len(models))
	for name := range models {
		names = append(names, name)
	}
	sort.Strings(names)
	for index, name := range names {
		groupIndices[name] = index + 1
	}
	start, _ := marketplaceRecentWindow(time.Now().Unix())
	var rows []auditprojection.GroupModelSeries
	var summaries []auditprojection.GroupSummary
	if platformdb.DB != nil && platformdb.LogDB != nil {
		rows, err = auditprojection.QuerySeriesByGroupModels(marketplaceRecentWindowHours, names)
		if err != nil {
			return nil, err
		}
		summaries, err = auditprojection.QuerySummaryByGroups(24, names)
		if err != nil {
			return nil, err
		}
	}
	metrics := make(map[string]auditprojection.GroupSummary, len(summaries))
	for _, summary := range summaries {
		metrics[summary.Group] = summary
	}
	series := buildMarketplaceRecentRequestSeries(start, groupIndices, rows)
	capabilities, err := loadOfficialGroupCapabilities(names)
	if err != nil {
		return nil, err
	}
	channelIDs := make([]int, 0)
	seen := make(map[int]bool)
	for _, capability := range capabilities {
		for _, id := range capability.ChannelIDs {
			if !seen[id] {
				channelIDs = append(channelIDs, id)
				seen[id] = true
			}
		}
	}
	active := gatewayruntime.ActiveChannelRequestsForChannels(channelIDs)
	walletStats := officialWalletConsumerStatsCached(names, 24)
	items := make([]GroupListItem, 0, len(names))
	for _, name := range names {
		capability := capabilities[name]
		currentConcurrency := 0
		for _, id := range capability.ChannelIDs {
			currentConcurrency += active[id]
		}
		key := officialAutoRoutePrefix + name
		summary := metrics[name]
		consumerStats := walletStats[name]
		recent := series[groupIndices[name]]
		averageConsumerAmount := consumerStats.averageConsumerAmount()
		items = append(items, GroupListItem{
			ID: key, PublicSlug: key, SystemDisplayName: name,
			SourceType: marketplacedomain.SourceTypeOfficial, SourceLabel: "官方",
			Multiplier:      gatewayroutingapp.GetUserGroupRatio(userGroup, name),
			LifecycleStatus: marketplacedomain.LifecycleActive, Models: models[name],
			ModelVerificationResults: []ModelVerificationResult{},
			RequestCount:             summary.RequestCount, SuccessRate: summary.SuccessRate, WilsonSuccessRate: summary.SuccessRate,
			AvgTTFTMs: float64(summary.AvgTtftMs), LatencySampleCount: summary.RequestCount,
			Score: summary.SuccessRate*0.35 + inverseMetricScore(float64(summary.AvgTtftMs), 3000)*0.2 + consumerAmountScore(averageConsumerAmount)*0.2, CacheHitRate: summary.CacheHitRate,
			AvgConsumerAmount: averageConsumerAmount, AvgConsumerAmountByModel: consumerStats.averageConsumerAmountsByModel(),
			RecentRequestSeries: recent, RecentRequestBucketSeconds: marketplaceRecentBucketSeconds,
			LatestRequestStatus:     latestRequestStatus(recent),
			MultiplierCardSupported: capability.MultiplierCard, MultiplierCardUserEnabled: capability.MultiplierCard,
			CurrentConcurrency: currentConcurrency,
		})
	}
	return items, nil
}

func officialWalletConsumerStats(names []string, hours int) (map[string]channelConsumerStats, error) {
	if len(names) == 0 || platformdb.LogDB == nil {
		return make(map[string]channelConsumerStats), nil
	}
	key := fmt.Sprintf("%d:%s", hours, strings.Join(names, "\x00"))
	officialWalletStatsCache.Lock()
	entry, exists := officialWalletStatsCache.entries[key]
	if exists && time.Since(entry.at) < 5*time.Minute {
		officialWalletStatsCache.Unlock()
		return entry.values, nil
	}
	officialWalletStatsCache.Unlock()
	value, err, _ := officialWalletStatsRefreshes.Do(key, func() (any, error) {
		result, queryErr := queryOfficialWalletConsumerStats(names, hours)
		if queryErr != nil {
			return nil, queryErr
		}
		storeOfficialWalletStats(key, result)
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	return value.(map[string]channelConsumerStats), nil
}

// List and route-pool requests must never wait for a 24-hour log aggregation.
// Return the last cache entry (or empty values after a restart) and let one
// singleflight refresh populate the next request.
func officialWalletConsumerStatsCached(names []string, hours int) map[string]channelConsumerStats {
	if len(names) == 0 || platformdb.LogDB == nil {
		return make(map[string]channelConsumerStats)
	}
	key := fmt.Sprintf("%d:%s", hours, strings.Join(names, "\x00"))
	officialWalletStatsCache.Lock()
	entry, exists := officialWalletStatsCache.entries[key]
	officialWalletStatsCache.Unlock()
	if exists && time.Since(entry.at) < 5*time.Minute {
		return entry.values
	}
	go func() {
		if _, err := officialWalletConsumerStats(names, hours); err != nil {
			platformobservability.SysError("refresh official wallet costs: " + err.Error())
		}
	}()
	if exists {
		return entry.values
	}
	return make(map[string]channelConsumerStats)
}

func storeOfficialWalletStats(key string, values map[string]channelConsumerStats) {
	officialWalletStatsCache.Lock()
	defer officialWalletStatsCache.Unlock()
	if officialWalletStatsCache.entries == nil {
		officialWalletStatsCache.entries = make(map[string]officialWalletStatsCacheEntry)
	}
	if len(officialWalletStatsCache.entries) >= 64 {
		oldestKey := ""
		var oldest time.Time
		for candidateKey, candidate := range officialWalletStatsCache.entries {
			if oldestKey == "" || candidate.at.Before(oldest) {
				oldestKey, oldest = candidateKey, candidate.at
			}
		}
		delete(officialWalletStatsCache.entries, oldestKey)
	}
	officialWalletStatsCache.entries[key] = officialWalletStatsCacheEntry{at: time.Now(), values: values}
}

func queryOfficialWalletConsumerStats(names []string, hours int) (map[string]channelConsumerStats, error) {
	result := make(map[string]channelConsumerStats, len(names))
	rows, err := auditprojection.QueryGroupConsumerMetrics(hours, names)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		stats := result[row.GroupName]
		stats.WalletRequestCount += row.RequestCount
		stats.WalletConsumerAmount += row.Quota
		stats.WalletTokenCount += row.TokenCount
		if strings.TrimSpace(row.ModelName) != "" {
			if stats.ByModel == nil {
				stats.ByModel = make(map[string]consumerAmountStats)
			}
			stats.ByModel[row.ModelName] = consumerAmountStats{TokenCount: row.TokenCount, Amount: row.Quota}
		}
		result[row.GroupName] = stats
	}
	return result, nil
}

type officialGroupCapability struct {
	Models         []string
	ChannelIDs     []int
	MultiplierCard bool
}

func loadOfficialGroupCapabilities(names []string) (map[string]officialGroupCapability, error) {
	result := make(map[string]officialGroupCapability)
	if len(names) == 0 {
		return result, nil
	}
	groupColumn := "abilities.`group`"
	if platformdb.UsingPostgreSQL {
		groupColumn = `abilities."group"`
	}
	var rows []struct {
		GroupName                 string
		Model                     string
		ChannelID                 int
		MultiplierCardUserEnabled bool
	}
	err := platformdb.DB.Table("abilities").
		Select(groupColumn+" AS group_name, abilities.model, abilities.channel_id, channels.multiplier_card_user_enabled").
		Joins("JOIN channels ON channels.id = abilities.channel_id").
		Where(groupColumn+" IN ? AND abilities.enabled = ? AND channels.status = ?", names, true, constant.ChannelStatusEnabled).
		Order("abilities.model, abilities.channel_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	modelsSeen, channelsSeen := make(map[string]map[string]bool), make(map[string]map[int]bool)
	for _, row := range rows {
		item := result[row.GroupName]
		if modelsSeen[row.GroupName] == nil {
			modelsSeen[row.GroupName], channelsSeen[row.GroupName] = make(map[string]bool), make(map[int]bool)
		}
		if row.Model != "" && !modelsSeen[row.GroupName][row.Model] {
			item.Models = append(item.Models, row.Model)
			modelsSeen[row.GroupName][row.Model] = true
		}
		if !channelsSeen[row.GroupName][row.ChannelID] {
			item.ChannelIDs = append(item.ChannelIDs, row.ChannelID)
			channelsSeen[row.GroupName][row.ChannelID] = true
		}
		item.MultiplierCard = item.MultiplierCard || row.MultiplierCardUserEnabled
		result[row.GroupName] = item
	}
	return result, nil
}

func getOfficialGroupModelStatus(name string, viewerUserID int) ([]GroupModelRequestStatus, error) {
	groups, err := officialStatusModels(viewerUserID)
	if err != nil {
		return nil, err
	}
	models, exists := groups[name]
	if !exists {
		return nil, gorm.ErrRecordNotFound
	}
	start, _ := marketplaceRecentWindow(time.Now().Unix())
	var rows []auditprojection.GroupModelSeries
	if platformdb.DB != nil && platformdb.LogDB != nil {
		rows, err = auditprojection.QuerySeriesByGroupModels(marketplaceRecentWindowHours, []string{name})
		if err != nil {
			return nil, err
		}
	}
	return buildGroupModelRequestStatus(start, name, models, rows), nil
}
