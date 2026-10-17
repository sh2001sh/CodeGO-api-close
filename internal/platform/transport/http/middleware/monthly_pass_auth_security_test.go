package middleware

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTokenAuthRechecksMonthlyPassOnEveryRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth.db")), &gorm.Config{})
	require.NoError(t, err)
	oldDB, oldRedis := platformdb.DB, platformcache.RedisEnabled
	platformdb.DB, platformcache.RedisEnabled = db, false
	t.Cleanup(func() {
		platformdb.DB, platformcache.RedisEnabled = oldDB, oldRedis
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &identityschema.Token{}, &commerceschema.BlindBoxProp{}))
	user := identityschema.User{Id: 92001, Username: "monthly-auth", Status: constant.UserStatusEnabled, Group: "default", Role: constant.RoleCommonUser}
	require.NoError(t, db.Create(&user).Error)
	token := identityschema.Token{UserId: user.Id, Key: strings.Repeat("x", 48), Group: commerceapp.LegacyMonthlyPassGroup, UnlimitedQuota: true, Status: constant.TokenStatusEnabled, ExpiredTime: -1}
	require.NoError(t, db.Create(&token).Error)
	engine := gin.New()
	engine.GET("/v1/models", TokenAuth(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := func(want int) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		r.Header.Set("Authorization", "Bearer sk-"+token.Key)
		engine.ServeHTTP(w, r)
		require.Equal(t, want, w.Code, w.Body.String())
	}
	request(http.StatusForbidden)
	prop := commerceschema.BlindBoxProp{UserId: user.Id, PropType: commerceschema.BlindBoxPropTypeMonthlyPassMultiplier, Status: commerceschema.BlindBoxPropStatusActive, ExpiresAt: platformruntime.GetTimestamp() + 3600}
	require.NoError(t, db.Create(&prop).Error)
	request(http.StatusNoContent)
	for _, state := range []string{commerceschema.BlindBoxPropStatusPaused, commerceschema.BlindBoxPropStatusAvailable} {
		require.NoError(t, db.Model(&prop).Update("status", state).Error)
		request(http.StatusForbidden)
	}
	require.NoError(t, db.Model(&prop).Updates(map[string]any{"status": commerceschema.BlindBoxPropStatusActive, "expires_at": platformruntime.GetTimestamp() - 1}).Error)
	request(http.StatusForbidden)
	require.NoError(t, db.Migrator().DropTable(&commerceschema.BlindBoxProp{}))
	request(http.StatusForbidden)
}
