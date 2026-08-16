package bootstrap

import (
	"context"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	workflowapp "github.com/sh2001sh/new-api/internal/workflow/app"
	workflowtemporal "github.com/sh2001sh/new-api/internal/workflow/temporal"
)

func RunWorkflowWorker() {
	if err := prepareRuntime("workflow-worker"); err != nil {
		return
	}
	defer closeDatabase()

	cfg := workflowtemporal.LoadConfigFromEnv()
	if !platformconfig.IsMasterNode {
		platformobservability.FatalLog("workflow-worker requires master node role")
		return
	}
	if err := startWorkflowWorkerBackgroundTasks(); err != nil {
		platformobservability.FatalLog("failed to wire workflow runtime: " + err.Error())
		return
	}
	startDiagnostics()

	platformobservability.SysLog("workflow worker temporal runtime enabled")
	if err := workflowtemporal.RunWorker(cfg, workflowtemporal.NewDefaultWorkerDependencies()); err != nil {
		platformobservability.FatalLog("workflow-worker temporal bootstrap failed: " + err.Error())
	}
}

func startWorkflowWorkerBackgroundTasks() error {
	startOptionSyncLoop()
	if err := applyRuntimeWiring("workflow-worker"); err != nil {
		return err
	}
	workflowapp.StartSubscriptionResetScheduler(context.Background())
	commerceapp.StartPendingOrderFulfillmentScheduler(context.Background())
	marketplaceapp.StartGPT56MappingScheduler(context.Background())
	return nil
}
