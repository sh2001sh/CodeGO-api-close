package app

import (
	"sort"
	"strings"
	"time"

	gatewaydomain "github.com/sh2001sh/new-api/internal/gateway/domain"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

var loadActiveChannelRequestLeases = gatewayruntime.ActiveChannelRequestLeasesForChannels

func normalizeGroupQuery(query GroupQuery) GroupQuery {
	if query.WindowHours != 24 && query.WindowHours != 24*7 && query.WindowHours != 24*30 {
		query.WindowHours = 24
	}
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize != 50 {
		query.PageSize = 20
	}
	if query.Direction != "asc" {
		query.Direction = "desc"
	}
	return query
}

func filterAndSortGroups(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel, snapshots map[string]marketplaceschema.RankingSnapshot, recentSeries map[int][]RecentRequestBucket, query GroupQuery) ([]GroupListItem, error) {
	items := make([]GroupListItem, 0, len(groups))
	overrides := make(map[string]float64)
	if query.ViewerUserID > 0 && len(channels) > 0 {
		channelIDs := make([]string, 0, len(channels))
		for id := range channels {
			channelIDs = append(channelIDs, id)
		}
		var rows []marketplaceschema.UserMultiplier
		if err := platformdb.DB.Where("user_id = ? AND channel_id IN ?", query.ViewerUserID, channelIDs).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			overrides[row.ChannelID] = row.Multiplier
		}
	}
	currentConcurrency := activeMarketplaceChannelRequests(channels)
	for _, group := range groups {
		channel := channels[group.ChannelID]
		models := decodeModels(channel.DeclaredModels)
		if !matchesGroupQuery(group, channel, models, query) {
			continue
		}
		snapshot := snapshots[group.ID]
		channelID := 0
		if channel.InternalChannelID != nil {
			channelID = *channel.InternalChannelID
		}
		item := groupListItem(group, channel, models, snapshot, recentSeries[channelID])
		if query.ViewerUserID > 0 && query.ViewerUserID != group.OwnerUserID {
			if override := overrides[channel.ID]; override > 0 {
				item.Multiplier = marketplacedomain.NormalizeMultiplier(override)
				item.SubscriptionMultiplier = marketplacedomain.SubscriptionMultiplier(item.Multiplier)
			}
		}
		item.CurrentConcurrency = currentConcurrency[channelID]
		items = append(items, item)
	}
	modelFilters := query.Models
	if len(modelFilters) == 0 && strings.TrimSpace(query.Model) != "" {
		modelFilters = []string{query.Model}
	}
	sortGroupItems(items, query.Sort, query.Direction, modelFilters...)
	return items, nil
}

func matchesGroupQuery(group marketplaceschema.Group, channel marketplaceschema.Channel, models []string, query GroupQuery) bool {
	search := strings.ToLower(strings.TrimSpace(query.Search))
	if search != "" {
		if isNumericChannelID(search) {
			if channel.ID != search {
				return false
			}
		} else if !matchesMarketplaceKeyword(group, channel, models, search) {
			return false
		}
	}
	modelFilters := query.Models
	if len(modelFilters) == 0 && strings.TrimSpace(query.Model) != "" {
		modelFilters = []string{query.Model}
	}
	if len(modelFilters) > 0 && !matchesAnyModelFilter(models, modelFilters) {
		return false
	}
	if query.Source != "" && !strings.EqualFold(publicSourceLabel(channel), query.Source) {
		return false
	}
	if query.Provider != "" && !strings.EqualFold(channel.ProviderType, query.Provider) {
		return false
	}
	switch query.MultiplierCard {
	case "supported":
		if !channel.MultiplierCardSupported {
			return false
		}
	case "unsupported":
		if channel.MultiplierCardSupported {
			return false
		}
	}
	return channel.ID != ""
}

