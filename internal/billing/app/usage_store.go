package app

import (
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
)

func RecordUsageStats(userID int, channelID int, quota int) {
	if quota <= 0 {
		return
	}
	identitystore.UpdateUserUsedQuotaAndRequestCount(userID, quota)
	gatewaystore.UpdateChannelUsedQuota(channelID, quota)
}

// AdjustUsageQuotaStats adjusts accumulated quota without changing request count.
func AdjustUsageQuotaStats(userID int, channelID int, quotaDelta int) {
	if quotaDelta == 0 {
		return
	}
	identitystore.UpdateUserUsedQuota(userID, quotaDelta)
	gatewaystore.UpdateChannelUsedQuota(channelID, quotaDelta)
}
