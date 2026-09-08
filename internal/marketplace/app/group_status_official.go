package app

import (
	"sort"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/constant"
	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
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
		rows, err = gatewaystore.LoadGroupModelRequestBuckets(start, end, marketplaceRecentBucketSeconds, names)
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
	items := make([]GroupListItem, 0, len(names))
	for _, name := range names {
		key := officialAutoRoutePrefix + name
		summary := metrics[name]
		recent := series[groupIndices[name]]
		items = append(items, GroupListItem{
			ID: key, PublicSlug: key, SystemDisplayName: name,
			SourceType: marketplacedomain.SourceTypeOfficial, SourceLabel: "官方", Multiplier: 1,
			LifecycleStatus: marketplacedomain.LifecycleActive, Models: models[name],
			ModelVerificationResults: []ModelVerificationResult{}, GPT56MappingResults: []GPT56MappingResult{},
			RequestCount: summary.RequestCount, SuccessRate: summary.SuccessRate, WilsonSuccessRate: summary.SuccessRate,
			Score: summary.SuccessRate*0.35 + inverseMetricScore(1, 3)*0.2, CacheHitRate: summary.CacheHitRate,
			RecentRequestSeries: recent, RecentRequestBucketSeconds: marketplaceRecentBucketSeconds,
			LatestRequestStatus: latestRequestStatus(recent),
		})
	}
	return items, nil
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
		rows, err = gatewaystore.LoadGroupModelRequestBuckets(start, end, marketplaceRecentBucketSeconds, []string{name})
		if err != nil {
			return nil, err
		}
	}
	return buildGroupModelRequestStatus(start, name, models, rows), nil
}
