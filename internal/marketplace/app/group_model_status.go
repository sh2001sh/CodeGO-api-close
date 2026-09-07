package app

import (
	"strings"
	"time"

	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
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

// GetMarketplaceGroupModelStatus shares the overview's cached group/model scan.
// Visibility is checked before reading statistics, including on cache hits.
func GetMarketplaceGroupModelStatus(slug string, viewerUserID int) ([]GroupModelRequestStatus, error) {
	var group marketplaceschema.Group
	if err := publicGroupsQuery(GroupQuery{ViewerUserID: viewerUserID, IncludeAccess: viewerUserID > 0}).
		Where("public_slug = ?", slug).First(&group).Error; err != nil {
		return nil, err
	}
	var channel marketplaceschema.Channel
	if err := platformdb.DB.Select("id, declared_models").Where("id = ?", group.ChannelID).First(&channel).Error; err != nil {
		return nil, err
	}
	start, end := marketplaceRecentWindow(time.Now().Unix())
	var rows []gatewaystore.GroupModelRequestBucket
	groupName := strings.TrimSpace(group.InternalGroupName)
	if groupName != "" && platformdb.LogDB != nil {
		var err error
		rows, err = gatewaystore.LoadGroupModelRequestBuckets(start, end, marketplaceRecentBucketSeconds, []string{groupName})
		if err != nil {
			return nil, err
		}
	}
	return buildGroupModelRequestStatus(start, groupName, decodeModels(channel.DeclaredModels), rows), nil
}

func buildGroupModelRequestStatus(start int64, groupName string, models []string, rows []gatewaystore.GroupModelRequestBucket) []GroupModelRequestStatus {
	result := make([]GroupModelRequestStatus, 0, len(models))
	indices := make(map[string]int, len(models))
	for _, model := range models {
		if _, exists := indices[model]; exists {
			continue
		}
		indices[model] = len(result)
		result = append(result, GroupModelRequestStatus{Model: model, RecentRequestBucketSeconds: marketplaceRecentBucketSeconds, RecentRequestSeries: newMarketplaceRecentRequestSeries(start)})
	}
	successes := make([][marketplaceRecentWindowSegments]int64, len(result))
	for _, row := range rows {
		index, exists := indices[row.ModelName]
		if !exists || row.GroupName != groupName || row.BucketIndex < 0 || row.BucketIndex >= marketplaceRecentWindowSegments || row.RequestCount <= 0 {
			continue
		}
		result[index].RecentRequestSeries[row.BucketIndex].RequestCount += row.RequestCount
		successes[index][row.BucketIndex] += row.SuccessCount
	}
	for index := range result {
		var totalSuccess int64
		for bucketIndex := range result[index].RecentRequestSeries {
			bucket := &result[index].RecentRequestSeries[bucketIndex]
			if bucket.RequestCount > 0 {
				bucket.SuccessRate = round2(float64(successes[index][bucketIndex]) / float64(bucket.RequestCount) * 100)
			}
			result[index].RequestCount += bucket.RequestCount
			totalSuccess += successes[index][bucketIndex]
		}
		if result[index].RequestCount > 0 {
			result[index].SuccessRate = round2(float64(totalSuccess) / float64(result[index].RequestCount) * 100)
		}
	}
	return result
}
