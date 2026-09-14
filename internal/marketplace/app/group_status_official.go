package app

import (
	"sort"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/constant"
	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

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
	start, end := marketplaceRecentWindow(time.Now().Unix())
	var rows []gatewaystore.GroupModelRequestBucket
	var summaries []auditprojection.GroupSummary
	if platformdb.LogDB != nil {
		rows, err = gatewaystore.LoadCachedGroupModelRequestBuckets(start, end, marketplaceRecentBucketSeconds, names)
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
	items := make([]GroupListItem, 0, len(names))
	for _, name := range names {
		capability := capabilities[name]
		currentConcurrency := 0
		for _, id := range capability.ChannelIDs {
			currentConcurrency += active[id]
		}
		key := officialAutoRoutePrefix + name
		summary := metrics[name]
		recent := series[groupIndices[name]]
		items = append(items, GroupListItem{
			ID: key, PublicSlug: key, SystemDisplayName: name,
			SourceType: marketplacedomain.SourceTypeOfficial, SourceLabel: "官方",
			Multiplier:      gatewayroutingapp.GetUserGroupRatio(userGroup, name),
			LifecycleStatus: marketplacedomain.LifecycleActive, Models: models[name],
			ModelVerificationResults: []ModelVerificationResult{}, GPT56MappingResults: []GPT56MappingResult{},
			RequestCount: summary.RequestCount, SuccessRate: summary.SuccessRate, WilsonSuccessRate: summary.SuccessRate,
			AvgTTFTMs: float64(summary.AvgTtftMs), LatencySampleCount: summary.RequestCount,
			Score: summary.SuccessRate*0.35 + inverseMetricScore(float64(summary.AvgTtftMs), 3000)*0.2 + inverseMetricScore(1, 3)*0.2, CacheHitRate: summary.CacheHitRate,
			RecentRequestSeries: recent, RecentRequestBucketSeconds: marketplaceRecentBucketSeconds,
			LatestRequestStatus:     latestRequestStatus(recent),
			MultiplierCardSupported: capability.MultiplierCard, MultiplierCardUserEnabled: capability.MultiplierCard,
			CurrentConcurrency: currentConcurrency,
		})
	}
	return items, nil
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
	start, end := marketplaceRecentWindow(time.Now().Unix())
	var rows []gatewaystore.GroupModelRequestBucket
	if platformdb.LogDB != nil {
		rows, err = gatewaystore.LoadCachedGroupModelRequestBuckets(start, end, marketplaceRecentBucketSeconds, []string{name})
		if err != nil {
			return nil, err
		}
	}
	return buildGroupModelRequestStatus(start, name, models, rows), nil
}
