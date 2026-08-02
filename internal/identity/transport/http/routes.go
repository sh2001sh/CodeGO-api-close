package http

import (
	"github.com/gin-gonic/gin"
	commercehttp "github.com/sh2001sh/new-api/internal/commerce/transport/http"
	gatewayhttp "github.com/sh2001sh/new-api/internal/gateway/transport/http"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	"github.com/sh2001sh/new-api/internal/platform/transport/http/middleware"
)

func RegisterUserRoutes(apiRouter *gin.RouterGroup, anonymousRequestBodyLimit gin.HandlerFunc) {
	apiRouter.GET("/verification", middleware.EmailVerificationRateLimit(), middleware.TurnstileCheck(), SendEmailVerification)
	apiRouter.GET("/reset_password", middleware.CriticalRateLimit(), middleware.TurnstileCheck(), SendPasswordResetEmail)
	apiRouter.POST("/user/reset", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, ResetPassword)

	userRoute := apiRouter.Group("/user")
	{
		userRoute.POST("/register", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, middleware.TurnstileCheck(), Register)
		userRoute.POST("/login", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, middleware.TurnstileCheck(), Login)
		userRoute.POST("/login/2fa", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, Verify2FALogin)
		userRoute.POST("/passkey/login/begin", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, PasskeyLoginBegin)
		userRoute.POST("/passkey/login/finish", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, PasskeyLoginFinish)
		userRoute.GET("/logout", Logout)
		userRoute.POST("/epay/notify", anonymousRequestBodyLimit, commercehttp.EpayNotify)
		userRoute.GET("/epay/notify", commercehttp.EpayNotify)
		userRoute.POST("/xunhu/notify", commercehttp.XunhuNotify)
		userRoute.GET("/xunhu/notify", commercehttp.XunhuNotify)
		userRoute.GET("/xunhu/return", commercehttp.XunhuReturn)
		userRoute.GET("/groups", gatewayhttp.GetUserGroups)

		selfRoute := userRoute.Group("/")
		selfRoute.Use(middleware.UserAuth())
		{
			selfRoute.GET("/self/groups", gatewayhttp.GetUserGroups)
			selfRoute.GET("/self/group-status", gatewayhttp.GetUserGroupStatus)
			selfRoute.GET("/self", GetUserSelf)
			selfRoute.GET("/models", GetUserModels)
			selfRoute.GET("/image-workspace/models", GetImageWorkspaceModels)
			selfRoute.GET("/image-workspace/items", GetImageWorkspaceItems)
			selfRoute.GET("/image-workspace/items/:id/content", GetImageWorkspaceItemContent)
			selfRoute.PUT("/self", UpdateSelf)
			selfRoute.DELETE("/self", DeleteSelf)
			selfRoute.GET("/token", GenerateAccessToken)
			selfRoute.GET("/token/status", GetTokenStatus)
			selfRoute.GET("/passkey", PasskeyStatus)
			selfRoute.POST("/passkey/register/begin", PasskeyRegisterBegin)
			selfRoute.POST("/passkey/register/finish", PasskeyRegisterFinish)
			selfRoute.POST("/passkey/verify/begin", PasskeyVerifyBegin)
			selfRoute.POST("/passkey/verify/finish", PasskeyVerifyFinish)
			selfRoute.DELETE("/passkey", PasskeyDelete)
			selfRoute.GET("/aff", GetUserAffiliateCode)
			selfRoute.GET("/aff/overview", GetUserAffiliateRewardsOverview)
			selfRoute.GET("/topup/info", commercehttp.GetTopUpInfo)
			selfRoute.GET("/topup/self", commercehttp.GetUserTopUps)
			selfRoute.POST("/topup", middleware.CriticalRateLimit(), commercehttp.RedeemTopUpCode)
			selfRoute.POST("/pay", middleware.CriticalRateLimit(), commercehttp.RequestTopUpPayment)
			selfRoute.POST("/xunhu/pay", middleware.CriticalRateLimit(), commercehttp.RequestXunhuPay)
			selfRoute.POST("/amount", commercehttp.RequestAmount)
			selfRoute.POST("/stripe/pay", middleware.CriticalRateLimit(), commercehttp.RequestStripePay)
			selfRoute.POST("/stripe/amount", commercehttp.RequestStripeAmount)
			selfRoute.POST("/creem/pay", middleware.CriticalRateLimit(), commercehttp.RequestCreemPay)
			selfRoute.POST("/waffo/amount", commercehttp.RequestWaffoAmount)
			selfRoute.POST("/waffo/pay", middleware.CriticalRateLimit(), commercehttp.RequestWaffoPay)
			selfRoute.POST("/waffo-pancake/amount", commercehttp.RequestWaffoPancakeAmount)
			selfRoute.POST("/waffo-pancake/pay", middleware.CriticalRateLimit(), commercehttp.RequestWaffoPancakePay)
			selfRoute.POST("/aff_transfer", TransferAffQuota)
			selfRoute.PUT("/setting", UpdateUserSetting)
			selfRoute.GET("/2fa/status", GetTwoFAStatus)
			selfRoute.POST("/2fa/setup", SetupTwoFA)
			selfRoute.POST("/2fa/enable", EnableTwoFA)
			selfRoute.POST("/2fa/disable", DisableTwoFA)
			selfRoute.POST("/2fa/backup_codes", RegenerateBackupCodes)
			selfRoute.GET("/checkin", GetUserCheckinStatus)
			selfRoute.POST("/checkin", middleware.TurnstileCheck(), DoUserCheckin)
			selfRoute.GET("/oauth/bindings", GetUserOAuthBindings)
			selfRoute.DELETE("/oauth/bindings/:provider_id", UnbindCustomOAuth)
			selfRoute.POST("/miniprogram/bind-code", middleware.CriticalRateLimit(), CreateMiniProgramBindCode)
			selfRoute.GET("/miniprogram/binding", GetMiniProgramBinding)
			selfRoute.DELETE("/miniprogram/binding", DeleteMiniProgramBinding)
		}

		adminRoute := userRoute.Group("/")
		adminRoute.Use(middleware.AdminAuth())
		{
			adminRoute.GET("/", GetAllUsers)
			adminRoute.GET("/topup", commercehttp.GetAllTopUps)
			adminRoute.POST("/topup/complete", commercehttp.AdminCompleteTopUp)
			adminRoute.GET("/search", SearchUsers)
			adminRoute.GET("/:id/oauth/bindings", GetUserOAuthBindingsByAdmin)
			adminRoute.DELETE("/:id/oauth/bindings/:provider_id", UnbindCustomOAuthByAdmin)
			adminRoute.DELETE("/:id/bindings/:binding_type", AdminClearUserBinding)
			adminRoute.GET("/:id", GetUser)
			adminRoute.POST("/", CreateUser)
			adminRoute.POST("/manage", ManageUser)
			adminRoute.PUT("/", UpdateUser)
			adminRoute.DELETE("/:id", DeleteUser)
			adminRoute.DELETE("/:id/reset_passkey", AdminResetPasskey)
			adminRoute.GET("/2fa/stats", Admin2FAStats)
			adminRoute.DELETE("/:id/2fa", AdminDisable2FA)
		}
	}
}

