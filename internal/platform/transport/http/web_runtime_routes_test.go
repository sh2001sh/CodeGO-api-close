package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	"github.com/sh2001sh/new-api/internal/platform/transport/http/middleware"
)

func TestHomeAndIndexHTMLServeSameEmbeddedShell(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	defaultPage := []byte(`<!doctype html><html lang="zh-CN"><head><title>Code Go | Codex API、Claude Code API、Codex 中转、Claude 中转</title></head><body><h1>Code Go home</h1><div id="root"></div></body></html>`)

	registerEmbeddedWebRoutes(engine, ThemeAssets{DefaultIndexPage: defaultPage})

	for _, path := range []string{"/", "/index.html"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "<title>Code Go | Codex API、Claude Code API、Codex 中转、Claude 中转</title>") {
			t.Fatalf("%s missing expected title: %s", path, body)
		}
		if !strings.Contains(body, "<h1>Code Go home</h1>") {
			t.Fatalf("%s missing expected h1: %s", path, body)
		}
	}
}

func TestEmbeddedStaticRateLimitKeepsNavigationAndAnonymousAPILimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldRedis := platformcache.RedisEnabled
	oldWebEnabled, oldWebLimit, oldWebDuration := platformconfig.GlobalWebRateLimitEnable, platformconfig.GlobalWebRateLimitNum, platformconfig.GlobalWebRateLimitDuration
	oldAPIEnabled, oldAPILimit, oldAPIDuration := platformconfig.GlobalApiRateLimitEnable, platformconfig.GlobalApiRateLimitNum, platformconfig.GlobalApiRateLimitDuration
	oldCriticalEnabled, oldCriticalLimit, oldCriticalDuration := platformconfig.CriticalRateLimitEnable, platformconfig.CriticalRateLimitNum, platformconfig.CriticalRateLimitDuration
	t.Cleanup(func() {
		platformcache.RedisEnabled = oldRedis
		platformconfig.GlobalWebRateLimitEnable, platformconfig.GlobalWebRateLimitNum, platformconfig.GlobalWebRateLimitDuration = oldWebEnabled, oldWebLimit, oldWebDuration
		platformconfig.GlobalApiRateLimitEnable, platformconfig.GlobalApiRateLimitNum, platformconfig.GlobalApiRateLimitDuration = oldAPIEnabled, oldAPILimit, oldAPIDuration
		platformconfig.CriticalRateLimitEnable, platformconfig.CriticalRateLimitNum, platformconfig.CriticalRateLimitDuration = oldCriticalEnabled, oldCriticalLimit, oldCriticalDuration
	})
	platformcache.RedisEnabled = false
	platformconfig.GlobalWebRateLimitEnable, platformconfig.GlobalWebRateLimitNum, platformconfig.GlobalWebRateLimitDuration = true, 2, 600
	platformconfig.GlobalApiRateLimitEnable, platformconfig.GlobalApiRateLimitNum, platformconfig.GlobalApiRateLimitDuration = true, 2, 600
	platformconfig.CriticalRateLimitEnable, platformconfig.CriticalRateLimitNum, platformconfig.CriticalRateLimitDuration = true, 1, 600

	files := fstest.MapFS{
		"static/main.js":      {Data: []byte("window.loaded = true;")},
		"static/main.css":     {Data: []byte("body { color: black; }")},
		"index.html":          {Data: []byte("SPA")},
		"route.js/index.html": {Data: []byte("fallback HTML")},
		"topics/post/main.js": {Data: []byte("topic")},
		"api/unexpected.js":   {Data: []byte("reserved")},
		"v1/unexpected.js":    {Data: []byte("reserved")},
	}
	engine := gin.New()
	api := engine.Group("/api", middleware.GlobalAPIRateLimit())
	api.GET("/status", func(c *gin.Context) { c.Status(http.StatusOK) })
	api.POST("/login", middleware.CriticalRateLimit(), func(c *gin.Context) { c.Status(http.StatusUnauthorized) })
	engine.Use(embeddedWebRateLimit(files, middleware.GlobalWebRateLimit()))
	engine.GET("/index.html", func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.Use(static.Serve("/", &embedFileSystem{FileSystem: http.FS(files)}))
	engine.NoRoute(func(c *gin.Context) { c.Status(http.StatusOK) })
	request := func(method, target, client string) int {
		req := httptest.NewRequest(method, target, nil)
		req.RemoteAddr = client + ":1234"
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec.Code
	}

	// Concurrent cold loads exceed the entire navigation quota without using it.
	var wg sync.WaitGroup
	for i := 0; i < 80; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			method, target := http.MethodGet, "/static/main.js"
			if i%2 == 0 {
				method, target = http.MethodHead, "/static/main.css"
			}
			if code := request(method, target, "198.51.100.221"); code != http.StatusOK {
				t.Errorf("cold asset status = %d", code)
			}
		}(i)
	}
	wg.Wait()
	for i, target := range []string{"/sign-in", "/wallet", "/packages"} {
		want := http.StatusOK
		if i == 2 {
			want = http.StatusTooManyRequests
		}
		if got := request(http.MethodGet, target, "198.51.100.221"); got != want {
			t.Fatalf("navigation %s status = %d, want %d", target, got, want)
		}
	}
	if got := request(http.MethodGet, "/static/main.js", "198.51.100.221"); got != http.StatusOK {
		t.Fatalf("asset after exhausted navigation quota = %d", got)
	}
	for i, target := range []string{"/missing.js", "/index.html", "/route.js"} {
		want := http.StatusOK
		if i == 2 {
			want = http.StatusTooManyRequests
		}
		if got := request(http.MethodGet, target, "198.51.100.222"); got != want {
			t.Fatalf("non-static %s status = %d, want %d", target, got, want)
		}
	}
	for i := 0; i < 3; i++ {
		want := http.StatusOK
		if i == 2 {
			want = http.StatusTooManyRequests
		}
		if got := request(http.MethodGet, "/api/status", "198.51.100.223"); got != want {
			t.Fatalf("anonymous API request %d = %d, want %d", i, got, want)
		}
	}
	for i, want := range []int{http.StatusUnauthorized, http.StatusTooManyRequests} {
		if got := request(http.MethodPost, "/api/login", "198.51.100.224"); got != want {
			t.Fatalf("anonymous login attempt %d = %d, want %d", i, got, want)
		}
	}

	for _, tc := range []struct{ method, target string }{
		{http.MethodGet, "/static/../static/main.js"},
		{http.MethodGet, "//static/main.js"},
		{http.MethodGet, "/static/%6dain.js"},
		{http.MethodGet, "/static%2fmain.js"},
		{http.MethodGet, "/static/main.js%00"},
		{http.MethodGet, "/static%5cmain.js"},
		{http.MethodGet, "/index.html"},
		{http.MethodGet, "/route.js"},
		{http.MethodGet, "/topics/post/main.js"},
		{http.MethodGet, "/api/unexpected.js"},
		{http.MethodGet, "/v1/unexpected.js"},
		{http.MethodPost, "/static/main.js"},
	} {
		if isEmbeddedStaticRequest(httptest.NewRequest(tc.method, tc.target, nil), files) {
			t.Errorf("unexpected static exemption: %s %s", tc.method, tc.target)
		}
	}
}
