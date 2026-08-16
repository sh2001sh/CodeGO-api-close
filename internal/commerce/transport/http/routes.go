package http

import (
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/internal/platform/transport/http/middleware"
)

func RegisterCommerceRoutes(apiRouter *gin.RouterGroup, anonymousRequestBodyLimit gin.HandlerFunc) {
	walletRoute := apiRouter.Group("/wallet")
	walletRoute.Use(middleware.UserAuth())
	{
		walletRoute.GET("/unified-credit-migration", getUnifiedCreditMigrationDetail)
		walletRoute.GET("/transfers", getWalletTransferOverview)
		walletRoute.GET("/transfers/recipients/:external_id", middleware.CriticalRateLimit(), getWalletTransferRecipient)
		walletRoute.PUT("/transfers/payment-password", middleware.CriticalRateLimit(), configureWalletTransferPassword)
		walletRoute.POST("/transfers/payment-password/email-code", middleware.EmailVerificationRateLimit(), sendWalletTransferPasswordEmailCode)
		walletRoute.POST("/transfers", middleware.CriticalRateLimit(), createWalletTransfer)
	}

	subscriptionRoute := apiRouter.Group("/subscription")
	subscriptionRoute.Use(middleware.UserAuth())
	{
		subscriptionRoute.GET("/plans", getSubscriptionPlans)
		subscriptionRoute.GET("/self", getSubscriptionSelf)
		subscriptionRoute.GET("/self/claude-conversions", listSubscriptionClaudeConversions)
		subscriptionRoute.POST("/self/claude-conversions", middleware.CriticalRateLimit(), createSubscriptionClaudeConversion)
		subscriptionRoute.GET("/orders/:trade_no", getSubscriptionOrderStatus)
		subscriptionRoute.POST("/orders/:trade_no/cancel", middleware.CriticalRateLimit(), cancelSubscriptionOrder)
		subscriptionRoute.PUT("/self/preference", updateSubscriptionPreference)
		subscriptionRoute.POST("/self/reset-opportunity/use", middleware.CriticalRateLimit(), useSubscriptionResetOpportunity)
		subscriptionRoute.POST("/fuel/quote", quoteSubscriptionFuel)
		subscriptionRoute.POST("/fuel/purchase", middleware.CriticalRateLimit(), purchaseSubscriptionFuel)
		subscriptionRoute.POST("/epay/pay", middleware.CriticalRateLimit(), RequestSubscriptionEpay)
		subscriptionRoute.POST("/xunhu/pay", middleware.CriticalRateLimit(), RequestSubscriptionXunhuPay)
		subscriptionRoute.POST("/stripe/pay", middleware.CriticalRateLimit(), RequestSubscriptionStripePay)
		subscriptionRoute.POST("/creem/pay", middleware.CriticalRateLimit(), RequestSubscriptionCreemPay)
	}

	subscriptionAdminRoute := apiRouter.Group("/subscription/admin")
	subscriptionAdminRoute.Use(middleware.AdminAuth())
	{
		subscriptionAdminRoute.GET("/plans", listAdminSubscriptionPlans)
		subscriptionAdminRoute.POST("/plans", createAdminSubscriptionPlan)
		subscriptionAdminRoute.PUT("/plans/:id", updateAdminSubscriptionPlan)
		subscriptionAdminRoute.PATCH("/plans/:id", updateAdminSubscriptionPlanStatus)
		subscriptionAdminRoute.DELETE("/plans/:id", deleteAdminSubscriptionPlan)
		subscriptionAdminRoute.POST("/bind", bindAdminSubscription)
		subscriptionAdminRoute.GET("/users/:id/subscriptions", listAdminUserSubscriptions)
		subscriptionAdminRoute.POST("/users/:id/subscriptions", createAdminUserSubscription)
		subscriptionAdminRoute.PUT("/user_subscriptions/:id", updateAdminUserSubscription)
		subscriptionAdminRoute.POST("/user_subscriptions/:id/reset", resetAdminUserSubscriptionQuota)
		subscriptionAdminRoute.POST("/user_subscriptions/:id/invalidate", invalidateAdminUserSubscription)
		subscriptionAdminRoute.DELETE("/user_subscriptions/:id", deleteAdminUserSubscription)
	}

	apiRouter.POST("/subscription/epay/notify", anonymousRequestBodyLimit, SubscriptionEpayNotify)
	apiRouter.GET("/subscription/epay/notify", SubscriptionEpayNotify)
	apiRouter.GET("/subscription/epay/return", SubscriptionEpayReturn)
	apiRouter.POST("/subscription/epay/return", anonymousRequestBodyLimit, SubscriptionEpayReturn)
	apiRouter.POST("/subscription/xunhu/notify", anonymousRequestBodyLimit, SubscriptionXunhuNotify)
	apiRouter.GET("/subscription/xunhu/notify", SubscriptionXunhuNotify)
	apiRouter.GET("/subscription/xunhu/return", SubscriptionXunhuReturn)

	invoiceRoute := apiRouter.Group("/invoices")
	invoiceRoute.Use(middleware.UserAuth())
	{
		invoiceRoute.GET("/eligible-orders", listInvoiceEligibleOrders)
		invoiceRoute.GET("/requests", listSelfInvoiceRequests)
		invoiceRoute.POST("/requests", middleware.CriticalRateLimit(), createInvoiceRequest)
	}

	invoiceAdminRoute := apiRouter.Group("/invoices/admin")
	invoiceAdminRoute.Use(middleware.AdminAuth())
	{
		invoiceAdminRoute.GET("/requests", listAdminInvoiceRequests)
		invoiceAdminRoute.PUT("/requests/:id", updateAdminInvoiceRequest)
	}

	packagesRoute := apiRouter.Group("/packages")
	packagesRoute.Use(middleware.UserAuth())
	{
		packagesRoute.GET("/public", getPublicPackages)
		packagesRoute.GET("/my-subscription", getSubscriptionSelf)
		packagesRoute.GET("/starter-upgrade-bonus", getStarterUpgradeBonus)
		packagesRoute.POST("/purchase", middleware.CriticalRateLimit(), PurchasePackage)
		packagesRoute.POST("/upgrade", middleware.CriticalRateLimit(), UpgradePackage)
		packagesRoute.POST("/renew", middleware.CriticalRateLimit(), RenewPackage)
	}

	groupBuyRoute := apiRouter.Group("/group-buy")
	groupBuyRoute.Use(middleware.UserAuth())
	{
		groupBuyRoute.GET("/list", listGroupBuys)
		groupBuyRoute.GET("/mine", listMyGroupBuys)
		groupBuyRoute.POST("/join", middleware.CriticalRateLimit(), joinGroupBuy)
		groupBuyRoute.GET("/:id", getGroupBuy)
	}

	blindBoxRoute := apiRouter.Group("/blind-box")
	blindBoxRoute.Use(middleware.UserAuth())
	{
		blindBoxRoute.GET("/self", getBlindBoxSelf)
		blindBoxRoute.GET("/history", getBlindBoxHistory)
		blindBoxRoute.GET("/orders/:trade_no", getBlindBoxOrderStatus)
		blindBoxRoute.POST("/amount", requestBlindBoxAmount)
		blindBoxRoute.POST("/pay", middleware.BlindBoxPaymentRateLimit(), requestBlindBoxPay)
		blindBoxRoute.POST("/props/:id/use", middleware.CriticalRateLimit(), useBlindBoxProp)
		blindBoxRoute.POST("/props/:id/pause", middleware.CriticalRateLimit(), pauseBlindBoxProp)
		blindBoxRoute.POST("/props/:id/convert", middleware.CriticalRateLimit(), convertBlindBoxProp)
		blindBoxRoute.GET("/inventory/overview", getBalanceBlindBoxOverview)
		blindBoxRoute.POST("/inventory/purchase", middleware.BalanceBlindBoxOpenRateLimit(), purchaseBalanceBlindBoxes)
		blindBoxRoute.POST("/inventory/open", middleware.BalanceBlindBoxOpenRateLimit(), openBalanceBlindBox)
		blindBoxRoute.POST("/inventory/gift", middleware.CriticalRateLimit(), giftBalanceBlindBoxes)
		blindBoxRoute.POST("/simulation/draw", middleware.BalanceBlindBoxOpenRateLimit(), simulateBalanceBlindBoxes)
	}

	dailyLuckyNumberRoute := apiRouter.Group("/daily-lucky-number")
	dailyLuckyNumberRoute.Use(middleware.UserAuth())
	{
		dailyLuckyNumberRoute.GET("/self", getDailyLuckyNumberSelf)
		dailyLuckyNumberRoute.GET("/history", getDailyLuckyNumberHistory)
		dailyLuckyNumberRoute.GET("/public-wins", getDailyLuckyNumberPublicWins)
		dailyLuckyNumberRoute.GET("/notifications", getDailyLuckyRewardNotifications)
		dailyLuckyNumberRoute.POST("/notifications/read-all", middleware.CriticalRateLimit(), markAllDailyLuckyRewardNotificationsRead)
		dailyLuckyNumberRoute.POST("/notifications/:id/read", middleware.CriticalRateLimit(), markDailyLuckyRewardNotificationRead)
	}

	dailyLuckyNumberAdminRoute := apiRouter.Group("/daily-lucky-number/admin")
	dailyLuckyNumberAdminRoute.Use(middleware.AdminAuth())
	{
		dailyLuckyNumberAdminRoute.GET("/config", getAdminDailyLuckyNumberConfig)
		dailyLuckyNumberAdminRoute.PUT("/config", updateAdminDailyLuckyNumberConfig)
		dailyLuckyNumberAdminRoute.GET("/draws", listAdminDailyLuckyNumberDraws)
		dailyLuckyNumberAdminRoute.POST("/draws/:id/retry", retryAdminDailyLuckyNumberDraw)
		dailyLuckyNumberAdminRoute.POST("/backfill", backfillAdminDailyLuckyNumbers)
	}

	blindBoxAdminRoute := apiRouter.Group("/blind-box/admin")
	blindBoxAdminRoute.Use(middleware.AdminAuth())
	{
		blindBoxAdminRoute.GET("/users/:id/overview", adminGetBlindBoxUserOverview)
		blindBoxAdminRoute.POST("/users/:id/grants", adminGrantBlindBoxes)
	}

	apiRouter.POST("/blind-box/epay/notify", anonymousRequestBodyLimit, blindBoxEpayNotify)
	apiRouter.GET("/blind-box/epay/notify", blindBoxEpayNotify)
	apiRouter.GET("/blind-box/epay/return", blindBoxEpayReturn)
	apiRouter.POST("/blind-box/epay/return", anonymousRequestBodyLimit, blindBoxEpayReturn)
	apiRouter.POST("/blind-box/xunhu/notify", anonymousRequestBodyLimit, blindBoxXunhuNotify)
	apiRouter.GET("/blind-box/xunhu/notify", blindBoxXunhuNotify)
	apiRouter.GET("/blind-box/xunhu/return", blindBoxXunhuReturn)
}
