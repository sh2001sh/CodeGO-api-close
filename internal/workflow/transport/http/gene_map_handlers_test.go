package http

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	workflowschema "github.com/sh2001sh/new-api/internal/workflow/schema"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func setupWorkflowHTTPTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	platformdb.UsingSQLite = true
	platformdb.UsingMySQL = false
	platformdb.UsingPostgreSQL = false
	platformcache.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	platformdb.DB = db
	platformdb.LogDB = db
	require.NoError(t, db.AutoMigrate(&workflowschema.GeneMapShare{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestGetGeneMapShareReturnsStoredSnapshot(t *testing.T) {
	db := setupWorkflowHTTPTestDB(t)

	snapshotJSON := `{"generated_at":1720000000,"window_days":30,"owner_label":"A*e","archetype":"全栈均衡派","tagline":"stable","share_caption":"caption","dominant_model":"gpt-4o","total_requests":3,"total_quota":1200,"total_tokens":2400,"models":[{"model":"gpt-4o","requests":3,"quota":1200,"token_used":2400,"share":1}],"time_bands":[],"rare_models":[]}`
	share := &workflowschema.GeneMapShare{
		UserId:       1,
		ShareToken:   "share-token-123",
		OwnerLabel:   "A*e",
		Headline:     "全栈均衡派",
		SnapshotJSON: snapshotJSON,
	}
	require.NoError(t, db.Create(share).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "token", Value: "share-token-123"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/gene-map/share/share-token-123", nil)

	GetGeneMapShare(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Token     string `json:"token"`
			Headline  string `json:"headline"`
			ShareText string `json:"share_text"`
			Snapshot  struct {
				OwnerLabel string `json:"owner_label"`
			} `json:"snapshot"`
		} `json:"data"`
	}
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Equal(t, "share-token-123", response.Data.Token)
	require.Equal(t, "全栈均衡派", response.Data.Headline)
	require.Equal(t, "caption", response.Data.ShareText)
	require.Equal(t, "A*e", response.Data.Snapshot.OwnerLabel)
}
