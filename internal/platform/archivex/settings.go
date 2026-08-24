package archivex

import (
	"strings"
	"time"

	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
)

const (
	defaultLogRetentionDays              = 90
	defaultGatewayExecutionRetentionDays = 90
	defaultArchiveBatchSize              = 2000
	defaultArchiveIntervalMinutes        = 1
)

type Settings struct {
	Directory                     string
	LogRetentionDays              int
	GatewayExecutionRetentionDays int
	BatchSize                     int
	Interval                      time.Duration
}

func SettingsFromEnv() Settings {
	return Settings{
		Directory:                     strings.TrimSpace(platformconfig.GetEnvOrDefaultString("DATA_ARCHIVE_DIR", "")),
		LogRetentionDays:              nonNegativeEnv("LOG_ARCHIVE_RETENTION_DAYS", defaultLogRetentionDays),
		GatewayExecutionRetentionDays: nonNegativeEnv("GATEWAY_EXECUTION_ARCHIVE_RETENTION_DAYS", defaultGatewayExecutionRetentionDays),
		BatchSize:                     positiveEnv("DATA_ARCHIVE_BATCH_SIZE", defaultArchiveBatchSize),
		Interval: time.Duration(positiveEnv(
			"DATA_ARCHIVE_INTERVAL_MINUTES",
			defaultArchiveIntervalMinutes,
		)) * time.Minute,
	}
}

func (setting Settings) Enabled() bool {
	return setting.Directory != "" && (setting.LogRetentionDays > 0 || setting.GatewayExecutionRetentionDays > 0)
}

func nonNegativeEnv(key string, fallback int) int {
	value := platformconfig.GetEnvOrDefaultInt(key, fallback)
	if value < 0 {
		return fallback
	}
	return value
}

func positiveEnv(key string, fallback int) int {
	value := platformconfig.GetEnvOrDefaultInt(key, fallback)
	if value <= 0 {
		return fallback
	}
	return value
}
