package responsesws

import (
	"github.com/gin-gonic/gin"
	routepin "github.com/sh2001sh/new-api/internal/gateway/routepin"
)

const sessionContextKey = "responses_upstream_websocket_session"

func Attach(c *gin.Context, session *Session) {
	if c != nil && session != nil {
		c.Set(sessionContextKey, session)
	}
}

func FromContext(c *gin.Context) *Session {
	if c == nil {
		return nil
	}
	value, exists := c.Get(sessionContextKey)
	if !exists {
		return nil
	}
	session, _ := value.(*Session)
	return session
}

func ApplyRoutePin(c *gin.Context, session *Session) {
	if c == nil || session == nil {
		return
	}
	channelID, keyIndex, bound := session.Route()
	if !bound {
		return
	}
	routepin.Attach(c, routepin.Pin{ChannelID: channelID, KeyIndex: keyIndex})
}

func PinnedKeyIndex(c *gin.Context, channelID int) (int, bool) {
	return routepin.KeyIndex(c, channelID)
}
