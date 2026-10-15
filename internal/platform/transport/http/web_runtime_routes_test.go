package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
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
