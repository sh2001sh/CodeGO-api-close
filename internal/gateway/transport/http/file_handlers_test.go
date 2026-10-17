package http

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewayFiles "github.com/sh2001sh/new-api/internal/gateway/files"
	gatewaySchema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupFileHandlers(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "files.db")), &gorm.Config{})
	require.NoError(t, err)
	oldDB := platformdb.DB
	oldPath := platformcache.GetDiskCachePath()
	platformdb.DB = db
	platformcache.SetDiskCacheConfig(platformcache.DiskCacheConfig{Path: root})
	require.NoError(t, db.AutoMigrate(&gatewaySchema.UserFile{}))
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		platformdb.DB = oldDB
		platformcache.SetDiskCacheConfig(platformcache.DiskCacheConfig{Path: oldPath})
	})
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", 11); c.Set("role", 1); c.Next() })
	router.POST("/v1/files", CreateFile)
	router.GET("/v1/files/:id/content", GetFileContent)
	router.GET("/v1/files/:id/delivery", DeliverFile)
	return router
}

func TestSignedFileDeliveryAllowsValidTokenAndRejectsTampering(t *testing.T) {
	_ = setupFileHandlers(t)
	router := gin.New()
	router.GET("/v1/files/:id/delivery", DeliverFile)
	t.Setenv("FILE_DELIVERY_BASE_URL", "https://api.example.com")
	t.Setenv("FILE_DELIVERY_SIGNING_SECRET", "delivery-test-secret")
	file, err := gatewayFiles.Create(11, "image.png", "vision", "image/png", bytes.NewReader([]byte("image")), 1024)
	require.NoError(t, err)
	deliveryURL, err := gatewayFiles.BuildSignedDeliveryURL(file.ID, time.Now().UTC())
	require.NoError(t, err)

	valid := httptest.NewRecorder()
	router.ServeHTTP(valid, httptest.NewRequest(http.MethodGet, deliveryURL, nil))
	require.Equal(t, http.StatusOK, valid.Code)
	require.Equal(t, "image", valid.Body.String())
	require.Contains(t, valid.Header().Get("Cache-Control"), "public")
	require.Equal(t, `"`+file.SHA256+`"`, valid.Header().Get("ETag"))

	tampered := httptest.NewRecorder()
	router.ServeHTTP(tampered, httptest.NewRequest(http.MethodGet, deliveryURL+"x", nil))
	require.Equal(t, http.StatusNotFound, tampered.Code)
}

