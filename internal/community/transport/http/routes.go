package http

import (
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/internal/platform/transport/http/middleware"
)

func RegisterCommunityRoutes(apiRouter *gin.RouterGroup) {
	resources := apiRouter.Group("/community-resources")
	resources.Use(middleware.UserAuth())
	{
		resources.GET("/config", getResourceConfig)
		resources.GET("", listApprovedResources)
		resources.GET("/mine", listMyResources)
		resources.POST("", middleware.CriticalRateLimit(), createResource)
	}

	admin := apiRouter.Group("/admin/community-resources")
	admin.Use(middleware.AdminAuth())
	{
		admin.GET("", listAdminResources)
		admin.PUT("/config", middleware.CriticalRateLimit(), updateResourceConfig)
		admin.PATCH("/:id", middleware.CriticalRateLimit(), reviewResource)
	}
}
