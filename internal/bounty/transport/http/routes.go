package http

import (
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/internal/platform/transport/http/middleware"
)

func RegisterBountyRoutes(apiRouter *gin.RouterGroup) {
	readRoute := apiRouter.Group("/bounties")
	{
		readRoute.GET("", listBounties)
		readRoute.GET("/mine", middleware.UserAuth(), listMineBounties)
		readRoute.GET("/balances", middleware.UserAuth(), listBountyBalances)
		readRoute.GET("/notifications", middleware.UserAuth(), listBountyNotifications)
		readRoute.POST("/notifications/:notification_id/read", middleware.UserAuth(), middleware.CriticalRateLimit(), markBountyNotificationRead)
		readRoute.POST("/notifications/read-all", middleware.UserAuth(), middleware.CriticalRateLimit(), markAllBountyNotificationsRead)
		readRoute.GET("/:id", getBountyDetail)
		readRoute.GET("/:id/timeline", getBountyTimeline)

		writeRoute := readRoute.Group("")
		writeRoute.Use(middleware.UserAuth())
		writeRoute.Use(middleware.CriticalRateLimit())
		{
			writeRoute.POST("", createBounty)
			writeRoute.POST("/drafts", saveBountyDraft)
			writeRoute.PUT("/:id/draft", updateBountyDraft)
			writeRoute.POST("/:id/draft/publish", publishBountyDraft)
			writeRoute.POST("/:id/applications", applyBounty)
			writeRoute.POST("/:id/assignment", assignBounty)
			writeRoute.POST("/:id/start", startBounty)
			writeRoute.POST("/:id/cancel", cancelBounty)
			writeRoute.POST("/:id/material-requests", createMaterialRequest)
			writeRoute.POST("/:id/material-requests/:request_id/replies", replyMaterialRequest)
			writeRoute.POST("/:id/material-requests/:request_id/resolve", resolveMaterialRequest)
			writeRoute.POST("/:id/material-requests/:request_id/timeout", handleMaterialTimeout)
			writeRoute.POST("/:id/submissions", submitBountyDelivery)
			writeRoute.POST("/:id/review", reviewBounty)
			writeRoute.POST("/:id/disputes", openBountyDispute)
			writeRoute.POST("/:id/reports", reportBounty)
		}
	}

	adminRoute := apiRouter.Group("/admin/bounties")
	adminRoute.Use(middleware.AdminAuth())
	{
		adminRoute.GET("", listAdminBounties)
		adminRoute.GET("/disputes", listAdminBountyDisputes)
		adminRoute.GET("/reports", listAdminBountyReports)
		adminRoute.POST("/:id/resolve", middleware.CriticalRateLimit(), resolveAdminBountyDispute)
		adminRoute.POST("/:id/suspend", middleware.CriticalRateLimit(), suspendBounty)
		adminRoute.POST("/:id/resume", middleware.CriticalRateLimit(), resumeBounty)
		adminRoute.POST("/:id/reports/:report_id/resolve", middleware.CriticalRateLimit(), resolveBountyReport)
	}
}
