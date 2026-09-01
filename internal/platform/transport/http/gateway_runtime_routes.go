package http

import (
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayhttp "github.com/sh2001sh/new-api/internal/gateway/transport/http"
	identityhttp "github.com/sh2001sh/new-api/internal/identity/transport/http"
	"github.com/sh2001sh/new-api/internal/platform/transport/http/middleware"
	workflowhttp "github.com/sh2001sh/new-api/internal/workflow/transport/http"
	"github.com/sh2001sh/new-api/types"
)

func RegisterGatewayRuntimeRoutes(router *gin.Engine) {
	router.Use(middleware.CORS())
	router.Use(middleware.DecompressRequestMiddleware())
	router.Use(middleware.BodyStorageCleanup())
	router.Use(middleware.StatsMiddleware())

	registerRelayModelRoutes(router)
	registerRelayPlaygroundRoutes(router)
	registerRelayCoreRoutes(router)
	registerRelayCompatibilityRoutes(router)
	registerRelayTaskRoutes(router)
}

func registerRelayCompatibilityRoutes(router *gin.Engine) {
	websocketCompatibility := router.Group("")
	websocketCompatibility.Use(middleware.RouteTag("relay"))
	websocketCompatibility.Use(middleware.SystemPerformanceCheck())
	websocketCompatibility.Use(middleware.TokenAuth())
	{
		websocketCompatibility.GET("/responses", gatewayhttp.ResponsesWebsocket)
		websocketCompatibility.GET("/backend-api/codex/responses", gatewayhttp.ResponsesWebsocket)
		websocketCompatibility.GET("/responses/:id", gatewayhttp.GetResponsesBackground)
		websocketCompatibility.POST("/responses/:id/cancel", gatewayhttp.CancelResponsesBackground)
		websocketCompatibility.GET("/backend-api/codex/responses/:id", gatewayhttp.GetResponsesBackground)
		websocketCompatibility.POST("/backend-api/codex/responses/:id/cancel", gatewayhttp.CancelResponsesBackground)
	}

	compatibility := router.Group("")
	compatibility.Use(middleware.RouteTag("relay"))
	compatibility.Use(middleware.SystemPerformanceCheck())
	compatibility.Use(middleware.TokenAuth())
	compatibility.Use(middleware.ModelRequestRateLimit())
	compatibility.Use(middleware.Distribute())
	{
		compatibility.POST("/responses", gatewayhttp.ResponsesCreateWithCanonicalPath("/v1/responses"))
		compatibility.POST("/responses/compact", gatewayhttp.RelayWithCanonicalPath("/v1/responses/compact", types.RelayFormatOpenAIResponsesCompaction))
		compatibility.POST("/alpha/search", gatewayhttp.RelayWithCanonicalPath("/v1/alpha/search", types.RelayFormatOpenAIAlphaSearch))
		compatibility.POST("/backend-api/codex/responses", gatewayhttp.ResponsesCreateWithCanonicalPath("/v1/responses"))
		compatibility.POST("/backend-api/codex/responses/compact", gatewayhttp.RelayWithCanonicalPath("/v1/responses/compact", types.RelayFormatOpenAIResponsesCompaction))
		compatibility.POST("/backend-api/codex/alpha/search", gatewayhttp.RelayWithCanonicalPath("/v1/alpha/search", types.RelayFormatOpenAIAlphaSearch))
	}
}

func registerRelayModelRoutes(router *gin.Engine) {
	modelsRouter := router.Group("/v1/models")
	modelsRouter.Use(middleware.RouteTag("relay"))
	modelsRouter.Use(middleware.TokenAuth())
	{
		modelsRouter.GET("", func(c *gin.Context) {
			switch {
			case c.GetHeader("x-api-key") != "" && c.GetHeader("anthropic-version") != "":
				gatewayhttp.ListModels(c, constant.ChannelTypeAnthropic)
			case c.GetHeader("x-goog-api-key") != "" || c.Query("key") != "":
				gatewayhttp.RetrieveModel(c, constant.ChannelTypeGemini)
			default:
				gatewayhttp.ListModels(c, constant.ChannelTypeOpenAI)
			}
		})

		modelsRouter.GET("/:model", func(c *gin.Context) {
			switch {
			case c.GetHeader("x-api-key") != "" && c.GetHeader("anthropic-version") != "":
				gatewayhttp.RetrieveModel(c, constant.ChannelTypeAnthropic)
			default:
				gatewayhttp.RetrieveModel(c, constant.ChannelTypeOpenAI)
			}
		})
	}

	geminiRouter := router.Group("/v1beta/models")
	geminiRouter.Use(middleware.RouteTag("relay"))
	geminiRouter.Use(middleware.TokenAuth())
	{
		geminiRouter.GET("", func(c *gin.Context) {
			gatewayhttp.ListModels(c, constant.ChannelTypeGemini)
		})
	}

	geminiCompatibleRouter := router.Group("/v1beta/openai/models")
	geminiCompatibleRouter.Use(middleware.RouteTag("relay"))
	geminiCompatibleRouter.Use(middleware.TokenAuth())
	{
		geminiCompatibleRouter.GET("", func(c *gin.Context) {
			gatewayhttp.ListModels(c, constant.ChannelTypeOpenAI)
		})
	}
}

