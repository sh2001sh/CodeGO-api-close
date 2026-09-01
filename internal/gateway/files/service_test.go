package files

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	gatewaySchema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupFileTestDB(t *testing.T) {
	t.Helper()
	oldDB := platformdb.DB
	oldPath := platformcache.GetDiskCachePath()
	root := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "files.db")), &gorm.Config{})
	require.NoError(t, err)
	platformdb.DB = db
	platformcache.SetDiskCacheConfig(platformcache.DiskCacheConfig{Enabled: false, Path: root})
	require.NoError(t, db.AutoMigrate(&gatewaySchema.UserFile{}, &gatewaySchema.UpstreamFileMapping{}))
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		platformdb.DB = oldDB
		platformcache.SetDiskCacheConfig(platformcache.DiskCacheConfig{Path: oldPath})
	})
}

func TestCreateAndResolveFileID(t *testing.T) {
	setupFileTestDB(t)
	file, err := Create(7, "sample.png", "vision", "image/png", bytes.NewReader([]byte("png-data")), 1024)
	require.NoError(t, err)
	resolved, err := ResolveFileIDsJSON([]byte(`{"input":[{"type":"input_image","file_id":"`+file.ID+`"}]}`), 7, false)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(resolved, &payload))
	input := payload["input"].([]any)[0].(map[string]any)
	require.Equal(t, "input_image", input["type"])
	require.NotEmpty(t, input["image_url"])
	require.NotContains(t, input, "file_id")
}

func TestResolveFileIDEnforcesOwner(t *testing.T) {
	setupFileTestDB(t)
	file, err := Create(7, "sample.txt", "user_data", "text/plain", bytes.NewReader([]byte("hello")), 1024)
	require.NoError(t, err)
	_, err = ResolveFileIDsJSON([]byte(`{"file_id":"`+file.ID+`"}`), 8, false)
	require.Error(t, err)
}

func TestCreateDeduplicatesPerUserAndDeleteInvalidatesReference(t *testing.T) {
	setupFileTestDB(t)
	first, err := Create(7, "first.txt", "user_data", "text/plain", bytes.NewReader([]byte("same")), 1024)
	require.NoError(t, err)
	second, err := Create(7, "second.txt", "user_data", "text/plain", bytes.NewReader([]byte("same")), 1024)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	otherUser, err := Create(8, "same.txt", "user_data", "text/plain", bytes.NewReader([]byte("same")), 1024)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, otherUser.ID)
	require.NoError(t, Delete(first.ID, 7, false))
	_, err = ResolveFileIDsJSON([]byte(`{"file_id":"`+first.ID+`"}`), 7, false)
	require.Error(t, err)
}

