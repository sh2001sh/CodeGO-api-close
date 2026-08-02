package bootstrap

import (
	"context"
	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	bountyapp "github.com/sh2001sh/new-api/internal/bounty/app"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	platformapp "github.com/sh2001sh/new-api/internal/platform/app"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	defaultweb "github.com/sh2001sh/new-api/web/default"
)

func RunLedgerWorker() {
	if err := prepareRuntime("ledger-worker"); err != nil {
		return
	}
	defer closeDatabase()

	if !platformconfig.IsMasterNode {
		platformobservability.FatalLog("ledger-worker requires master node role")
		return
	}

	startLedgerWorkerBackgroundTasks()
	startDiagnostics()

	platformobservability.SysLog("ledger worker maintenance loops started")
	select {}
}

func startLedgerWorkerBackgroundTasks() {
	startOptionSyncLoop()
	billingapp.StartLedgerWorker(context.Background())
	billingapp.StartOperationalSLOMonitor(context.Background())
	commerceapp.StartDailyLuckyNumberTask()
	auditprojection.StartReadModelWorker(context.Background())
	commerceapp.StartSubscriptionMaintenanceTask()
	commerceapp.StartGroupBuySettlementTask()
	identityapp.StartImageWorkspaceCleanupTask()
	bountyapp.StartBountyMaintenanceTask()
	platformapp.StartIndexNowSubmissionTask(defaultweb.BuildFS())
}
