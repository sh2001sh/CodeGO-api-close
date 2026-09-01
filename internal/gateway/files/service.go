package files

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	gatewaySchema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/types"
	"gorm.io/gorm"
)

var ErrNotFound = gorm.ErrRecordNotFound
var ErrTooLarge = errors.New("file too large")
var ErrStorageQuota = errors.New("file storage quota exceeded")

const defaultPurpose = "user_data"

// Create persists an uploaded file and deduplicates identical content per user.
func Create(userID int, filename, purpose, mimeType string, src io.Reader, maxBytes int64) (*gatewaySchema.UserFile, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user")
	}
	if strings.TrimSpace(purpose) == "" {
		purpose = defaultPurpose
	}
	if err := os.MkdirAll(storageDir(), 0700); err != nil {
		return nil, fmt.Errorf("create file storage: %w", err)
	}
	tmpPath, written, digest, err := writeUploadTemp(src, maxBytes)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(tmpPath) }()
	existing, err := findByDigest(userID, digest)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		retention := retentionSettingsFromEnv().Retention
		now := time.Now().UTC()
		if !isExpiredAt(existing, now, retention) {
			return existing, markUsedAt(existing, now, retention)
		}
		if _, err := deleteExpiredFile(existing, now.Add(-retention)); err != nil {
			return nil, err
		}
	}
	if err := ensureUserStorageQuota(userID, written); err != nil {
		return nil, err
	}
	return persistUpload(userID, filename, purpose, mimeType, tmpPath, digest, written)
}

func findByDigest(userID int, digest string) (*gatewaySchema.UserFile, error) {
	var file gatewaySchema.UserFile
	err := platformdb.DB.Where("user_id = ? AND sha256 = ?", userID, digest).First(&file).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &file, err
}

func ensureUserStorageQuota(userID int, incomingBytes int64) error {
	var usedBytes int64
	if err := platformdb.DB.Model(&gatewaySchema.UserFile{}).Where("user_id = ?", userID).
		Select("COALESCE(SUM(size), 0)").Scan(&usedBytes).Error; err != nil {
		return err
	}
	if usedBytes+incomingBytes > userStorageLimitBytes() {
		return fmt.Errorf("%w: maximum is %d MB", ErrStorageQuota, userStorageLimitBytes()>>20)
	}
	return nil
}

func persistUpload(userID int, filename, purpose, mimeType, tmpPath, digest string, written int64) (*gatewaySchema.UserFile, error) {
	id := types.LocalFileIDPrefix + strings.ReplaceAll(uuid.NewString(), "-", "")
	path := filepath.Join(storageDir(), id)
	if err := os.Rename(tmpPath, path); err != nil {
		return nil, fmt.Errorf("persist uploaded file: %w", err)
	}
	now := time.Now().UTC()
	file := &gatewaySchema.UserFile{
		ID: id, UserID: userID, SHA256: digest, Purpose: purpose,
		Filename: filepath.Base(filename), MimeType: mimeType, Size: written,
		StoragePath: id, CreatedAt: now, LastUsedAt: &now,
	}
	if err := platformdb.DB.Create(file).Error; err != nil {
		_ = os.Remove(path)
		if existing, lookupErr := findByDigest(userID, digest); lookupErr == nil && existing != nil && !isExpiredAt(existing, now, retentionSettingsFromEnv().Retention) {
			return existing, markUsedAt(existing, now, retentionSettingsFromEnv().Retention)
		}
		return nil, err
	}
	return file, nil
}

func writeUploadTemp(src io.Reader, maxBytes int64) (string, int64, string, error) {
	tmp, err := os.CreateTemp(storageDir(), ".upload-*")
	if err != nil {
		return "", 0, "", fmt.Errorf("create temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(src, maxBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return "", 0, "", fmt.Errorf("read upload: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return "", 0, "", closeErr
	}
	if written > maxBytes {
		_ = os.Remove(tmpPath)
		return "", 0, "", fmt.Errorf("%w: maximum size is %d bytes", ErrTooLarge, maxBytes)
	}
	return tmpPath, written, fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// Get returns file metadata when the caller owns the file or is an administrator.
func Get(id string, userID int, admin bool) (*gatewaySchema.UserFile, error) {
	var file gatewaySchema.UserFile
	query := platformdb.DB.Where("id = ?", id)
	if !admin {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.First(&file).Error; err != nil {
		return nil, err
	}
	if isExpiredAt(&file, time.Now().UTC(), retentionSettingsFromEnv().Retention) {
		return nil, gorm.ErrRecordNotFound
	}
	return &file, nil
}

// ReadContent reads the persisted bytes for a file metadata record.
func ReadContent(file *gatewaySchema.UserFile) ([]byte, error) {
	if file == nil || file.StoragePath == "" {
		return nil, errors.New("file content unavailable")
	}
	return os.ReadFile(contentPath(file.StoragePath))
}

// OpenContent opens persisted content for streaming downloads.
func OpenContent(file *gatewaySchema.UserFile) (*os.File, error) {
	if file == nil || file.StoragePath == "" {
		return nil, errors.New("file content unavailable")
	}
	return os.Open(contentPath(file.StoragePath))
}

func contentPath(stored string) string {
	if filepath.IsAbs(stored) {
		return stored
	}
	return filepath.Join(storageDir(), filepath.Base(stored))
}

// Delete removes an authorized file record and its persisted content.
func Delete(id string, userID int, admin bool) error {
	file, err := Get(id, userID, admin)
	if err != nil {
		return err
	}
	if err := platformdb.DB.Delete(&gatewaySchema.UserFile{}, "id = ?", id).Error; err != nil {
		return err
	}
	if err := deleteUpstreamMappings(platformdb.DB, id); err != nil {
		return err
	}
	if file.StoragePath != "" {
		if removeErr := os.Remove(contentPath(file.StoragePath)); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("remove file content: %w", removeErr)
		}
	}
	return nil
}

// List returns files visible to the caller in reverse creation order.
func List(userID int, admin bool, limit int, after string) ([]gatewaySchema.UserFile, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	query := activeFilesQuery(platformdb.DB, time.Now().UTC()).Order("created_at DESC").Limit(limit)
	if !admin {
		query = query.Where("user_id = ?", userID)
	}
	if after != "" {
		var cursor gatewaySchema.UserFile
		cursorQuery := platformdb.DB.Where("id = ?", after)
		if !admin {
			cursorQuery = cursorQuery.Where("user_id = ?", userID)
		}
		if err := cursorQuery.First(&cursor).Error; err == nil {
			query = query.Where("created_at < ?", cursor.CreatedAt)
		}
	}
	var files []gatewaySchema.UserFile
	return files, query.Find(&files).Error
}
