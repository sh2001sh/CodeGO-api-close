package app

import (
	"sort"
	"strings"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
)

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

func filterAndSortGroups(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel, snapshots map[string]marketplaceschema.RankingSnapshot, query GroupQuery) []GroupListItem {
	items := make([]GroupListItem, 0, len(groups))
	for _, group := range groups {
		channel := channels[group.ChannelID]
		models := decodeModels(channel.DeclaredModels)
		if !matchesGroupQuery(group, channel, models, query) {
			continue
		}
		snapshot := snapshots[group.ID]
		items = append(items, groupListItem(group, channel, models, snapshot))
	}
	sortGroupItems(items, query.Sort, query.Direction)
	return items
}

func matchesGroupQuery(group marketplaceschema.Group, channel marketplaceschema.Channel, models []string, query GroupQuery) bool {
	search := strings.ToLower(strings.TrimSpace(query.Search))
	if search != "" {
		haystack := strings.ToLower(group.SystemDisplayName + " " + group.PublicSlug + " " + group.OwnerDisplayName + " " + publicSourceLabel(channel) + " " + strings.Join(models, " "))
		if !strings.Contains(haystack, search) {
			return false
		}
	}
	if query.Model != "" && !containsFold(models, query.Model) {
		return false
	}
	return channel.ID != ""
}

func groupListItem(group marketplaceschema.Group, channel marketplaceschema.Channel, models []string, snapshot marketplaceschema.RankingSnapshot) GroupListItem {
	return GroupListItem{
		ID: group.ID, PublicSlug: group.PublicSlug, SystemDisplayName: group.SystemDisplayName,
		OwnerDisplayName: group.OwnerDisplayName, SourceType: group.SourceType, SourceLabel: publicSourceLabel(channel),
		CreditPoolPolicy: group.CreditPoolPolicy,
		LifecycleStatus:  group.LifecycleStatus, VerificationStatus: group.VerificationStatus,
		VerificationDueAt: group.VerificationDueAt, Multiplier: group.Multiplier, Models: models,
		ModelVerificationResults: publicModelVerificationResults(channel.ModelVerificationResults),
		ModelConsistencyStatus:   channel.ModelConsistencyStatus,
		Rank:                     snapshot.Rank, Score: snapshot.Score, SuccessRate: snapshot.RawSuccessRate,
		WilsonSuccessRate: snapshot.WilsonSuccessRate, AvgTTFTMs: snapshot.AvgTTFTMs,
		AvgLatencyMs: snapshot.AvgLatencyMs, AvgTPS: snapshot.AvgTPS,
		RequestCount: snapshot.RequestCount, IndependentConsumers: snapshot.IndependentConsumers,
		Observing: snapshot.Observing, UpdatedAt: group.UpdatedAt,
	}
}

func publicSourceLabel(channel marketplaceschema.Channel) string {
	if channel.SourceLabelStatus != marketplacedomain.SourceLabelApproved {
		return ""
	}
	return strings.TrimSpace(channel.ApprovedSourceLabel)
}

func sortGroupItems(items []GroupListItem, field, direction string) {
	desc := direction != "asc"
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i].Score, items[j].Score
		switch field {
		case "success_rate":
			left, right = items[i].SuccessRate, items[j].SuccessRate
		case "ttft":
			left, right = items[i].AvgTTFTMs, items[j].AvgTTFTMs
		case "latency":
			left, right = items[i].AvgLatencyMs, items[j].AvgLatencyMs
		case "multiplier":
			left, right = items[i].Multiplier, items[j].Multiplier
		case "requests":
			if items[i].RequestCount != items[j].RequestCount {
				if desc {
					return items[i].RequestCount > items[j].RequestCount
				}
				return items[i].RequestCount < items[j].RequestCount
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
