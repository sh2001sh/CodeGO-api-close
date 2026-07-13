package bootstrap

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
	"github.com/sh2001sh/new-api/internal/platform/transport/http/middleware"

	_ "net/http/pprof"
)

type httpRouteRegistrar func(*gin.Engine)

func prepareRuntime(component string) error {
	if err := initResources(); err != nil {
		platformobservability.FatalLog("failed to initialize resources: " + err.Error())
		return err
	}
	logStartupMode(component)
	initCaches()
	return nil
}

func logStartupMode(component string) {
	platformobservability.SysLog(component + " " + platformconfig.Version + " started")
	if os.Getenv("GIN_MODE") != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}
	if platformconfig.DebugEnabled {
		platformobservability.SysLog("running in debug mode")
	}
}

func closeDatabase() {
	if err := platformstore.CloseDatabases(); err != nil {
		platformobservability.FatalLog("failed to close database: " + err.Error())
	}
}

func initCaches() {
	if platformcache.RedisEnabled {
		platformconfig.MemoryCacheEnabled = true
	}
	if !platformconfig.MemoryCacheEnabled {
		return
	}

	platformobservability.SysLog("memory cache enabled")
	platformobservability.SysLog(fmt.Sprintf("sync frequency: %d seconds", platformconfig.SyncFrequency))
	func() {
		defer func() {
			if r := recover(); r != nil {
				platformobservability.SysLog(fmt.Sprintf("InitChannelCache panic: %v, retrying once", r))
				if _, _, fixErr := gatewaystore.RebuildChannelAbilities(); fixErr != nil {
					platformobservability.FatalLog(fmt.Sprintf("InitChannelCache failed: %s", fixErr.Error()))
				}
			}
		}()
		gatewaystore.InitChannelCache()
	}()
	go gatewaystore.SyncChannelCache(platformconfig.SyncFrequency)
}

func startOptionSyncLoop() {
	go platformstore.SyncOptions(platformconfig.SyncFrequency)
}

func startDiagnostics() {
	if os.Getenv("ENABLE_PPROF") == "true" {
		gopool.Go(func() {
			log.Println(http.ListenAndServe("0.0.0.0:8005", nil))
		})
		platformobservability.StartPprofCPUMonitor()
		platformobservability.SysLog("pprof enabled")
	}

	if err := platformobservability.StartPyroscope(); err != nil {
		platformobservability.SysError(fmt.Sprintf("start pyroscope error : %v", err))
	}

	auditprojection.Init()
}

func buildHTTPServer(registerRoutes httpRouteRegistrar) *gin.Engine {
	server := gin.New()
	middleware.ConfigureTrustedProxies(server)
	server.Use(gin.CustomRecovery(func(c *gin.Context, err any) {
		platformobservability.SysLog(fmt.Sprintf("panic detected: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": fmt.Sprintf("Panic detected, error: %v. Please submit a issue here: https://github.com/Calcium-Ion/new-api", err),
				"type":    "new_api_panic",
			},
		})
	}))
	server.Use(middleware.RequestId())
	server.Use(middleware.PoweredBy())
	server.Use(middleware.I18n())
	middleware.SetUpLogger(server)
	server.Use(sessions.Sessions("session", buildSessionStore()))
	registerRoutes(server)
	return server
}

func buildSessionStore() sessions.Store {
	store := cookie.NewStore([]byte(platformconfig.SessionSecret))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   2592000,
		HttpOnly: true,
		Secure:   platformconfig.SessionCookieSecure(),
		// OAuth providers redirect the browser back from another site. Lax keeps the
		// session available for that top-level GET callback while retaining CSRF
		// protection for cross-site subrequests.
		SameSite: http.SameSiteLaxMode,
	})
	return store
}

func resolvePort(primaryEnv string) string {
	port := os.Getenv(primaryEnv)
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = strconv.Itoa(*platformconfig.Port)
	}
	return port
}
