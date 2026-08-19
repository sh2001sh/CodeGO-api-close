package bootstrap

import (
	"context"
	"fmt"
	"time"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformarchive "github.com/sh2001sh/new-api/internal/platform/archivex"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

func startDataArchiveMaintenance(ctx context.Context) {
	setting := platformarchive.SettingsFromEnv()
	if !setting.Enabled() {
		platformobservability.SysLog("data archive maintenance disabled: DATA_ARCHIVE_DIR is not configured")
		return
	}
	sink, err := platformarchive.NewFileSink(setting.Directory)
	if err != nil {
		platformobservability.SysError("data archive maintenance failed to start: " + err.Error())
		return
	}
	platformobservability.SysLog(fmt.Sprintf(
		"data archive maintenance started: interval=%s batch=%d log_retention=%dd gateway_retention=%dd",
		setting.Interval,
		setting.BatchSize,
		setting.LogRetentionDays,
		setting.GatewayExecutionRetentionDays,
	))
	go runDataArchiveMaintenance(ctx, sink, setting)
}

func runDataArchiveMaintenance(ctx context.Context, sink platformarchive.Sink, setting platformarchive.Settings) {
	ticker := time.NewTicker(setting.Interval)
	defer ticker.Stop()
	for {
		runDataArchiveCycle(ctx, sink, setting, time.Now().UTC())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runDataArchiveCycle(ctx context.Context, sink platformarchive.Sink, setting platformarchive.Settings, now time.Time) {
	if setting.LogRetentionDays > 0 {
		archived, err := auditprojection.ArchiveOldLogsBatch(ctx, sink, now, setting.LogRetentionDays, setting.BatchSize)
		reportArchiveCycle("logs", archived, err)
	}
	if setting.GatewayExecutionRetentionDays > 0 {
		archived, err := gatewaystore.ArchiveSettledExecutionsBatch(ctx, sink, now, setting.GatewayExecutionRetentionDays, setting.BatchSize)
		reportArchiveCycle("gateway executions", archived, err)
	}
}

func reportArchiveCycle(dataset string, archived int64, err error) {
	if err != nil {
		platformobservability.SysError(dataset + " archive batch failed: " + err.Error())
		return
	}
	if archived > 0 {
		platformobservability.SysLog(fmt.Sprintf("%s archive batch completed: rows=%d", dataset, archived))
	}
}
