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
	publicMarketplaceRoute := apiRouter.Group("/marketplace")
	publicMarketplaceRoute.Use(middleware.HeaderNavModulePublicOrUserAuth("pricing"))
	{
		publicMarketplaceRoute.GET("/groups", ListGroups)
		publicMarketplaceRoute.GET("/multiplier-trends", ListMultiplierTrends)
		publicMarketplaceRoute.GET("/groups/:slug", GetGroup)
	}

	marketplaceRoute := apiRouter.Group("/marketplace")
	marketplaceRoute.Use(middleware.UserAuth())
	{
		marketplaceRoute.POST("/groups/:id/bind-token", middleware.CriticalRateLimit(), BindToken)
		marketplaceRoute.POST("/groups/:id/feedback", middleware.CriticalRateLimit(), SubmitChannelFeedback)
		marketplaceRoute.GET("/auto-route-pool", GetAutoRoutePool)
		marketplaceRoute.PUT("/auto-route-pool", middleware.CriticalRateLimit(), UpdateAutoRoutePool)
		marketplaceRoute.POST("/channels", middleware.CriticalRateLimit(), CreateChannel)
		marketplaceRoute.POST("/channels/fetch-models", middleware.CriticalRateLimit(), FetchModels)
		marketplaceRoute.GET("/channels/mine", ListMyChannels)
		marketplaceRoute.GET("/channels/mine/logs", ListMyUsageLogs)
		marketplaceRoute.PATCH("/channels/:id", UpdateChannel)
		marketplaceRoute.DELETE("/channels/:id", DeleteChannel)
		marketplaceRoute.POST("/channels/:id/verify", middleware.CriticalRateLimit(), VerifyChannel)
		marketplaceRoute.POST("/channels/:id/detect", middleware.CriticalRateLimit(), DetectChannel)
		marketplaceRoute.POST("/channels/:id/test", middleware.CriticalRateLimit(), TestChannelConnectivity)
		marketplaceRoute.POST("/channels/:id/verification/pause", PauseChannelVerification)
		marketplaceRoute.POST("/channels/:id/pause", PauseChannel)
		marketplaceRoute.POST("/channels/:id/resume", ResumeChannel)
	}

	adminRoute := apiRouter.Group("/marketplace/admin")
	adminRoute.Use(middleware.AdminAuth())
	{
		adminRoute.GET("/channels", ListAdminChannels)
		adminRoute.GET("/owner-income", ListAdminOwnerIncome)
		adminRoute.PATCH("/channels/:id", UpdateAdminChannel)
		adminRoute.POST("/channels/:id/verify", middleware.CriticalRateLimit(), VerifyAdminChannel)
		adminRoute.POST("/channels/:id/detect", middleware.CriticalRateLimit(), DetectAdminChannel)
		adminRoute.POST("/channels/:id/test", middleware.CriticalRateLimit(), TestAdminChannelConnectivity)
		adminRoute.POST("/channels/:id/verification/pause", PauseAdminChannelVerification)
		adminRoute.DELETE("/channels/:id", DeleteAdminChannel)
		adminRoute.POST("/channels/:id/review", middleware.CriticalRateLimit(), ReviewChannel)
	}
}
