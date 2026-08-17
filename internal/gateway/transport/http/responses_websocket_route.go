package http

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	responsesws "github.com/sh2001sh/new-api/internal/gateway/responsesws"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
)

func bindResponsesWebsocketRoute(c *gin.Context) error {
	session := responsesws.FromContext(c)
	if session == nil {
		return nil
	}
	if _, _, bound := session.Route(); bound {
		responsesws.ApplyRoutePin(c, session)
		return nil
	}
	channelID := httpctx.GetContextKeyInt(c, constant.ContextKeyChannelId)
	if channelID <= 0 {
		return errors.New("responses websocket channel was not selected")
	}
	channel, err := gatewaystore.LoadChannelByID(channelID, true)
	if err != nil {
		return err
	}
	keyIndex := httpctx.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
	if err := session.BindRoute(channelID, keyIndex, channel.ChannelInfo.ResponsesCapabilities.SupportsWebSocket()); err != nil {
		return err
	}
	responsesws.ApplyRoutePin(c, session)
	return nil
}
