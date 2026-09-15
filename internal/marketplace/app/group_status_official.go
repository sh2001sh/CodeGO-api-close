package app

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sh2001sh/new-api/constant"
	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

var officialWalletStatsCache struct {
	sync.Mutex
	at     time.Time
	key    string
	values map[string]channelConsumerStats
}

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
	walletStats, err := officialWalletConsumerStats(names, 24)
	if err != nil {
		return nil, err
	}
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
	result := make(map[string]channelConsumerStats, len(names))
	if len(names) == 0 || platformdb.LogDB == nil {
		return result, nil
	}
	key := fmt.Sprintf("%d:%s", hours, strings.Join(names, "\x00"))
	officialWalletStatsCache.Lock()
	if officialWalletStatsCache.key == key && time.Since(officialWalletStatsCache.at) < 5*time.Minute {
		values := officialWalletStatsCache.values
		officialWalletStatsCache.Unlock()
		return values, nil
	}
	officialWalletStatsCache.Unlock()

	groupColumn := "`group`"
	if platformdb.UsingPostgreSQL {
		groupColumn = `"group"`
	}
	var rows []struct {
		GroupName            string `gorm:"column:group_name"`
		ModelName            string `gorm:"column:model_name"`
		WalletRequestCount   int64  `gorm:"column:wallet_request_count"`
		WalletConsumerAmount int64  `gorm:"column:wallet_consumer_amount"`
		WalletTokenCount     int64  `gorm:"column:wallet_token_count"`
	}
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
	err := platformdb.LogDB.Model(&auditschema.Log{}).
		Select(groupColumn+` AS group_name, model_name,
			COUNT(*) AS wallet_request_count,
			COALESCE(SUM(quota), 0) AS wallet_consumer_amount,
			COALESCE(SUM(prompt_tokens + completion_tokens), 0) AS wallet_token_count`).
		Where("type = ? AND created_at >= ? AND "+groupColumn+" IN ? AND other LIKE ? AND prompt_tokens + completion_tokens > 0", auditschema.LogTypeConsume, cutoff, names, walletBillingSourcePattern).
		Group(groupColumn + ", model_name").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		stats := result[row.GroupName]
		stats.WalletRequestCount += row.WalletRequestCount
		stats.WalletConsumerAmount += row.WalletConsumerAmount
		stats.WalletTokenCount += row.WalletTokenCount
		if strings.TrimSpace(row.ModelName) != "" {
			if stats.ByModel == nil {
				stats.ByModel = make(map[string]consumerAmountStats)
			}
			stats.ByModel[row.ModelName] = consumerAmountStats{TokenCount: row.WalletTokenCount, Amount: row.WalletConsumerAmount}
		}
		result[row.GroupName] = stats
	}
	officialWalletStatsCache.Lock()
	officialWalletStatsCache.at = time.Now()
	officialWalletStatsCache.key = key
	officialWalletStatsCache.values = result
	officialWalletStatsCache.Unlock()
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