func TestSignedFileDeliverySandboxesActiveContentAndPreservesProviderFetches(t *testing.T) {
	_ = setupFileHandlers(t)
	router := gin.New()
	router.GET("/v1/files/:id/delivery", DeliverFile)
	t.Setenv("FILE_DELIVERY_BASE_URL", "https://api.example.com")
	t.Setenv("FILE_DELIVERY_SIGNING_SECRET", "delivery-test-secret")
	for _, tc := range []struct {
		name, mime, body string
	}{
		{"fixture.html", "text/html", "<!doctype html><script>document.title='untrusted'</script>"},
		{"fixture.svg", "image/svg+xml", `<svg xmlns="http://www.w3.org/2000/svg"><script>document.title='untrusted'</script></svg>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := gatewayFiles.Create(11, tc.name, "user_data", tc.mime, bytes.NewBufferString(tc.body), 4096)
			require.NoError(t, err)
			deliveryURL, err := gatewayFiles.BuildSignedDeliveryURL(file.ID, time.Now().UTC())
			require.NoError(t, err)
			// Providers fetch without a bearer token and may replay or request ranges.
			for i := 0; i < 2; i++ {
				result := httptest.NewRecorder()
				router.ServeHTTP(result, httptest.NewRequest(http.MethodGet, deliveryURL, nil))
				require.Equal(t, http.StatusOK, result.Code)
				require.Equal(t, tc.body, result.Body.String())
				require.Equal(t, tc.mime, result.Header().Get("Content-Type"))
				require.Equal(t, "sandbox", result.Header().Get("Content-Security-Policy"))
				require.Contains(t, result.Header().Get("Content-Disposition"), "inline")
			}
			rangeRequest := httptest.NewRequest(http.MethodGet, deliveryURL, nil)
			rangeRequest.Header.Set("Range", "bytes=0-4")
			partial := httptest.NewRecorder()
			router.ServeHTTP(partial, rangeRequest)
			require.Equal(t, http.StatusPartialContent, partial.Code)
			require.Equal(t, tc.body[:5], partial.Body.String())
			require.Equal(t, "sandbox", partial.Header().Get("Content-Security-Policy"))
		})
	}
}

func TestAuthenticatedFileContentSandboxesActiveContent(t *testing.T) {
	router := setupFileHandlers(t)
	body := "<!doctype html><script>document.title='untrusted'</script>"
	file, err := gatewayFiles.Create(11, "fixture.html", "user_data", "text/html", bytes.NewBufferString(body), 4096)
	require.NoError(t, err)
	contentURL := "/v1/files/" + file.ID + "/content"
	result := httptest.NewRecorder()
	router.ServeHTTP(result, httptest.NewRequest(http.MethodGet, contentURL, nil))
	require.Equal(t, http.StatusOK, result.Code)
	require.Equal(t, body, result.Body.String())
	require.Equal(t, "text/html", result.Header().Get("Content-Type"))
	require.Equal(t, "sandbox", result.Header().Get("Content-Security-Policy"))
	require.Equal(t, "nosniff", result.Header().Get("X-Content-Type-Options"))

	request := httptest.NewRequest(http.MethodGet, contentURL, nil)
	request.Header.Set("Range", "bytes=0-4")
	partial := httptest.NewRecorder()
	router.ServeHTTP(partial, request)
	require.Equal(t, http.StatusPartialContent, partial.Code)
	require.Equal(t, body[:5], partial.Body.String())
	require.Equal(t, "sandbox", partial.Header().Get("Content-Security-Policy"))

	other, err := gatewayFiles.Create(12, "other.html", "user_data", "text/html", bytes.NewBufferString(body), 4096)
	require.NoError(t, err)
	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, "/v1/files/"+other.ID+"/content", nil))
	require.Equal(t, http.StatusNotFound, denied.Code)
	require.NotContains(t, denied.Body.String(), "<script>")
}

func TestValidateLocalFileIDsPreservesResponsesRequest(t *testing.T) {
	_ = setupFileHandlers(t)
	file, err := gatewayFiles.Create(11, "image.png", "vision", "image/png", bytes.NewReader([]byte("image")), 1024)
	require.NoError(t, err)
	raw := []byte(`{"model":"gpt-5","input":[{"role":"user","content":[{"type":"input_image","file_id":"` + file.ID + `"}]}]}`)
	storage, err := platformhttpx.CreateBodyStorage(raw)
	require.NoError(t, err)
	defer storage.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("id", 11)
	c.Set(platformhttpx.KeyBodyStorage, storage)
	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, json.Unmarshal(raw, request))
	require.NoError(t, resolveLocalFileIDs(c, request))
	require.Contains(t, string(request.Input), file.ID)
	require.NotContains(t, string(request.Input), "base64,")
	resolved, exists := c.Get(string(constant.ContextKeyResolvedFileReferences))
	require.True(t, exists)
	require.Equal(t, true, resolved)
}

func TestResponsesCompactionPreservesLocalFileIDsUntilChannelSelection(t *testing.T) {
	_ = setupFileHandlers(t)
	file, err := gatewayFiles.Create(11, "history.txt", "user_data", "text/plain", bytes.NewReader([]byte("history")), 1024)
	require.NoError(t, err)
	raw := []byte(`{"model":"gpt-5","input":[{"role":"user","content":[{"type":"input_file","file_id":"` + file.ID + `"}]}]}`)
	storage, err := platformhttpx.CreateBodyStorage(raw)
	require.NoError(t, err)
	defer storage.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 11)
	c.Set(platformhttpx.KeyBodyStorage, storage)

	request, err := getAndValidateResponsesCompactionRequest(c)
	require.NoError(t, err)
	require.Contains(t, string(request.Input), file.ID)
	require.NotContains(t, string(request.Input), "base64,")
}

func TestFileUploadAndContent(t *testing.T) {
	router := setupFileHandlers(t)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "hello.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("hello"))
	require.NoError(t, err)
	require.NoError(t, writer.WriteField("purpose", "user_data"))
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/v1/files", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code)
	var uploaded map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &uploaded))
	id := uploaded["id"].(string)
	require.NotZero(t, uploaded["expires_at"])
	old := time.Now().UTC().Add(-48 * time.Hour)
	require.NoError(t, platformdb.DB.Model(&gatewaySchema.UserFile{}).Where("id = ?", id).Update("last_used_at", old).Error)

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/files/"+id+"/content", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "hello", recorder.Body.String())
	var downloaded gatewaySchema.UserFile
	require.NoError(t, platformdb.DB.Where("id = ?", id).First(&downloaded).Error)
	require.NotNil(t, downloaded.LastUsedAt)
	require.Greater(t, downloaded.LastUsedAt.Unix(), old.Unix())
}
