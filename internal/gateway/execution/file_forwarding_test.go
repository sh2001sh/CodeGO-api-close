package execution

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	openaiadapter "github.com/sh2001sh/new-api/internal/gateway/execution/providers/openai"
	gatewayfiles "github.com/sh2001sh/new-api/internal/gateway/files"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupForwardingTest(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "forwarding.db")), &gorm.Config{})
	require.NoError(t, err)
	oldDB := platformdb.DB
	oldPath := platformcache.GetDiskCachePath()
	platformdb.DB = db
	platformcache.SetDiskCacheConfig(platformcache.DiskCacheConfig{Path: root})
	require.NoError(t, db.AutoMigrate(&gatewayschema.UserFile{}, &gatewayschema.UpstreamFileMapping{}))
	t.Cleanup(func() {
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
		platformdb.DB = oldDB
		platformcache.SetDiskCacheConfig(platformcache.DiskCacheConfig{Path: oldPath})
	})
}

func forwardingContext() *gin.Context {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	context.Request.Header.Set("Content-Type", "application/json")
	context.Set("role", 1)
	return context
}

func forwardingInfo(baseURL, apiKey string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:           7,
		AttemptStartTime: time.Now().UTC(),
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: 9, ChannelType: constant.ChannelTypeOpenAI,
			ApiType: constant.APITypeOpenAI, ApiKey: apiKey,
			ChannelBaseUrl: baseURL,
			ChannelSetting: dto.ChannelSettings{FileInputMode: "auto"},
		},
	}
}

func TestPrepareSelectedChannelFileJSONUploadsOncePerCredential(t *testing.T) {
	setupForwardingTest(t)
	file, err := gatewayfiles.Create(7, "image.png", "vision", "image/png", bytes.NewReader([]byte("image")), 1024)
	require.NoError(t, err)
	var uploads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/v1/files", request.URL.Path)
		require.Contains(t, []string{"Bearer key-a", "Bearer key-b"}, request.Header.Get("Authorization"))
		require.NoError(t, request.ParseMultipartForm(1024))
		opened, _, openErr := request.FormFile("file")
		require.NoError(t, openErr)
		content, readErr := io.ReadAll(opened)
		require.NoError(t, readErr)
		require.Equal(t, "image", string(content))
		_ = opened.Close()
		count := uploads.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, `{"id":"file-upstream-%d"}`, count)
	}))
	defer server.Close()

	adaptor := &openaiadapter.Adaptor{}
	rawSource := map[string]any{"input": []any{map[string]any{"type": "input_image", "file_id": file.ID}}}
	info := forwardingInfo(server.URL, "key-a")
	context := forwardingContext()
	prepared, err := prepareSelectedChannelFileJSON(context, info, adaptor, rawSource, "openai_responses")
	require.NoError(t, err)
	require.Contains(t, string(prepared), `"file_id":"file-upstream-1"`)

	prepared, err = prepareSelectedChannelFileJSON(context, info, adaptor, rawSource, "openai_responses")
	require.NoError(t, err)
	require.Contains(t, string(prepared), `"file_id":"file-upstream-1"`)
	require.EqualValues(t, 1, uploads.Load())

	info.ApiKey = "key-b"
	prepared, err = prepareSelectedChannelFileJSON(context, info, adaptor, rawSource, "openai_responses")
	require.NoError(t, err)
	require.Contains(t, string(prepared), `"file_id":"file-upstream-2"`)
	require.EqualValues(t, 2, uploads.Load())
}

func TestPrepareSelectedChannelFileJSONFallsBackToSignedURL(t *testing.T) {
	setupForwardingTest(t)
	t.Setenv("FILE_DELIVERY_BASE_URL", "https://api.codego.test")
	t.Setenv("FILE_DELIVERY_SIGNING_SECRET", "delivery-test-secret")
	file, err := gatewayfiles.Create(7, "image.png", "vision", "image/png", bytes.NewReader([]byte("image")), 1024)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "unsupported", http.StatusNotFound)
	}))
	defer server.Close()

	info := forwardingInfo(server.URL, "key-a")
	prepared, err := prepareSelectedChannelFileJSON(
		forwardingContext(), info, &openaiadapter.Adaptor{},
		map[string]any{"input": []any{map[string]any{"type": "input_image", "file_id": file.ID}}},
		"openai_responses",
	)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(prepared, &payload))
	item := payload["input"].([]any)[0].(map[string]any)
	require.Contains(t, item["image_url"], "https://api.codego.test/v1/files/")
	require.NotContains(t, item, "file_id")
}
