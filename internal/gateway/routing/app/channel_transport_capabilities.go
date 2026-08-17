package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sh2001sh/new-api/constant"
	gatewaycapability "github.com/sh2001sh/new-api/internal/gateway/capability"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

const (
	channelCapabilityProbeTimeout = 4 * time.Minute
	channelCapabilityMaxKeys      = 3
	channelCapabilityMaxModels    = 4
	channelCapabilityMaxAge       = 24 * time.Hour
)

var (
	channelCapabilityProbeInFlight sync.Map
	queueChannelCapabilityProbe    = scheduleChannelCapabilityProbe
	probeChannelCandidates         = gatewaycapability.ProbeResponsesTransportsForCandidates
)

// StartChannelTransportCapabilityBackfill probes official channels whose
// persisted transport result is missing, interrupted, or stale.
func StartChannelTransportCapabilityBackfill() {
	go func() {
		channels, err := gatewaystore.ListAllChannels(0, 0, true, true)
		if err != nil {
			platformobservability.SysLog("failed to load channels for transport capability backfill: " + err.Error())
			return
		}
		for _, channel := range channels {
			if channel == nil || !channel.IsOfficial() || !responsesCapabilitiesNeedProbe(channel.ChannelInfo.ResponsesCapabilities, time.Now()) {
				continue
			}
			markChannelCapabilitiesPending(channel)
			queueChannelCapabilityProbe(channel.Id)
		}
	}()
}

func scheduleChannelCapabilityProbe(channelID int) {
	if channelID <= 0 {
		return
	}
	if _, loaded := channelCapabilityProbeInFlight.LoadOrStore(channelID, struct{}{}); loaded {
		return
	}
	go func() {
		defer channelCapabilityProbeInFlight.Delete(channelID)
		ctx, cancel := context.WithTimeout(context.Background(), channelCapabilityProbeTimeout)
		defer cancel()
		if err := probeAndPersistChannelCapabilities(ctx, channelID); err != nil {
			platformobservability.SysLog(fmt.Sprintf("channel transport capability probe failed: channel_id=%d error=%v", channelID, err))
		}
	}()
}

func probeAndPersistChannelCapabilities(ctx context.Context, channelID int) error {
	channel, err := gatewaystore.LoadChannelByID(channelID, true)
	if err != nil {
		return err
	}
	candidates := channelProbeCandidates(channel)
	result := probeChannelCandidates(ctx, candidates)
	channel.ChannelInfo.ResponsesCapabilities = gatewayschema.ResponsesCapabilities{
		WebSocket:        result.WebSocket,
		NativeBackground: result.NativeBackground,
	}
	return gatewaystore.SaveChannelInfo(channel)
}

func markChannelCapabilitiesPending(channel *gatewayschema.Channel) {
	if channel == nil {
		return
	}
	channel.ChannelInfo.ResponsesCapabilities = gatewaycapability.PendingResponsesCapabilities(firstChannelProbeModel(channel))
	if channel.Id > 0 {
		if err := gatewaystore.SaveChannelInfo(channel); err != nil {
			platformobservability.SysLog(fmt.Sprintf("failed to mark channel transport capability pending: channel_id=%d error=%v", channel.Id, err))
		}
	}
}

func channelProbeCandidates(channel *gatewayschema.Channel) []gatewaycapability.ProbeInput {
	if channel == nil {
		return nil
	}
	models := channelProbeModels(channel)
	keys := channel.GetKeys()
	candidates := make([]gatewaycapability.ProbeInput, 0, len(models)*len(keys))
	usedKeys := 0
	for keyIndex, key := range keys {
		if usedKeys == channelCapabilityMaxKeys {
			break
		}
		if !channelProbeKeyEnabled(channel, keyIndex) || strings.TrimSpace(key) == "" {
			continue
		}
		usedKeys++
		for _, model := range models {
			candidates = append(candidates, gatewaycapability.ProbeInput{
				BaseURL: channel.GetBaseURL(), APIKey: key, Model: model, KeyIndex: keyIndex,
			})
		}
	}
	return candidates
}

func channelProbeModels(channel *gatewayschema.Channel) []string {
	if channel == nil {
		return nil
	}
	values := make([]string, 0, len(channel.GetModels())+1)
	if channel.TestModel != nil {
		values = append(values, *channel.TestModel)
	}
	values = append(values, channel.GetModels()...)
	seen := make(map[string]struct{}, len(values))
	models := make([]string, 0, len(values))
	for _, value := range values {
		model := strings.TrimSpace(value)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		models = append(models, model)
		if len(models) == channelCapabilityMaxModels {
			break
		}
	}
	return models
}

func firstChannelProbeModel(channel *gatewayschema.Channel) string {
	models := channelProbeModels(channel)
	if len(models) == 0 {
		return ""
	}
	return models[0]
}

func channelProbeKeyEnabled(channel *gatewayschema.Channel, index int) bool {
	if !channel.ChannelInfo.IsMultiKey || channel.ChannelInfo.MultiKeyStatusList == nil {
		return true
	}
	status, exists := channel.ChannelInfo.MultiKeyStatusList[index]
	return !exists || status == constant.ChannelStatusEnabled
}

func responsesCapabilitiesNeedProbe(capabilities gatewayschema.ResponsesCapabilities, now time.Time) bool {
	states := []gatewayschema.CapabilityProbeState{capabilities.WebSocket, capabilities.NativeBackground}
	for _, state := range states {
		if state.Status == "" || state.Status == gatewayschema.CapabilityStatusUnknown || state.Status == gatewayschema.CapabilityStatusPending {
			return true
		}
		if state.CheckedAt <= 0 || now.Sub(time.Unix(state.CheckedAt, 0)) >= channelCapabilityMaxAge {
			return true
		}
	}
	return false
}