func registerRelayPlaygroundRoutes(router *gin.Engine) {
	playgroundRouter := router.Group("/pg")
	playgroundRouter.Use(middleware.RouteTag("relay"))
	playgroundRouter.Use(middleware.SystemPerformanceCheck())
	playgroundRouter.Use(middleware.UserAuth(), middleware.Distribute())
	{
		playgroundRouter.POST("/chat/completions", gatewayhttp.Playground)
		playgroundRouter.POST("/images/generations", gatewayhttp.PlaygroundImage)
		playgroundRouter.POST("/images/edits", gatewayhttp.PlaygroundImage)
	}
}

func registerRelayCoreRoutes(router *gin.Engine) {
	balanceRouter := router.Group("/v1/dashboard")
	balanceRouter.Use(middleware.RouteTag("relay"))
	balanceRouter.Use(middleware.SystemPerformanceCheck())
	balanceRouter.Use(middleware.TokenAuth())
	{
		balanceRouter.GET("/balance", middleware.DisableCache(), identityhttp.GetTokenAccountBalance)
	}
	responsesWebsocketRouter := router.Group("/v1")
	responsesWebsocketRouter.Use(middleware.RouteTag("relay"))
	responsesWebsocketRouter.Use(middleware.SystemPerformanceCheck())
	responsesWebsocketRouter.Use(middleware.TokenAuth())
	{
		responsesWebsocketRouter.GET("/responses", gatewayhttp.ResponsesWebsocket)
		responsesWebsocketRouter.GET("/responses/:id", gatewayhttp.GetResponsesBackground)
		responsesWebsocketRouter.POST("/responses/:id/cancel", gatewayhttp.CancelResponsesBackground)
	}
	fileDeliveryRouter := router.Group("/v1/files")
	fileDeliveryRouter.Use(middleware.RouteTag("relay"))
	{
		fileDeliveryRouter.GET("/:id/delivery", gatewayhttp.DeliverFile)
	}
	filesRouter := router.Group("/v1")
	filesRouter.Use(middleware.RouteTag("relay"))
	filesRouter.Use(middleware.SystemPerformanceCheck())
	filesRouter.Use(middleware.TokenAuth())
	{
		filesRouter.GET("/files", gatewayhttp.ListFiles)
		filesRouter.POST("/files", gatewayhttp.CreateFile)
		filesRouter.DELETE("/files/:id", gatewayhttp.DeleteFile)
		filesRouter.GET("/files/:id", gatewayhttp.GetFile)
		filesRouter.GET("/files/:id/content", gatewayhttp.GetFileContent)
	}

	relayV1Router := router.Group("/v1")
	relayV1Router.Use(middleware.RouteTag("relay"))
	relayV1Router.Use(middleware.SystemPerformanceCheck())
	relayV1Router.Use(middleware.TokenAuth())
	relayV1Router.Use(middleware.ModelRequestRateLimit())
	{
		wsRouter := relayV1Router.Group("")
		wsRouter.Use(middleware.Distribute())
		wsRouter.GET("/realtime", gatewayhttp.RelayWithFormat(types.RelayFormatOpenAIRealtime))
	}
	{
		httpRouter := relayV1Router.Group("")
		httpRouter.Use(middleware.Distribute())

		httpRouter.POST("/messages", gatewayhttp.RelayWithFormat(types.RelayFormatClaude))
		httpRouter.POST("/completions", gatewayhttp.RelayWithFormat(types.RelayFormatOpenAI))
		httpRouter.POST("/chat/completions", gatewayhttp.RelayWithFormat(types.RelayFormatOpenAI))
		httpRouter.POST("/responses", gatewayhttp.ResponsesCreate)
		httpRouter.POST("/responses/compact", gatewayhttp.RelayWithFormat(types.RelayFormatOpenAIResponsesCompaction))
		httpRouter.POST("/alpha/search", gatewayhttp.RelayWithFormat(types.RelayFormatOpenAIAlphaSearch))
		httpRouter.POST("/edits", gatewayhttp.RelayWithFormat(types.RelayFormatOpenAIImage))
		httpRouter.POST("/images/generations", gatewayhttp.RelayWithFormat(types.RelayFormatOpenAIImage))
		httpRouter.POST("/images/edits", gatewayhttp.RelayWithFormat(types.RelayFormatOpenAIImage))
		httpRouter.POST("/embeddings", gatewayhttp.RelayWithFormat(types.RelayFormatEmbedding))
		httpRouter.POST("/audio/transcriptions", gatewayhttp.RelayWithFormat(types.RelayFormatOpenAIAudio))
		httpRouter.POST("/audio/translations", gatewayhttp.RelayWithFormat(types.RelayFormatOpenAIAudio))
		httpRouter.POST("/audio/speech", gatewayhttp.RelayWithFormat(types.RelayFormatOpenAIAudio))
		httpRouter.POST("/rerank", gatewayhttp.RelayWithFormat(types.RelayFormatRerank))
		httpRouter.POST("/engines/:model/embeddings", gatewayhttp.RelayWithFormat(types.RelayFormatGemini))
		httpRouter.POST("/models/*path", gatewayhttp.RelayWithFormat(types.RelayFormatGemini))
		httpRouter.POST("/moderations", gatewayhttp.RelayWithFormat(types.RelayFormatOpenAI))

		httpRouter.POST("/images/variations", gatewayhttp.RelayNotImplemented)
		httpRouter.POST("/fine-tunes", gatewayhttp.RelayNotImplemented)
		httpRouter.GET("/fine-tunes", gatewayhttp.RelayNotImplemented)
		httpRouter.GET("/fine-tunes/:id", gatewayhttp.RelayNotImplemented)
		httpRouter.POST("/fine-tunes/:id/cancel", gatewayhttp.RelayNotImplemented)
		httpRouter.GET("/fine-tunes/:id/events", gatewayhttp.RelayNotImplemented)
		httpRouter.DELETE("/models/:model", gatewayhttp.RelayNotImplemented)
	}

	relayGeminiRouter := router.Group("/v1beta")
	relayGeminiRouter.Use(middleware.RouteTag("relay"))
	relayGeminiRouter.Use(middleware.SystemPerformanceCheck())
	relayGeminiRouter.Use(middleware.TokenAuth())
	relayGeminiRouter.Use(middleware.ModelRequestRateLimit())
	relayGeminiRouter.Use(middleware.Distribute())
	{
		relayGeminiRouter.POST("/models/*path", gatewayhttp.RelayWithFormat(types.RelayFormatGemini))
	}
}

