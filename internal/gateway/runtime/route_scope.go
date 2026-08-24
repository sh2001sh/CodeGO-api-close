package runtime

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	singleChannelRouteContextKey        = "single_channel_route"
	remainingCrossGroupRoutesContextKey = "remaining_cross_group_routes"
)

// MarkSingleChannelRoute records whether the selected group/model has no
// alternative channel. Multiple keys on the selected channel do not change
// this classification.
func MarkSingleChannelRoute(c *gin.Context, single bool) {
	if c != nil {
		c.Set(singleChannelRouteContextKey, single)
	}
}

func IsSingleChannelRoute(c *gin.Context) bool {
	return c != nil && c.GetBool(singleChannelRouteContextKey)
}

// MarkRemainingCrossGroupRoutes records how many later groups can still be
// selected without replaying client-visible output.
func MarkRemainingCrossGroupRoutes(c *gin.Context, remaining int) {
	if c == nil {
		return
	}
	if remaining < 0 {
		remaining = 0
	}
	c.Set(remainingCrossGroupRoutesContextKey, remaining)
}

// HasRemainingCrossGroupRoute reports whether the current automatic route can
// still move to a later group before client-visible output is committed.
func HasRemainingCrossGroupRoute(c *gin.Context) bool {
	return c != nil && c.GetInt(remainingCrossGroupRoutesContextKey) > 0
}

// SingleUsedChannelID returns the channel ID when every recorded attempt used
// the same channel. Repeated attempts and multi-key selection remain one route.
func SingleUsedChannelID(c *gin.Context) (int, bool) {
	if c == nil {
		return 0, false
	}
	channelID := 0
	for _, raw := range c.GetStringSlice("use_channel") {
		candidate, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || candidate <= 0 {
			continue
		}
		if channelID == 0 {
			channelID = candidate
			continue
		}
		if channelID != candidate {
			return 0, false
		}
	}
	return channelID, channelID > 0
}
