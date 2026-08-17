package bootstrap

import (
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"strconv"

	gatewayexecutionapp "github.com/sh2001sh/new-api/internal/gateway/execution/app"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
)

func startControlBackgroundTasks() {
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
	marketplaceapp.StartMarketplaceTransportCapabilityBackfill()
	gatewayexecutionapp.StartCodexCredentialAutoRefreshTask()
	gatewayroutingapp.StartChannelUpstreamModelUpdateTask()
	gatewayroutingapp.StartChannelTransportCapabilityBackfill()
}
