package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestEmailVerificationClientIPTrustBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	require.NoError(t, engine.SetTrustedProxies([]string{"127.0.0.0/8", "10.0.0.0/8"}))
	engine.GET("/", func(c *gin.Context) { c.String(http.StatusOK, emailVerificationClientIP(c)) })
	for _, tc := range []struct{ name, peer, forwarded, cf, want string }{
		{"direct-spoof", "8.8.8.8:123", "104.16.1.1", "1.1.1.1", "8.8.8.8"},
		{"local-proxy-direct-spoof", "127.0.0.1:123", "8.8.4.4", "1.1.1.1", "8.8.4.4"},
		{"cdn-client-a", "127.0.0.1:123", "104.16.1.1", "1.1.1.1", "1.1.1.1"},
		{"cdn-client-b", "127.0.0.1:123", "104.16.1.1", "9.9.9.9", "9.9.9.9"},
		{"cdn-ipv6", "127.0.0.1:123", "2606:4700::1111", "2001:4860:4860::8888", "2001:4860:4860::8888"},
		{"private-header", "127.0.0.1:123", "104.16.1.1", "10.0.0.7", "104.16.1.1"},
		{"invalid-header", "127.0.0.1:123", "104.16.1.1", "malformed", "104.16.1.1"},
		{"no-header", "127.0.0.1:123", "104.16.1.1", "", "104.16.1.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("X-Forwarded-For", tc.forwarded)
			req.Header.Set("CF-Connecting-IP", tc.cf)
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)
			require.Equal(t, tc.want, w.Body.String())
		})
	}
}

func TestEmailVerificationRateLimitSeparatesClientsAndFailsClosed(t *testing.T) {
	oldRedis, oldRDB := platformcache.RedisEnabled, platformcache.RDB
	platformcache.RedisEnabled = false
	t.Cleanup(func() { platformcache.RedisEnabled, platformcache.RDB = oldRedis, oldRDB })
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	require.NoError(t, engine.SetTrustedProxies([]string{"127.0.0.0/8"}))
	engine.GET("/", EmailVerificationRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := func(ip string, want int) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "127.0.0.1:123"
		r.Header.Set("X-Forwarded-For", "104.16.1.1")
		r.Header.Set("CF-Connecting-IP", ip)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		require.Equal(t, want, w.Code, w.Body.String())
	}
	request("8.8.8.201", http.StatusNoContent)
	request("8.8.8.201", http.StatusNoContent)
	request("8.8.8.201", http.StatusTooManyRequests)
	request("8.8.8.202", http.StatusNoContent)
	platformcache.RedisEnabled, platformcache.RDB = true, nil
	request("8.8.8.202", http.StatusServiceUnavailable)
}

func TestTokenAuthReadOnlyRejectsInvalidLifecycleAndEnforcesUserStatus(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "readonly-auth.db")), &gorm.Config{})
	require.NoError(t, err)
	oldDB, oldRedis := platformdb.DB, platformcache.RedisEnabled
	platformdb.DB, platformcache.RedisEnabled = db, false
	t.Cleanup(func() {
		platformdb.DB, platformcache.RedisEnabled = oldDB, oldRedis
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &identityschema.Token{}))
	engine := gin.New()
	engine.GET("/", TokenAuthReadOnly(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for i, tc := range []struct {
		name              string
		status            int
		expiry            int64
		quota             int
		unlimited, banned bool
		want              int
	}{
		{"valid", constant.TokenStatusEnabled, -1, 1, false, false, http.StatusNoContent},
		{"unlimited-zero", constant.TokenStatusEnabled, -1, 0, true, false, http.StatusNoContent},
		{"disabled", constant.TokenStatusDisabled, -1, 10, false, false, http.StatusUnauthorized},
		{"expired-status", constant.TokenStatusExpired, -1, 10, false, false, http.StatusUnauthorized},
		{"exhausted-status", constant.TokenStatusExhausted, -1, 10, false, false, http.StatusUnauthorized},
		{"expired-time", constant.TokenStatusEnabled, time.Now().Add(-time.Minute).Unix(), 10, false, false, http.StatusUnauthorized},
		{"finite-zero", constant.TokenStatusEnabled, -1, 0, false, false, http.StatusUnauthorized},
		{"banned-user", constant.TokenStatusEnabled, -1, 10, false, true, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := identityschema.User{Id: 95001 + i, Username: fmt.Sprintf("read-only-%d", i), Status: constant.UserStatusEnabled, AffCode: fmt.Sprintf("READ%d", i)}
			if tc.banned {
				user.Status = constant.UserStatusDisabled
			}
			require.NoError(t, db.Create(&user).Error)
			token := identityschema.Token{UserId: user.Id, Key: fmt.Sprintf("%048d", i+1), Status: tc.status, ExpiredTime: tc.expiry, RemainQuota: tc.quota, UnlimitedQuota: tc.unlimited}
			require.NoError(t, db.Create(&token).Error)
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("Authorization", "Bearer sk-"+token.Key)
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, r)
			require.Equal(t, tc.want, w.Code, w.Body.String())
		})
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer sk-"+strings.Repeat("z", 48))
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, r)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.NoError(t, db.Migrator().DropTable(&identityschema.Token{}))
	w = httptest.NewRecorder()
	engine.ServeHTTP(w, r)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}
