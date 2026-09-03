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
		marketplaceRoute.POST("/groups/:id/invite", middleware.CriticalRateLimit(), CreateGroupInvite)
		marketplaceRoute.POST("/invites/accept", middleware.CriticalRateLimit(), AcceptGroupInvite)
		marketplaceRoute.POST("/groups/:id/feedback", middleware.CriticalRateLimit(), SubmitChannelFeedback)
		marketplaceRoute.GET("/auto-route-pool", GetAutoRoutePool)
		marketplaceRoute.PUT("/auto-route-pool", middleware.CriticalRateLimit(), UpdateAutoRoutePool)
		marketplaceRoute.GET("/route-pools", ListRoutePools)
		marketplaceRoute.POST("/route-pools", middleware.CriticalRateLimit(), CreateRoutePool)
		marketplaceRoute.GET("/route-pools/:id", GetRoutePool)
		marketplaceRoute.PUT("/route-pools/:id", middleware.CriticalRateLimit(), UpdateRoutePool)
		marketplaceRoute.DELETE("/route-pools/:id", middleware.CriticalRateLimit(), DeleteRoutePool)
		marketplaceRoute.POST("/batch-tests", middleware.CriticalRateLimit(), StartBatchTest)
		marketplaceRoute.GET("/batch-tests/:id", GetBatchTest)
		marketplaceRoute.POST("/channels", middleware.CriticalRateLimit(), CreateChannel)
		marketplaceRoute.POST("/channels/fetch-models", middleware.CriticalRateLimit(), FetchModels)
		marketplaceRoute.GET("/channels/mine", ListMyChannels)
		marketplaceRoute.GET("/channels/mine/logs", ListMyUsageLogs)
		marketplaceRoute.GET("/channels/mine/observability", ListMyObservability)
		marketplaceRoute.GET("/channels/mine/user-usage", ListMyChannelUserUsage)
		marketplaceRoute.POST("/channels/:id/user-multiplier", SetUserMultiplier)
		marketplaceRoute.GET("/channels/:id/user-usage/:userId/time-series", GetUserUsageTimeSeries)
		marketplaceRoute.POST("/channels/:id/batch-welfare", BatchWelfare)
		marketplaceRoute.GET("/channels/:id/time-range-multipliers", ListTimeRangeMultipliers)
		marketplaceRoute.POST("/channels/:id/time-range-multipliers", CreateTimeRangeMultiplier)
		marketplaceRoute.DELETE("/channels/:id/time-range-multipliers/:ruleId", DeleteTimeRangeMultiplier)
		marketplaceRoute.POST("/groups/:id/bargain-requests", CreateBargainRequest)
		marketplaceRoute.GET("/channels/mine/bargain-requests", ListMyBargainRequests)
		marketplaceRoute.POST("/channels/mine/bargain-requests/:id/resolve", middleware.CriticalRateLimit(), ResolveMyBargainRequest)
		marketplaceRoute.PATCH("/channels/:id", UpdateChannel)
		marketplaceRoute.DELETE("/channels/:id", DeleteChannel)
		marketplaceRoute.POST("/channels/:id/verify", middleware.CriticalRateLimit(), VerifyChannel)
		marketplaceRoute.POST("/channels/:id/detect", middleware.CriticalRateLimit(), DetectChannel)
		marketplaceRoute.POST("/channels/:id/test", middleware.CriticalRateLimit(), TestChannelConnectivity)
		marketplaceRoute.POST("/channels/:id/test/failed", middleware.CriticalRateLimit(), RetryFailedChannelConnectivity)
		marketplaceRoute.POST("/channels/:id/models/remove-failed", middleware.CriticalRateLimit(), RemoveFailedChannelModel)
		marketplaceRoute.POST("/channels/:id/verification/pause", PauseChannelVerification)
		marketplaceRoute.POST("/channels/:id/pause", PauseChannel)
		marketplaceRoute.POST("/channels/:id/resume", ResumeChannel)
		marketplaceRoute.POST("/channels/:id/user-block", SetChannelUserBlock)
	}

	adminRoute := apiRouter.Group("/marketplace/admin")
	adminRoute.Use(middleware.AdminAuth())
	{
		adminRoute.GET("/channels", ListAdminChannels)
		adminRoute.GET("/owner-income", ListAdminOwnerIncome)
		adminRoute.POST("/owner-income/release", middleware.CriticalRateLimit(), ReleaseAdminOwnerIncome)
		adminRoute.PATCH("/channels/:id", UpdateAdminChannel)
		adminRoute.POST("/channels/:id/verify", middleware.CriticalRateLimit(), VerifyAdminChannel)
		adminRoute.POST("/channels/:id/detect", middleware.CriticalRateLimit(), DetectAdminChannel)
		adminRoute.POST("/channels/:id/test", middleware.CriticalRateLimit(), TestAdminChannelConnectivity)
		adminRoute.POST("/channels/:id/test/failed", middleware.CriticalRateLimit(), RetryFailedAdminChannelConnectivity)
		adminRoute.POST("/channels/:id/models/remove-failed", middleware.CriticalRateLimit(), RemoveFailedAdminChannelModel)
		adminRoute.POST("/channels/:id/verification/pause", PauseAdminChannelVerification)
		adminRoute.POST("/channels/:id/pause", PauseAdminChannel)
		adminRoute.POST("/channels/:id/resume", ResumeAdminChannel)
		adminRoute.DELETE("/channels/:id", DeleteAdminChannel)
		adminRoute.POST("/channels/:id/review", middleware.CriticalRateLimit(), ReviewChannel)
	}
}