func TestResolveChatImageFileID(t *testing.T) {
	setupFileTestDB(t)
	file, err := Create(7, "image.png", "vision", "image/png", bytes.NewReader([]byte("image")), 1024)
	require.NoError(t, err)
	raw := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"file_id":"` + file.ID + `","detail":"high"}}]}]}`)
	resolved, err := ResolveFileIDsJSON(raw, 7, false)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(resolved, &payload))
	image := payload["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["image_url"].(map[string]any)
	require.NotEmpty(t, image["url"])
	require.Equal(t, "high", image["detail"])
	require.NotContains(t, image, "file_id")
}

func TestCreateEnforcesPerUserStorageQuota(t *testing.T) {
	setupFileTestDB(t)
	t.Setenv("FILE_STORAGE_USER_LIMIT_MB", "1")
	_, err := Create(7, "large.bin", "user_data", "application/octet-stream", bytes.NewReader(make([]byte, (1<<20)+1)), 2<<20)
	require.ErrorIs(t, err, ErrStorageQuota)
}

func TestResolveFileIDRefreshesLastUsedAt(t *testing.T) {
	setupFileTestDB(t)
	t.Setenv("FILE_STORAGE_RETENTION_DAYS", "30")
	file, err := Create(7, "history.txt", "user_data", "text/plain", bytes.NewReader([]byte("history")), 1024)
	require.NoError(t, err)
	old := time.Now().UTC().Add(-48 * time.Hour)
	require.NoError(t, platformdb.DB.Model(file).Update("last_used_at", old).Error)

	_, err = ResolveFileIDsJSON([]byte(`{"file_id":"`+file.ID+`"}`), 7, false)
	require.NoError(t, err)
	var refreshed gatewaySchema.UserFile
	require.NoError(t, platformdb.DB.Where("id = ?", file.ID).First(&refreshed).Error)
	require.NotNil(t, refreshed.LastUsedAt)
	require.Greater(t, refreshed.LastUsedAt.Unix(), old.Unix())
}

func TestCleanupExpiredFilesRemovesMetadataAndContent(t *testing.T) {
	setupFileTestDB(t)
	t.Setenv("FILE_STORAGE_RETENTION_DAYS", "7")
	file, err := Create(7, "expired.txt", "user_data", "text/plain", bytes.NewReader([]byte("expired")), 1024)
	require.NoError(t, err)
	old := time.Now().UTC().Add(-8 * 24 * time.Hour)
	require.NoError(t, platformdb.DB.Model(file).Update("last_used_at", old).Error)

	_, err = Get(file.ID, 7, false)
	require.ErrorIs(t, err, ErrNotFound)
	deleted, err := cleanupExpiredFiles(time.Now().UTC(), retentionSettingsFromEnv())
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	var stored gatewaySchema.UserFile
	require.ErrorIs(t, platformdb.DB.Where("id = ?", file.ID).First(&stored).Error, gorm.ErrRecordNotFound)
	_, err = os.Stat(contentPath(file.StoragePath))
	require.True(t, errors.Is(err, os.ErrNotExist))
}

func TestCleanupKeepsRecentlyUsedFiles(t *testing.T) {
	setupFileTestDB(t)
	t.Setenv("FILE_STORAGE_RETENTION_DAYS", "7")
	file, err := Create(7, "active.txt", "user_data", "text/plain", bytes.NewReader([]byte("active")), 1024)
	require.NoError(t, err)

	deleted, err := cleanupExpiredFiles(time.Now().UTC(), retentionSettingsFromEnv())
	require.NoError(t, err)
	require.Zero(t, deleted)
	_, err = Get(file.ID, 7, false)
	require.NoError(t, err)
	_, err = os.Stat(contentPath(file.StoragePath))
	require.NoError(t, err)
}

func TestListExcludesExpiredFiles(t *testing.T) {
	setupFileTestDB(t)
	t.Setenv("FILE_STORAGE_RETENTION_DAYS", "7")
	expired, err := Create(7, "expired.txt", "user_data", "text/plain", bytes.NewReader([]byte("expired")), 1024)
	require.NoError(t, err)
	active, err := Create(7, "active.txt", "user_data", "text/plain", bytes.NewReader([]byte("active")), 1024)
	require.NoError(t, err)
	old := time.Now().UTC().Add(-8 * 24 * time.Hour)
	require.NoError(t, platformdb.DB.Model(expired).Update("last_used_at", old).Error)

	files, err := List(7, false, 20, "")
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, active.ID, files[0].ID)
}

func TestReuploadReplacesExpiredDeduplicatedFile(t *testing.T) {
	setupFileTestDB(t)
	t.Setenv("FILE_STORAGE_RETENTION_DAYS", "7")
	first, err := Create(7, "first.txt", "user_data", "text/plain", bytes.NewReader([]byte("same")), 1024)
	require.NoError(t, err)
	old := time.Now().UTC().Add(-8 * 24 * time.Hour)
	require.NoError(t, platformdb.DB.Model(first).Update("last_used_at", old).Error)

	second, err := Create(7, "second.txt", "user_data", "text/plain", bytes.NewReader([]byte("same")), 1024)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	_, err = os.Stat(contentPath(first.StoragePath))
	require.True(t, errors.Is(err, os.ErrNotExist))
	_, err = Get(second.ID, 7, false)
	require.NoError(t, err)
}

func TestCleanupDoesNotDeleteFileRefreshedAfterSelection(t *testing.T) {
	setupFileTestDB(t)
	t.Setenv("FILE_STORAGE_RETENTION_DAYS", "7")
	file, err := Create(7, "refreshed.txt", "user_data", "text/plain", bytes.NewReader([]byte("refreshed")), 1024)
	require.NoError(t, err)
	now := time.Now().UTC()
	old := now.Add(-8 * 24 * time.Hour)
	require.NoError(t, platformdb.DB.Model(file).Update("last_used_at", old).Error)
	file.LastUsedAt = &old

	require.NoError(t, platformdb.DB.Model(file).Update("last_used_at", now).Error)
	removed, err := deleteExpiredFile(file, now.Add(-7*24*time.Hour))
	require.NoError(t, err)
	require.False(t, removed)
	_, err = os.Stat(contentPath(file.StoragePath))
	require.NoError(t, err)
}

func TestCleanupRetriesAfterContentRemovalFailure(t *testing.T) {
	setupFileTestDB(t)
	t.Setenv("FILE_STORAGE_RETENTION_DAYS", "7")
	file, err := Create(7, "retry.txt", "user_data", "text/plain", bytes.NewReader([]byte("retry")), 1024)
	require.NoError(t, err)
	now := time.Now().UTC()
	old := now.Add(-8 * 24 * time.Hour)
	require.NoError(t, platformdb.DB.Model(file).Update("last_used_at", old).Error)
	path := contentPath(file.StoragePath)
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Mkdir(path, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(path, "blocked"), []byte("blocked"), 0600))

	deleted, err := cleanupExpiredFiles(now, retentionSettingsFromEnv())
	require.Error(t, err)
	require.Zero(t, deleted)
	var retained gatewaySchema.UserFile
	require.NoError(t, platformdb.DB.Where("id = ?", file.ID).First(&retained).Error)
	require.NoError(t, os.Remove(filepath.Join(path, "blocked")))
	require.NoError(t, os.Remove(path))

	deleted, err = cleanupExpiredFiles(now, retentionSettingsFromEnv())
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
}

func TestZeroRetentionDisablesExpiration(t *testing.T) {
	setupFileTestDB(t)
	t.Setenv("FILE_STORAGE_RETENTION_DAYS", "0")
	file, err := Create(7, "kept.txt", "user_data", "text/plain", bytes.NewReader([]byte("kept")), 1024)
	require.NoError(t, err)
	old := time.Now().UTC().Add(-365 * 24 * time.Hour)
	require.NoError(t, platformdb.DB.Model(file).Update("last_used_at", old).Error)

	_, err = Get(file.ID, 7, false)
	require.NoError(t, err)
	deleted, err := cleanupExpiredFiles(time.Now().UTC(), retentionSettingsFromEnv())
	require.NoError(t, err)
	require.Zero(t, deleted)
}