func RegisterDesktopRoutes(apiRouter *gin.RouterGroup) {
	apiRouter.GET("/desktop/import/config", GetDesktopImportConfig)
	apiRouter.GET("/desktop/release/latest", middleware.DisableCache(), GetDesktopReleaseLatest)
	apiRouter.GET("/desktop/release/latest.json", middleware.DisableCache(), GetDesktopReleaseLatestJSON)
	// Browser-side one-click imports use the website session, not a desktop token.
	apiRouter.POST("/desktop/import/deeplink", middleware.UserAuth(), middleware.CriticalRateLimit(), CreateDesktopImportConfig)

	desktopRoute := apiRouter.Group("/desktop")
	desktopRoute.Use(middleware.DesktopAuth())
	{
		desktopRoute.GET("/account/summary", middleware.RequireDesktopScope(identitydomain.DesktopScopeAccountRead), GetDesktopAccountSummary)
		desktopRoute.GET("/usage/logs", middleware.RequireDesktopScope(identitydomain.DesktopScopeLogsRead), GetDesktopUsageLogs)
		desktopRoute.GET("/usage/trends", middleware.RequireDesktopScope(identitydomain.DesktopScopeLogsRead), GetDesktopUsageTrends)
		desktopRoute.GET("/groups", middleware.RequireDesktopScope(identitydomain.DesktopScopeAccountRead), GetDesktopGroups)
		desktopRoute.GET("/pricing", middleware.RequireDesktopScope(identitydomain.DesktopScopeAccountRead), gatewayhttp.GetPricing)
		desktopRoute.GET("/group-status", middleware.RequireDesktopScope(identitydomain.DesktopScopeAccountRead), gatewayhttp.GetUserGroupStatus)
		desktopRoute.GET("/authorized-devices", middleware.RequireDesktopScope(identitydomain.DesktopScopeAccountRead), ListDesktopAuthorizedDevices)
		desktopRoute.DELETE("/authorized-devices/:id", middleware.RequireDesktopScope(identitydomain.DesktopScopeAccountRead), middleware.CriticalRateLimit(), RevokeDesktopAuthorizedDevice)
		desktopRoute.GET("/tokens", middleware.RequireDesktopScope(identitydomain.DesktopScopeTokensRead), GetDesktopTokens)
		desktopRoute.POST("/tokens", middleware.RequireDesktopScope(identitydomain.DesktopScopeTokensWrite), AddToken)
		desktopRoute.PUT("/tokens", middleware.RequireDesktopScope(identitydomain.DesktopScopeTokensWrite), UpdateToken)
		desktopRoute.DELETE("/tokens/:id", middleware.RequireDesktopScope(identitydomain.DesktopScopeTokensWrite), DeleteToken)
		desktopRoute.POST("/tokens/:id/key", middleware.RequireDesktopScope(identitydomain.DesktopScopeTokensRead), middleware.CriticalRateLimit(), middleware.DisableCache(), GetDesktopTokenKey)
		desktopRoute.PUT("/tokens/:id/group", middleware.RequireDesktopScope(identitydomain.DesktopScopeTokensWrite), UpdateDesktopTokenGroup)
		desktopRoute.POST("/tokens/ensure", middleware.RequireDesktopScope(identitydomain.DesktopScopeTokensWrite), EnsureDesktopToken)
		desktopRoute.GET("/tokens/:id/config", middleware.RequireDesktopScope(identitydomain.DesktopScopeTokensRead), middleware.CriticalRateLimit(), middleware.DisableCache(), GetDesktopTokenConfig)
		desktopRoute.GET("/config/template", middleware.RequireDesktopScope(identitydomain.DesktopScopeConfigRead), GetDesktopConfigTemplate)
		desktopRoute.GET("/config/templates", middleware.RequireDesktopScope(identitydomain.DesktopScopeConfigRead), GetDesktopConfigTemplates)
		desktopRoute.GET("/service/status", middleware.RequireDesktopScope(identitydomain.DesktopScopeAccountRead), GetDesktopServiceStatus)
		desktopRoute.POST("/diagnostics/report", middleware.RequireDesktopScope(identitydomain.DesktopScopeConfigWrite), CreateDesktopDiagnosticReport)
		desktopRoute.POST("/telemetry/events", middleware.RequireDesktopScope(identitydomain.DesktopScopeTelemetryWrite), CreateDesktopTelemetryEvent)
	}

	desktopAuthRoute := apiRouter.Group("/desktop/auth")
	{
		desktopAuthRoute.POST("/session", middleware.CriticalRateLimit(), StartDesktopAuthSession)
		desktopAuthRoute.POST("/poll", middleware.CriticalRateLimit(), PollDesktopAuthSession)
		desktopAuthRoute.GET("/session", middleware.UserAuth(), GetDesktopAuthSession)
		desktopAuthRoute.POST("/approve", middleware.UserAuth(), middleware.CriticalRateLimit(), ApproveDesktopAuthSession)
		desktopAuthRoute.POST("/reject", middleware.UserAuth(), middleware.CriticalRateLimit(), RejectDesktopAuthSession)
	}

	desktopDevicesRoute := apiRouter.Group("/desktop/devices")
	desktopDevicesRoute.Use(middleware.UserAuth())
	{
		desktopDevicesRoute.GET("", ListDesktopAuthorizedDevices)
		desktopDevicesRoute.DELETE("/:id", middleware.CriticalRateLimit(), RevokeDesktopAuthorizedDevice)
	}

	tokenRoute := apiRouter.Group("/token")
	tokenRoute.Use(middleware.UserAuth())
	{
		tokenRoute.GET("/", GetAllTokens)
		tokenRoute.GET("/search", middleware.SearchRateLimit(), SearchTokens)
		tokenRoute.GET("/:id", GetToken)
		tokenRoute.POST("/:id/key", middleware.CriticalRateLimit(), middleware.DisableCache(), GetTokenKey)
		tokenRoute.POST("/", AddToken)
		tokenRoute.PUT("/", UpdateToken)
		tokenRoute.DELETE("/:id", DeleteToken)
		tokenRoute.POST("/batch", DeleteTokenBatch)
		tokenRoute.POST("/batch/keys", middleware.CriticalRateLimit(), middleware.DisableCache(), GetTokenKeysBatch)
	}

	usageRoute := apiRouter.Group("/usage")
	usageRoute.Use(middleware.CORS(), middleware.CriticalRateLimit())
	{
		tokenUsageRoute := usageRoute.Group("/token")
		tokenUsageRoute.Use(middleware.TokenAuthReadOnly())
		{
			tokenUsageRoute.GET("/", GetTokenUsage)
		}
	}
}
