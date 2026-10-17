package bootstrap

import (
	"errors"
	"os"
	"strconv"
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
	commerceBootstrapEnabled, err := shouldRunCommerceBootstrap(platformconfig.IsMasterNode, os.Getenv("SKIP_COMMERCE_BOOTSTRAP"))
	if err != nil {
		return err
	}
	platformconcurrency.ConfigureRelayAdmission(platformconfig.RelayMaxConcurrentRequests)
	platformconcurrency.ConfigureRelayUploadAdmission(platformconfig.RelayMaxConcurrentUploads)
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
	// Only masters without an explicit rollout opt-out initialize commerce.
	// Gateways and rolling replacements must not seed plans or move old credits.
	if commerceBootstrapEnabled {
		if err := commerceapp.EnsureDefaultSubscriptionPlans(); err != nil {
			platformobservability.FatalLog("failed to initialize default subscription plans: " + err.Error())
			return err
		}
		if err := commerceapp.MigrateBlindBoxLegacyCredits(); err != nil {
			platformobservability.FatalLog("failed to migrate legacy blind box credits: " + err.Error())
			return err
		}
	}
	if shouldRunAuditSchemaBootstrap(platformconfig.IsMasterNode) {
		if err := auditprojection.EnsureSchema(); err != nil {
			platformobservability.FatalLog("failed to initialize audit projection schema: " + err.Error())
			return err
		}
	}

	platformstore.CheckSetup()

	if commerceBootstrapEnabled {
		if err := commerceapp.InitGhostUsers(); err != nil {
			platformobservability.SysLog("failed to initialize ghost users: " + err.Error())
		}
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

// shouldRunCommerceBootstrap keeps startup writers opt-out independently of
// schema migrations. Invalid rollout configuration must fail before opening DBs.
func shouldRunCommerceBootstrap(master bool, rawSkip string) (bool, error) {
	rawSkip = strings.TrimSpace(rawSkip)
	if rawSkip == "" {
		return master, nil
	}
	skip, err := strconv.ParseBool(rawSkip)
	if err != nil {
		return false, errors.New("SKIP_COMMERCE_BOOTSTRAP must be a boolean")
	}
	return master && !skip, nil
}

// Retain legacy startup behavior when unset; explicit false also disables audit
// DDL. The db-migrate command owns its separate explicit read-model migration.
func shouldRunAuditSchemaBootstrap(master bool) bool {
	return master && platformconfig.GetEnvOrDefaultBool("AUTO_DB_MIGRATION", true)
}
