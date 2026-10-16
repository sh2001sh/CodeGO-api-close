package http

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	nethttp "net/http"
	"os"
	"path"
	"strings"

	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	gatewayhttp "github.com/sh2001sh/new-api/internal/gateway/transport/http"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"github.com/sh2001sh/new-api/internal/platform/transport/http/middleware"
)

type ThemeAssets struct {
	DefaultBuildFS   embed.FS
	DefaultIndexPage []byte
}

func registerControlWebRoutes(router *gin.Engine, assets ThemeAssets) {
	frontendBaseURL := os.Getenv("FRONTEND_BASE_URL")
	if platformconfig.IsMasterNode && frontendBaseURL != "" {
		frontendBaseURL = ""
		platformobservability.SysLog("FRONTEND_BASE_URL is ignored on master node")
	}
	if frontendBaseURL == "" {
		registerEmbeddedWebRoutes(router, assets)
		return
	}

	frontendBaseURL = strings.TrimSuffix(frontendBaseURL, "/")
	router.NoRoute(func(c *gin.Context) {
		c.Set(middleware.RouteTagKey, "web")
		c.Redirect(nethttp.StatusMovedPermanently, fmt.Sprintf("%s%s", frontendBaseURL, c.Request.RequestURI))
	})
}

func registerEmbeddedWebRoutes(router *gin.Engine, assets ThemeAssets) {
	defaultFS := embedFolder(assets.DefaultBuildFS, "dist")
	staticHandler := static.Serve("/", defaultFS)
	exactFS, err := fs.Sub(assets.DefaultBuildFS, "dist")
	if err != nil {
		panic(err)
	}

	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(embeddedWebRateLimit(exactFS, middleware.GlobalWebRateLimit()))
	router.Use(middleware.Cache())

	topicsIndexHandler := func(c *gin.Context) {
		serveEmbeddedHTML(c, defaultFS, "/topics/index.html")
	}
	topicDetailHandler := func(c *gin.Context) {
		serveEmbeddedHTML(c, defaultFS, "/topics/"+c.Param("slug")+"/index.html")
	}
	homeHandler := func(c *gin.Context) {
		serveIndexPage(c, assets.DefaultIndexPage)
	}

	router.GET("/", homeHandler)
	router.HEAD("/", homeHandler)
	router.GET("/index.html", homeHandler)
	router.HEAD("/index.html", homeHandler)
	router.GET("/topics", topicsIndexHandler)
	router.HEAD("/topics", topicsIndexHandler)
	router.GET("/topics/:slug", topicDetailHandler)
	router.HEAD("/topics/:slug", topicDetailHandler)
	router.Use(func(c *gin.Context) {
		path := c.Request.URL.Path
		if path == "/topics" || strings.HasPrefix(path, "/topics/") {
			c.Next()
			return
		}
		// Application routes are extensionless. Let the SPA handle them even if
		// an outdated build happens to contain a same-named directory.
		lastSegment := path[strings.LastIndex(path, "/")+1:]
		if !strings.Contains(lastSegment, ".") {
			c.Next()
			return
		}
		staticHandler(c)
	})
	router.NoRoute(func(c *gin.Context) {
		c.Set(middleware.RouteTagKey, "web")
		if strings.HasPrefix(c.Request.RequestURI, "/v1") || strings.HasPrefix(c.Request.RequestURI, "/api") || strings.HasPrefix(c.Request.RequestURI, "/assets") {
			gatewayhttp.RelayNotFound(c)
			return
		}
		serveIndexPage(c, assets.DefaultIndexPage)
	})
}

// A cold page loads many immutable chunks. They must not consume the shared
// navigation budget; unknown paths and SPA HTML still use the web limiter.
func embeddedWebRateLimit(files fs.FS, limit gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isEmbeddedStaticRequest(c.Request, files) {
			limit(c)
		}
	}
}

func isEmbeddedStaticRequest(request *nethttp.Request, files fs.FS) bool {
	if request.Method != nethttp.MethodGet && request.Method != nethttp.MethodHead {
		return false
	}
	if request.URL.RawPath != "" {
		return false
	}
	name := strings.TrimPrefix(request.URL.Path, "/")
	if !fs.ValidPath(name) || strings.ContainsAny(name, "\\\x00") {
		return false
	}
	for _, reserved := range []string{"api", "v1", "topics"} {
		if name == reserved || strings.HasPrefix(name, reserved+"/") {
			return false
		}
	}
	ext := strings.ToLower(path.Ext(name))
	if ext == "" || ext == ".html" || ext == ".htm" {
		return false
	}
	// Use the exact embedded filesystem, never embedFileSystem.Open: its
	// directory/index.html fallback would exempt non-static application routes.
	info, err := fs.Stat(files, name)
	return err == nil && info.Mode().IsRegular()
}

func serveEmbeddedHTML(c *gin.Context, fs static.ServeFileSystem, assetPath string) {
	file, err := fs.Open(assetPath)
	if err != nil {
		gatewayhttp.RelayNotFound(c)
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		gatewayhttp.RelayNotFound(c)
		return
	}

	c.Header("Cache-Control", "no-cache")
	c.Data(nethttp.StatusOK, "text/html; charset=utf-8", content)
}

func serveIndexPage(c *gin.Context, indexPage []byte) {
	c.Header("Cache-Control", "no-cache")
	c.Data(nethttp.StatusOK, "text/html; charset=utf-8", indexPage)
}
