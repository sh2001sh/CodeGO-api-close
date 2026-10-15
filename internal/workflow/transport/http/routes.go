package http

import (
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/internal/platform/transport/http/middleware"
)

func RegisterWorkflowRoutes(apiRouter *gin.RouterGroup) {
	apiRouter.GET("/gene-map/share/:token", GetGeneMapShare)

	taskRoute := apiRouter.Group("/task")
	{
		taskRoute.GET("/self", middleware.UserAuth(), GetUserTask)
		taskRoute.GET("/", middleware.AdminAuth(), GetAllTask)
	}

	miniProgramRoute := apiRouter.Group("/miniprogram")
	{
		miniProgramRoute.POST("/session", middleware.CriticalRateLimit(), MiniProgramSession)

		miniProgramAuthedRoute := miniProgramRoute.Group("/")
		miniProgramAuthedRoute.Use(middleware.MiniProgramAuth())
		{
			miniProgramAuthedRoute.GET("/me", GetMiniProgramMe)
			miniProgramAuthedRoute.POST("/bind", middleware.CriticalRateLimit(), BindMiniProgram)
			miniProgramAuthedRoute.POST("/unbind", UnbindMiniProgram)
			miniProgramAuthedRoute.POST("/share-check", middleware.CriticalRateLimit(), CheckMiniProgramShareContent)
		}

		miniProgramBoundRoute := miniProgramRoute.Group("/")
		miniProgramBoundRoute.Use(middleware.MiniProgramAuth(), middleware.MiniProgramBoundAuth())
		{
			miniProgramBoundRoute.GET("/dashboard", GetMiniProgramDashboard)
			miniProgramBoundRoute.GET("/logs", GetMiniProgramLogs)
			miniProgramBoundRoute.GET("/stat", GetMiniProgramStat)
			miniProgramBoundRoute.GET("/gene-map", GetMiniProgramGeneMap)
		}
	}
}
