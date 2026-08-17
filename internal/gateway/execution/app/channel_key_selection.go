package app

import (
	"github.com/gin-gonic/gin"
	routepin "github.com/sh2001sh/new-api/internal/gateway/routepin"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	"github.com/sh2001sh/new-api/types"
)

const requestMultiKeyUsageContextKey = "request_multi_key_usage"

type requestMultiKeyUsage map[int]map[int]struct{}

func selectChannelKeyForRequest(c *gin.Context, channel *gatewayschema.Channel) (string, int, *types.NewAPIError) {
	if pinnedIndex, pinned := routepin.KeyIndex(c, channel.Id); pinned {
		key, apiErr := gatewaystore.GetEnabledChannelKeyByIndex(channel, pinnedIndex)
		return key, pinnedIndex, apiErr
	}
	if c != nil && channel.ChannelInfo.IsMultiKey && gatewayruntime.IsSingleChannelRoute(c) {
		if index, found := soleUsedKeyIndex(multiKeyUsageFromContext(c)[channel.Id]); found {
			key, apiErr := gatewaystore.GetEnabledChannelKeyByIndex(channel, index)
			return key, index, apiErr
		}
	}

	key, index, apiErr := gatewaystore.GetNextEnabledChannelKey(channel)
	if apiErr != nil || c == nil || !channel.ChannelInfo.IsMultiKey {
		return key, index, apiErr
	}

	usage := multiKeyUsageFromContext(c)
	usedIndexes := usage[channel.Id]
	if usedIndexes == nil {
		usedIndexes = make(map[int]struct{})
		usage[channel.Id] = usedIndexes
	}
	if _, alreadyUsed := usedIndexes[index]; alreadyUsed {
		if rotatedKey, rotatedIndex, found := nextUnusedEnabledChannelKey(channel, usedIndexes); found {
			key = rotatedKey
			index = rotatedIndex
		}
	}
	usedIndexes[index] = struct{}{}
	return key, index, nil
}

func soleUsedKeyIndex(indexes map[int]struct{}) (int, bool) {
	if len(indexes) != 1 {
		return 0, false
	}
	for index := range indexes {
		return index, true
	}
	return 0, false
}

func multiKeyUsageFromContext(c *gin.Context) requestMultiKeyUsage {
	if value, found := c.Get(requestMultiKeyUsageContextKey); found {
		if usage, ok := value.(requestMultiKeyUsage); ok && usage != nil {
			return usage
		}
	}
	usage := make(requestMultiKeyUsage)
	c.Set(requestMultiKeyUsageContextKey, usage)
	return usage
}

func nextUnusedEnabledChannelKey(channel *gatewayschema.Channel, usedIndexes map[int]struct{}) (string, int, bool) {
	for index := range channel.GetKeys() {
		if _, used := usedIndexes[index]; used {
			continue
		}
		key, apiErr := gatewaystore.GetEnabledChannelKeyByIndex(channel, index)
		if apiErr == nil {
			return key, index, true
		}
	}
	return "", 0, false
}
