package bootstrap

import (
	"strings"

	"github.com/joho/godotenv"
	"github.com/sh2001sh/new-api/i18n"
	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	"github.com/sh2001sh/new-api/internal/identity/oauth"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconcurrency "github.com/sh2001sh/new-api/internal/platform/concurrency"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
	platformtokenx "github.com/sh2001sh/new-api/internal/platform/tokenx"
)

func initResources() error {
	if err := godotenv.Load(".env"); err != nil && platformconfig.DebugEnabled {
		platformobservability.SysLog("No .env file found, using default environment variables. If needed, please create a .env file and set the relevant variables.")
	}

	initEnvironment()
	platformconcurrency.ConfigureRelayAdmission(platformconfig.RelayMaxConcurrentRequests)
	platformcache.ConfigureRedisRuntime(platformcache.RedisRuntimeConfig{
		DebugEnabled:  platformconfig.DebugEnabled,
		SyncFrequency: platformconfig.SyncFrequency,
		Logf:          platformobservability.SysLog,
		FatalLog: func(message string) {
			platformobservability.FatalLog(message)
		},
	})
	logger.SetupLogger()
	gatewaystore.InitRatioSettings()
	platformhttpx.InitHTTPClient()
	platformtokenx.InitTokenEncoders()

	if err := platformstore.InitPrimaryDB(); err != nil {
		platformobservability.FatalLog("failed to initialize database: " + err.Error())
		return err
	}
	if err := commerceapp.EnsureDefaultSubscriptionPlans(); err != nil {
		platformobservability.FatalLog("failed to initialize default subscription plans: " + err.Error())
		return err
	}
	if err := commerceapp.MigrateBlindBoxLegacyCredits(); err != nil {
		platformobservability.FatalLog("failed to migrate legacy blind box credits: " + err.Error())
		return err
	}
	if err := auditprojection.EnsureSchema(); err != nil {
		platformobservability.FatalLog("failed to initialize audit projection schema: " + err.Error())
		return err
	}

	platformstore.CheckSetup()

	if err := commerceapp.InitGhostUsers(); err != nil {
		platformobservability.SysLog("failed to initialize ghost users: " + err.Error())
	}
	if err := commerceapp.EnsureGhostGroupBuys(); err != nil {
		platformobservability.SysLog("failed to ensure ghost group buys: " + err.Error())
	}

	platformstore.InitOptionMap()
	platformhttpx.CleanupOldCacheFiles()
	loadBootstrapPricing()

	if err := platformstore.InitLogDB(); err != nil {
		return err
	}
	if err := platformcache.InitRedisClient(); err != nil {
		return err
	}

	platformobservability.StartSystemMonitor()
	if err := i18n.Init(); err != nil {
		platformobservability.SysError("failed to initialize i18n: " + err.Error())
	} else {
		platformobservability.SysLog("i18n initialized with languages: " + strings.Join(i18n.SupportedLanguages(), ", "))
	}
	i18n.SetUserLangLoader(identityapp.LoadUserLanguage)

	if err := oauth.LoadCustomProviders(); err != nil {
		platformobservability.SysError("failed to load custom OAuth providers: " + err.Error())
	}

	return nil
}