func matchesAnyModelFilter(models, filters []string) bool {
	for _, filter := range filters {
		if strings.TrimSpace(filter) != "" && containsSubstringFold(models, filter) {
			return true
		}
	}
	return false
}

func isNumericChannelID(search string) bool {
	if search == "" {
		return false
	}
	for _, char := range search {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func matchesMarketplaceKeyword(group marketplaceschema.Group, channel marketplaceschema.Channel, models []string, search string) bool {
	haystack := strings.ToLower(group.ID + " " + group.SystemDisplayName + " " + channel.ID + " " + marketplaceDisplayName(publicSourceLabel(channel), group.Multiplier, channel.ID) + " " + group.PublicSlug + " " + channel.ProviderType + " " + publicSourceLabel(channel) + " " + strings.Join(models, " "))
	return strings.Contains(haystack, search)
}

func groupListItem(group marketplaceschema.Group, channel marketplaceschema.Channel, models []string, snapshot marketplaceschema.RankingSnapshot, recentSeries []RecentRequestBucket) GroupListItem {
	if len(recentSeries) == 0 {
		recentSeries = emptyMarketplaceRecentRequestSeries(time.Now().Unix())
	}
	groupMultiplier := marketplacedomain.NormalizeMultiplier(group.Multiplier)
	return GroupListItem{
		ModelPrices: decodeChannelModelPrices(channel.ModelPrices),
		ID:          group.ID, ChannelID: channel.ID, PublicSlug: group.PublicSlug,
		SystemDisplayName: marketplaceDisplayName(publicSourceLabel(channel), groupMultiplier, channel.ID),
		SourceType:        group.SourceType, SourceLabel: publicSourceLabel(channel),
		ProviderType:     channel.ProviderType,
		CreditPoolPolicy: group.CreditPoolPolicy,
		LifecycleStatus:  group.LifecycleStatus, VerificationStatus: group.VerificationStatus,
		VerificationDueAt: group.VerificationDueAt, Multiplier: groupMultiplier,
		SubscriptionEnabled:    group.CreditPoolPolicy == marketplacedomain.CreditPolicySubscriptionAndUniversal,
		SubscriptionMultiplier: marketplacedomain.SubscriptionMultiplier(groupMultiplier), Models: models,
		MultiplierCardSupported:   channel.MultiplierCardSupported,
		MultiplierCardUserEnabled: channel.MultiplierCardUserEnabled,
		VerificationCompletedAt:   latestModelVerificationAt(channel.ModelVerificationResults),
		ModelVerificationResults:  publicModelVerificationResults(channel.ModelVerificationResults),
		ConnectivityTestStatus:    channel.ConnectivityTestStatus,
		ConnectivityTestCheckedAt: channel.ConnectivityTestCheckedAt,
		RemoteCompactionSupport:   remoteCompactionSupport(channel.TransportCapabilities),
		ModelConsistencyStatus:    channel.ModelConsistencyStatus,
		Rank:                      snapshot.Rank, Score: snapshot.Score, SuccessRate: snapshot.RawSuccessRate,
		WilsonSuccessRate: snapshot.WilsonSuccessRate, AvgTTFTMs: snapshot.AvgTTFTMs,
		AttemptTTFTP50Ms: snapshot.AttemptTTFTP50Ms, AttemptTTFTP95Ms: snapshot.AttemptTTFTP95Ms,
		E2ETTFTP50Ms: snapshot.E2ETTFTP50Ms, E2ETTFTP95Ms: snapshot.E2ETTFTP95Ms,
		LatencySampleCount: snapshot.LatencySampleCount,
		AvgLatencyMs:       snapshot.AvgLatencyMs, AvgTPS: snapshot.AvgTPS,
		CacheHitRate: snapshot.CacheHitRate, AvgConsumerAmount: snapshot.AvgConsumerAmount,
		AvgConsumerAmountByModel: decodeConsumerAmountsByModel(snapshot.AvgConsumerAmountByModel), LatestRequestStatus: latestRequestStatus(recentSeries),
		RecentRequestSeries: recentSeries, RecentRequestBucketSeconds: marketplaceRecentBucketSeconds,
		RequestCount: snapshot.RequestCount, MaxConcurrency: channel.MaxConcurrency,
		UserMaxConcurrency:   channel.UserMaxConcurrency,
		IndependentConsumers: snapshot.IndependentConsumers,
		Observing:            snapshot.Observing, UpdatedAt: group.UpdatedAt,
	}
}

// remoteCompactionSupport reduces the persisted capability evidence to the
// versions that are actually supported. Unknown, pending, transient-error and
// protocol-not-applicable states are deliberately omitted from public market
// cards so they cannot be mistaken for a usable feature.
func remoteCompactionSupport(raw string) string {
	capabilities := decodeMarketplaceCapabilities(raw)
	v1 := capabilities.RemoteCompactionV1.Status == "supported"
	v2 := capabilities.RemoteCompactionV2.Status == "supported"
	switch {
	case v1 && v2:
		return "v1_v2"
	case v1:
		return "v1"
	case v2:
		return "v2"
	default:
		return ""
	}
}

func activeMarketplaceChannelRequests(channels map[string]marketplaceschema.Channel) map[int]int {
	limitedIDs := make([]int, 0, len(channels))
	unlimitedIDs := make([]int, 0, len(channels))
	for _, channel := range channels {
		if channel.InternalChannelID != nil && *channel.InternalChannelID > 0 {
			if channel.MaxConcurrency > 0 || channel.UserMaxConcurrency > 0 {
				limitedIDs = append(limitedIDs, *channel.InternalChannelID)
			} else {
				unlimitedIDs = append(unlimitedIDs, *channel.InternalChannelID)
			}
		}
	}
	result := gatewayruntime.ActiveChannelRequestsForChannels(unlimitedIDs)
	if limited, ok := loadActiveChannelRequestLeases(limitedIDs); ok {
		for channelID, active := range limited {
			result[channelID] = active
		}
		return result
	}
	for channelID, active := range gatewayruntime.ActiveChannelRequestsForChannels(limitedIDs) {
		result[channelID] = active
	}
	return result
}

func latestModelVerificationAt(raw string) *time.Time {
	results := decodeModelVerificationResults(raw)
	var latest time.Time
	for _, result := range results {
		if result.TestedAt.After(latest) {
			latest = result.TestedAt
		}
	}
	if latest.IsZero() {
		return nil
	}
	return &latest
}

func latestRequestStatus(series []RecentRequestBucket) string {
	for index := len(series) - 1; index >= 0; index-- {
		point := series[index]
		if point.RequestCount <= 0 {
			continue
		}
		return gatewaydomain.ClassifyRequestHealth(point.SuccessRate, point.RequestCount)
	}
	return gatewaydomain.RequestHealthUnknown
}

func marketplaceHighlights(items []GroupListItem) GroupHighlights {
	var result GroupHighlights
	for index := range items {
		item := items[index]
		highlight := GroupHighlight{
			GroupID: item.ID, SystemDisplayName: item.SystemDisplayName,
			Score: item.Score, Multiplier: item.Multiplier, AvgConsumerAmount: item.AvgConsumerAmount, AvgTTFTMs: item.AvgTTFTMs,
			AttemptTTFTP50Ms: item.AttemptTTFTP50Ms,
		}
		if !item.Observing && betterBest(result.Best, highlight) {
			value := highlight
			result.Best = &value
		}
		if highlight.AvgConsumerAmount > 0 && betterCheapest(result.Cheapest, highlight) {
			value := highlight
			result.Cheapest = &value
		}
		if item.LatencySampleCount > 0 && item.AttemptTTFTP50Ms > 0 && betterFastest(result.Fastest, highlight) {
			value := highlight
			result.Fastest = &value
		}
	}
	return result
}

func betterBest(current *GroupHighlight, candidate GroupHighlight) bool {
	return current == nil || candidate.Score > current.Score ||
		(candidate.Score == current.Score && candidate.GroupID < current.GroupID)
}

func betterCheapest(current *GroupHighlight, candidate GroupHighlight) bool {
	return current == nil || candidate.AvgConsumerAmount < current.AvgConsumerAmount ||
		(candidate.AvgConsumerAmount == current.AvgConsumerAmount && candidate.GroupID < current.GroupID)
}

func betterFastest(current *GroupHighlight, candidate GroupHighlight) bool {
	return current == nil || candidate.AttemptTTFTP50Ms < current.AttemptTTFTP50Ms ||
		(candidate.AttemptTTFTP50Ms == current.AttemptTTFTP50Ms && candidate.GroupID < current.GroupID)
}

func publicSourceLabel(channel marketplaceschema.Channel) string {
	if channel.SourceLabelStatus != marketplacedomain.SourceLabelApproved {
		return ""
	}
	return strings.TrimSpace(channel.ApprovedSourceLabel)
}

func sortGroupItems(items []GroupListItem, field, direction string, models ...string) {
	desc := direction != "asc"
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i].Score, items[j].Score
		switch field {
		case "success_rate":
			left, right = items[i].SuccessRate, items[j].SuccessRate
		case "ttft":
			leftMissing := items[i].LatencySampleCount <= 0 || items[i].AttemptTTFTP50Ms <= 0
			rightMissing := items[j].LatencySampleCount <= 0 || items[j].AttemptTTFTP50Ms <= 0
			if leftMissing != rightMissing {
				return !leftMissing
			}
			left, right = items[i].AttemptTTFTP50Ms, items[j].AttemptTTFTP50Ms
		case "latency":
			left, right = items[i].AvgLatencyMs, items[j].AvgLatencyMs
		case "multiplier":
			left, right = items[i].Multiplier, items[j].Multiplier
		case "consumer_amount", "model_consumer_amount":
			selectedModels := models
			if field == "consumer_amount" {
				selectedModels = nil
			}
			leftAmount, leftPresent := groupConsumerAmount(items[i], selectedModels)
			rightAmount, rightPresent := groupConsumerAmount(items[j], selectedModels)
			if leftPresent != rightPresent {
				return leftPresent
			}
			left, right = leftAmount, rightAmount
		case "requests":
			if items[i].RequestCount != items[j].RequestCount {
				if desc {
					return items[i].RequestCount > items[j].RequestCount
				}
				return items[i].RequestCount < items[j].RequestCount
			}
		case "name":
			leftName := strings.ToLower(items[i].SystemDisplayName)
			rightName := strings.ToLower(items[j].SystemDisplayName)
			if leftName != rightName {
				if desc {
					return leftName > rightName
				}
				return leftName < rightName
			}
		}
		if left != right {
			if desc {
				return left > right
			}
			return left < right
		}
		return items[i].ID < items[j].ID
	})
}

func groupConsumerAmount(item GroupListItem, models []string) (float64, bool) {
	if len(models) == 0 {
		return float64(item.AvgConsumerAmount), item.AvgConsumerAmount > 0
	}
	var sum int64
	count := int64(0)
	for _, target := range models {
		for model, amount := range item.AvgConsumerAmountByModel {
			if strings.EqualFold(strings.TrimSpace(model), strings.TrimSpace(target)) && amount > 0 {
				sum += amount
				count++
				break
			}
		}
	}
	if count == 0 {
		return 0, false
	}
	return float64(sum) / float64(count), true
}

func paginateGroups(items []GroupListItem, page, size int) []GroupListItem {
	start := (page - 1) * size
	if start >= len(items) {
		return []GroupListItem{}
	}
	end := start + size
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func containsSubstringFold(values []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), target) {
			return true
		}
	}
	return false
}
