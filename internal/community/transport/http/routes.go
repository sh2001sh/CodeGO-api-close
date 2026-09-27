package http

import (
	"github.com/gin-gonic/gin"
)

func RegisterCommunityRoutes(apiRouter *gin.RouterGroup) {
	bridge := apiRouter.Group("/community/v1")
	bridge.Use(communityServiceAuth())
	{
		bridge.GET("/sellers", listCommunitySellers)
		bridge.GET("/members/:sub", getCommunityMember)
		bridge.GET("/members/:sub/channels", listCommunityMemberChannels)
		bridge.PUT("/channels/:id/rating", rateCommunityChannel)
	}

}
