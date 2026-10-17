package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	gatewaygroups "github.com/sh2001sh/new-api/internal/gateway/groupsettings"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestModelRankingsHTTPUsesSamePublicSourcesForSummaryAndTrend(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gatewayschema.Channel{}, &gatewayschema.Model{}, &marketplaceschema.Group{}, &marketplaceschema.Channel{}))
	originalDB, originalGroups := platformdb.DB, gatewaygroups.UserUsableGroups2JSONString()
	originalMySQL, originalPostgres := platformdb.UsingMySQL, platformdb.UsingPostgreSQL
	platformdb.DB, platformdb.UsingMySQL, platformdb.UsingPostgreSQL = db, false, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.UsingMySQL, platformdb.UsingPostgreSQL = originalDB, originalMySQL, originalPostgres
		require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(originalGroups))
	})
	require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(`{"default":"Public"}`))
	require.NoError(t, db.Exec(`CREATE TABLE perf_metrics (
		id INTEGER PRIMARY KEY, model_name TEXT, "group" TEXT, bucket_ts BIGINT,
		request_count BIGINT DEFAULT 0, success_count BIGINT DEFAULT 0, total_latency_ms BIGINT DEFAULT 0,
		ttft_sum_ms BIGINT DEFAULT 0, ttft_count BIGINT DEFAULT 0, output_tokens BIGINT DEFAULT 0,
		generation_ms BIGINT DEFAULT 0, input_tokens BIGINT DEFAULT 0,
		cache_read_tokens BIGINT DEFAULT 0, cache_write_tokens BIGINT DEFAULT 0)`).Error)
	require.NoError(t, db.Create(&gatewayschema.Channel{Id: 1, Models: "shared", Group: "default", ChannelScope: "official"}).Error)
	for _, visibility := range []string{"public", "private"} {
		require.NoError(t, db.Create(&marketplaceschema.Channel{ID: visibility, DeclaredModels: `["shared","third-only","secret"]`}).Error)
		require.NoError(t, db.Create(&marketplaceschema.Group{ID: visibility, ChannelID: visibility, PublicSlug: visibility, InternalGroupName: visibility, Visibility: visibility, LifecycleStatus: "active", VerificationStatus: "passed"}).Error)
	}
	hidden := gatewayschema.Model{ModelName: "secret", Status: 1}
	require.NoError(t, db.Create(&hidden).Error)
	require.NoError(t, db.Model(&hidden).Update("status", 0).Error)
	now := time.Now().Add(-time.Hour).Unix()
	for _, row := range []struct {
		group, model string
		count, input int
	}{
		{"default", "shared", 3, 300}, {"public", "shared", 5, 500},
		{"public", "third-only", 2, 200}, {"public", "secret", 999, 99999},
		{"private", "shared", 1000, 100000}, {"private", "private-only", 1000, 100000},
	} {
		require.NoError(t, db.Exec(`INSERT INTO perf_metrics (model_name, "group", bucket_ts, request_count, input_tokens) VALUES (?, ?, ?, ?, ?)`, row.model, row.group, now, row.count, row.input).Error)
	}
	router := gin.New()
	router.GET("/summary", GetModelRankingSummary)
	router.GET("/trend", GetModelRankingTrend)
	read := func(url string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, url, nil))
		return recorder
	}
	for _, hours := range []string{"24", "168", "720"} {
		response := read("/summary?hours=" + hours + "&group=private")
		require.Equal(t, http.StatusOK, response.Code)
		var summary struct {
			Success bool                             `json:"success"`
			Data    auditprojection.SummaryAllResult `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &summary))
		require.True(t, summary.Success)
		require.Len(t, summary.Data.Models, 2)
		require.Equal(t, "shared", summary.Data.Models[0].ModelName)
		require.EqualValues(t, 8, summary.Data.Models[0].RequestCount)
		require.EqualValues(t, 800, summary.Data.Models[0].InputTokens)
		require.Equal(t, "third-only", summary.Data.Models[1].ModelName)
		response = read("/trend?model=shared&hours=" + hours + "&group=private")
		require.Equal(t, http.StatusOK, response.Code)
		var trend struct {
			Data auditprojection.QueryResult `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &trend))
		require.Len(t, trend.Data.Groups, 2)
		var count int64
		for _, group := range trend.Data.Groups {
			require.Contains(t, []string{"default", "public"}, group.Group)
			for _, point := range group.Series {
				count += point.RequestCount
			}
		}
		require.EqualValues(t, 8, count)
	}
	for _, model := range []string{"secret", "private-only", "shared%27%20OR%201%3D1--"} {
		response := read("/trend?model=" + model)
		require.Equal(t, http.StatusOK, response.Code)
		var trend struct {
			Data auditprojection.QueryResult `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &trend))
		require.Empty(t, trend.Data.Groups)
	}
	require.Equal(t, http.StatusBadRequest, read("/trend").Code)
	require.NoError(t, db.Migrator().DropTable(&marketplaceschema.Group{}))
	response := read("/summary")
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.NotContains(t, response.Body.String(), "no such table")
	require.NotContains(t, response.Body.String(), "SELECT")
}
