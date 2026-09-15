package app

import (
	"strings"
	"time"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

type GroupModelRequestStatus struct {
	Model                      string                `json:"model"`
	RequestCount               int64                 `json:"request_count"`
	SuccessRate                float64               `json:"success_rate"`
	RecentRequestBucketSeconds int64                 `json:"recent_request_bucket_seconds"`
	RecentRequestSeries        []RecentRequestBucket `json:"recent_request_series"`
}

// GetMarketplaceGroupModelStatus shares the overview's pre-aggregated metrics.
// Visibility is checked before reading statistics, including on cache hits.
func GetMarketplaceGroupModelStatus(slug string, viewerUserID int) ([]GroupModelRequestStatus, error) {
	if strings.HasPrefix(slug, officialAutoRoutePrefix) {
		return getOfficialGroupModelStatus(strings.TrimPrefix(slug, officialAutoRoutePrefix), viewerUserID)
	}
	var group marketplaceschema.Group
	if err := publicGroupsQuery(GroupQuery{ViewerUserID: viewerUserID, IncludeAccess: viewerUserID > 0}).
		Where("public_slug = ?", slug).First(&group).Error; err != nil {
		return nil, err
	}
	var channel marketplaceschema.Channel
	if err := platformdb.DB.Select("id, declared_models").Where("id = ?", group.ChannelID).First(&channel).Error; err != nil {
		return nil, err
	}
	start, _ := marketplaceRecentWindow(time.Now().Unix())
	var rows []auditprojection.GroupModelSeries
	groupName := strings.TrimSpace(group.InternalGroupName)
	if groupName != "" && platformdb.DB != nil && platformdb.LogDB != nil {
		var err error
		rows, err = auditprojection.QuerySeriesByGroupModels(marketplaceRecentWindowHours, []string{groupName})
		if err != nil {
			return nil, err
		}
	}
	return buildGroupModelRequestStatus(start, groupName, decodeModels(channel.DeclaredModels), rows), nil
}

func buildGroupModelRequestStatus(start int64, groupName string, models []string, rows []auditprojection.GroupModelSeries) []GroupModelRequestStatus {
	result := make([]GroupModelRequestStatus, 0, len(models))
	indices := make(map[string]int, len(models))
	for _, model := range models {
		if _, exists := indices[model]; exists {
			continue
		}
		indices[model] = len(result)
		result = append(result, GroupModelRequestStatus{Model: model, RecentRequestBucketSeconds: marketplaceRecentBucketSeconds, RecentRequestSeries: newMarketplaceRecentRequestSeries(start)})
	}
	weightedSuccess := make([][]float64, len(result))
	for index := range weightedSuccess {
		weightedSuccess[index] = make([]float64, marketplaceRecentWindowSegments)
	}
	for _, row := range rows {
		index, exists := indices[row.ModelName]
		if !exists || row.Group != groupName {
			continue
		}
		for _, point := range row.Series {
			bucketIndex := (point.Ts - start) / marketplaceRecentBucketSeconds
			if point.Ts < start || bucketIndex < 0 || bucketIndex >= marketplaceRecentWindowSegments || point.RequestCount <= 0 {
				continue
			}
			result[index].RecentRequestSeries[bucketIndex].RequestCount += point.RequestCount
			weightedSuccess[index][bucketIndex] += point.SuccessRate * float64(point.RequestCount)
		}
	}
	for index := range result {
		var totalWeightedSuccess float64
		for bucketIndex := range result[index].RecentRequestSeries {
			bucket := &result[index].RecentRequestSeries[bucketIndex]
			if bucket.RequestCount > 0 {
				bucket.SuccessRate = round2(weightedSuccess[index][bucketIndex] / float64(bucket.RequestCount))
			}
			result[index].RequestCount += bucket.RequestCount
			totalWeightedSuccess += weightedSuccess[index][bucketIndex]
		}
		if result[index].RequestCount > 0 {
			result[index].SuccessRate = round2(totalWeightedSuccess / float64(result[index].RequestCount))
		}
	}
	return result
}