func registerRelayTaskRoutes(router *gin.Engine) {
	relaySunoRouter := router.Group("/suno")
	relaySunoRouter.Use(middleware.RouteTag("relay"))
	relaySunoRouter.Use(middleware.SystemPerformanceCheck())
	relaySunoRouter.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		relaySunoRouter.POST("/submit/:action", workflowhttp.SubmitRelayTask)
		relaySunoRouter.POST("/fetch", workflowhttp.FetchRelayTask)
		relaySunoRouter.GET("/fetch/:id", workflowhttp.FetchRelayTask)
	}

	videoProxyRouter := router.Group("/v1")
	videoProxyRouter.Use(middleware.RouteTag("relay"))
	videoProxyRouter.Use(middleware.TokenOrUserAuth())
	{
		videoProxyRouter.GET("/videos/:task_id/content", workflowhttp.VideoProxy)
	}

	videoV1Router := router.Group("/v1")
	videoV1Router.Use(middleware.RouteTag("relay"))
	videoV1Router.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		videoV1Router.POST("/video/generations", workflowhttp.SubmitRelayTask)
		videoV1Router.GET("/video/generations/:task_id", workflowhttp.FetchRelayTask)
		videoV1Router.POST("/videos/:video_id/remix", workflowhttp.SubmitRelayTask)
		videoV1Router.POST("/videos", workflowhttp.SubmitRelayTask)
		videoV1Router.GET("/videos/:task_id", workflowhttp.FetchRelayTask)
	}

	klingV1Router := router.Group("/kling/v1")
	klingV1Router.Use(middleware.RouteTag("relay"))
	klingV1Router.Use(middleware.KlingRequestConvert(), middleware.TokenAuth(), middleware.Distribute())
	{
		klingV1Router.POST("/videos/text2video", workflowhttp.SubmitRelayTask)
		klingV1Router.POST("/videos/image2video", workflowhttp.SubmitRelayTask)
		klingV1Router.GET("/videos/text2video/:task_id", workflowhttp.FetchRelayTask)
		klingV1Router.GET("/videos/image2video/:task_id", workflowhttp.FetchRelayTask)
	}

	jimengOfficialGroup := router.Group("jimeng")
	jimengOfficialGroup.Use(middleware.RouteTag("relay"))
	jimengOfficialGroup.Use(middleware.JimengRequestConvert(), middleware.TokenAuth(), middleware.Distribute())
	{
		jimengOfficialGroup.POST("/", workflowhttp.SubmitRelayTask)
	}
}
