package routepin

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
)

const contextKey = "gateway_route_pin"

type Pin struct {
	ChannelID int
	KeyIndex  int
}

func Attach(c *gin.Context, pin Pin) {
	if c == nil || pin.ChannelID <= 0 || pin.KeyIndex < 0 {
		return
	}
	c.Set(contextKey, pin)
	Apply(c, pin)
}

func FromContext(c *gin.Context) (Pin, bool) {
	if c == nil {
		return Pin{}, false
	}
	value, exists := c.Get(contextKey)
	if !exists {
		return Pin{}, false
	}
	pin, ok := value.(Pin)
	return pin, ok && pin.ChannelID > 0 && pin.KeyIndex >= 0
}

func Apply(c *gin.Context, pin Pin) {
	if c == nil || pin.ChannelID <= 0 {
		return
	}
	httpctx.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, strconv.Itoa(pin.ChannelID))
}

func KeyIndex(c *gin.Context, channelID int) (int, bool) {
	pin, ok := FromContext(c)
	if !ok || pin.ChannelID != channelID {
		return 0, false
	}
	return pin.KeyIndex, true
}

func Clear(c *gin.Context) {
	if c == nil {
		return
	}
	c.Set(contextKey, nil)
	httpctx.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, "")
}
