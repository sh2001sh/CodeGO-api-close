package files

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
)

const defaultUserStorageLimitMB = 1024
const defaultFileRetentionDays = 30
const defaultCleanupIntervalMinutes = 60
const defaultCleanupBatchSize = 200
const fileUsageTouchInterval = time.Hour

type retentionSettings struct {
	Retention       time.Duration
	CleanupInterval time.Duration
	BatchSize       int
}

func storageDir() string {
	if configured := strings.TrimSpace(os.Getenv("FILE_STORAGE_PATH")); configured != "" {
		return configured
	}
	root := platformcache.GetDiskCachePath()
	if root == "" {
		root = os.TempDir()
	}
	return filepath.Join(root, "new-api-files")
}

func userStorageLimitBytes() int64 {
	limitMB := positiveEnvInt("FILE_STORAGE_USER_LIMIT_MB", defaultUserStorageLimitMB)
	return int64(limitMB) << 20
}

func retentionSettingsFromEnv() retentionSettings {
	days := nonNegativeEnvInt("FILE_STORAGE_RETENTION_DAYS", defaultFileRetentionDays)
	interval := positiveEnvInt("FILE_STORAGE_CLEANUP_INTERVAL_MINUTES", defaultCleanupIntervalMinutes)
	return retentionSettings{
		Retention:       time.Duration(days) * 24 * time.Hour,
		CleanupInterval: time.Duration(interval) * time.Minute,
		BatchSize:       defaultCleanupBatchSize,
	}
}

func positiveEnvInt(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func nonNegativeEnvInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}
