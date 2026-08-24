package bootstrap

import (
	"context"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	marketplacesettlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"strconv"

	gatewayexecutionapp "github.com/sh2001sh/new-api/internal/gateway/execution/app"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
)

func startControlBackgroundTasks() {
	// The control API exposes administrator settlement actions. Register the
	// same idempotent wallet-credit hook used by the ledger worker so those
	// actions work when the control plane is run independently.
	marketplacesettlement.RegisterReleaseHook(billingapp.CreditMarketplaceOwnerEarningsTx)
	marketplacesettlement.StartReleaseWorker(context.Background())

	startOptionSyncLoop()

	if frequencyText := getenvTrimmed("CHANNEL_UPDATE_FREQUENCY"); frequencyText != "" {
		frequency, err := strconv.Atoi(frequencyText)
		if err != nil {
			platformobservability.FatalLog("failed to parse CHANNEL_UPDATE_FREQUENCY: " + err.Error())
			return
		}
		gatewayexecutionapp.StartChannelBalanceUpdateTask(frequency)
	}

	gatewayexecutionapp.StartAutomaticChannelTestTask()
	marketplaceapp.StartMarketplaceAutoProbeTask()
	marketplaceapp.StartMarketplaceRankingTask()
	marketplaceapp.StartMarketplaceTransportCapabilityBackfill()
	gatewayexecutionapp.StartCodexCredentialAutoRefreshTask()
	gatewayroutingapp.StartChannelUpstreamModelUpdateTask()
	gatewayroutingapp.StartChannelTransportCapabilityBackfill()
}
