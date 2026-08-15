package http

import (
	"github.com/gin-gonic/gin"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"github.com/sh2001sh/new-api/internal/platform/transport/http/middleware"
)

func RegisterMarketplaceRoutes(apiRouter *gin.RouterGroup) {
	if err := marketplaceapp.ReconcileMarketplaceChannels(); err != nil {
		platformobservability.SysError("reconcile marketplace channels: " + err.Error())
	}
	marketplaceRoute := apiRouter.Group("/marketplace")
	marketplaceRoute.Use(middleware.UserAuth())
	{
		marketplaceRoute.GET("/groups", ListGroups)
		marketplaceRoute.GET("/groups/:slug", GetGroup)
		marketplaceRoute.POST("/groups/:id/bind-token", middleware.CriticalRateLimit(), BindToken)
		marketplaceRoute.POST("/channels", middleware.CriticalRateLimit(), CreateChannel)
		marketplaceRoute.POST("/channels/fetch-models", middleware.CriticalRateLimit(), FetchModels)
		marketplaceRoute.GET("/channels/mine", ListMyChannels)
		marketplaceRoute.PATCH("/channels/:id", UpdateChannel)
		marketplaceRoute.POST("/channels/:id/verify", middleware.CriticalRateLimit(), VerifyChannel)
		marketplaceRoute.POST("/channels/:id/pause", PauseChannel)
		marketplaceRoute.POST("/channels/:id/resume", ResumeChannel)
	}

	adminRoute := apiRouter.Group("/marketplace/admin")
	adminRoute.Use(middleware.AdminAuth())
	{
		adminRoute.GET("/channels", ListAdminChannels)
		adminRoute.PATCH("/channels/:id", UpdateAdminChannel)
		adminRoute.POST("/channels/:id/review", middleware.CriticalRateLimit(), ReviewChannel)
	}
}
