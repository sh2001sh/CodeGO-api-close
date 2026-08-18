package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	gatewaycapability "github.com/sh2001sh/new-api/internal/gateway/capability"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
)

const (
	marketplaceCapabilityProbeTimeout = 4 * time.Minute
	marketplaceCapabilityMaxModels    = 4
	marketplaceCapabilityMaxAge       = 24 * time.Hour
	marketplaceCapabilityRetryDelay   = 15 * time.Minute
)

var (
	marketplaceCapabilityProbeInFlight sync.Map
	queueMarketplaceCapabilityProbe    = scheduleMarketplaceCapabilityProbe
	probeMarketplaceCandidates         = gatewaycapability.ProbeResponsesTransportsForCandidates
)

// StartMarketplaceTransportCapabilityBackfill probes marketplace channels
// whose persisted transport result is missing, interrupted, or stale.
func StartMarketplaceTransportCapabilityBackfill() {
	go func() {
		var channels []marketplaceschema.Channel
		if err := platformdb.DB.Find(&channels).Error; err != nil {
			platformobservability.SysLog("failed to load marketplace transport capability backfill: " + err.Error())
			return
		}
		for index := range channels {
			channel := &channels[index]
			if !marketplaceCapabilitiesNeedProbe(channel.TransportCapabilities, time.Now()) {
				continue
			}
			markMarketplaceCapabilitiesPending(channel)
			if err := platformdb.DB.Model(channel).Update("transport_capabilities", channel.TransportCapabilities).Error; err != nil {
				platformobservability.SysLog(fmt.Sprintf("failed to mark marketplace transport capability pending: channel_id=%s error=%v", channel.ID, err))
				continue
			}
			queueMarketplaceCapabilityProbe(channel.ID)
		}
	}()
}

func scheduleMarketplaceCapabilityProbe(channelID string) {
	if strings.TrimSpace(channelID) == "" {
		return
	}
	if _, loaded := marketplaceCapabilityProbeInFlight.LoadOrStore(channelID, struct{}{}); loaded {
		return
	}
	go func() {
		defer marketplaceCapabilityProbeInFlight.Delete(channelID)
		ctx, cancel := context.WithTimeout(context.Background(), marketplaceCapabilityProbeTimeout)
		defer cancel()
		if err := probeAndPersistMarketplaceCapabilities(ctx, channelID); err != nil {
			platformobservability.SysLog(fmt.Sprintf("marketplace transport capability probe failed: channel_id=%s error=%v", channelID, err))
		}
	}()
}

func probeAndPersistMarketplaceCapabilities(ctx context.Context, channelID string) error {
	var channel marketplaceschema.Channel
	if err := platformdb.DB.First(&channel, "id = ?", channelID).Error; err != nil {
		return err
	}
	result := probeMarketplaceCandidates(ctx, marketplaceProbeCandidates(&channel))
	capabilities := gatewayschema.ResponsesCapabilities{
		WebSocket:        result.WebSocket,
		NativeBackground: result.NativeBackground,
		BackgroundCreate: result.BackgroundCreate,
		BackgroundResume: result.BackgroundResume,
		BackgroundCancel: result.BackgroundCancel,
	}
	raw, err := platformencoding.Marshal(capabilities)
	if err != nil {
		return err
	}
	channel.TransportCapabilities = string(raw)
	if err := platformdb.DB.Model(&channel).Update("transport_capabilities", channel.TransportCapabilities).Error; err != nil {
		return err
	}
	if err := syncMarketplaceCapabilitiesToInternal(&channel, capabilities); err != nil {
		return err
	}
	if marketplaceCapabilitiesHaveTransientFailure(capabilities) {
		time.AfterFunc(marketplaceCapabilityRetryDelay, func() {
			scheduleMarketplaceCapabilityProbe(channelID)
		})
	}
	return nil
}

func marketplaceCapabilitiesHaveTransientFailure(capabilities gatewayschema.ResponsesCapabilities) bool {
	return gatewaycapability.IsTransientProbeFailure(capabilities.WebSocket) ||
		gatewaycapability.IsTransientProbeFailure(capabilities.NativeBackground) ||
		gatewaycapability.IsTransientProbeFailure(capabilities.BackgroundCreate) ||
		gatewaycapability.IsTransientProbeFailure(capabilities.BackgroundResume) ||
		gatewaycapability.IsTransientProbeFailure(capabilities.BackgroundCancel)
}

func markMarketplaceCapabilitiesPending(channel *marketplaceschema.Channel) {
	if channel == nil {
		return
	}
	capabilities := gatewaycapability.PendingResponsesCapabilities(firstMarketplaceProbeModel(channel))
	raw, err := platformencoding.Marshal(capabilities)
	if err == nil {
		channel.TransportCapabilities = string(raw)
	}
}

func marketplaceProbeCandidates(channel *marketplaceschema.Channel) []gatewaycapability.ProbeInput {
	if channel == nil {
		return nil
	}
	baseURL, err := platformsecurity.DecryptSecret(channel.BaseURLCiphertext)
	if err != nil {
		return nil
	}
	key, err := platformsecurity.DecryptSecret(channel.CredentialCiphertext)
	if err != nil || strings.TrimSpace(key) == "" {
		return nil
	}
	models := marketplaceProbeModels(channel)
	candidates := make([]gatewaycapability.ProbeInput, 0, len(models))
	for _, model := range models {
		candidates = append(candidates, gatewaycapability.ProbeInput{
			BaseURL: baseURL, APIKey: key, Model: model, KeyIndex: 0,
		})
	}
	return candidates
}

func marketplaceProbeModels(channel *marketplaceschema.Channel) []string {
	if channel == nil {
		return nil
	}
	values := append([]string{channel.AutoProbeModel}, decodeModels(channel.DeclaredModels)...)
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
		if len(models) == marketplaceCapabilityMaxModels {
			break
		}
	}
	return models
}

func firstMarketplaceProbeModel(channel *marketplaceschema.Channel) string {
	models := marketplaceProbeModels(channel)
	if len(models) == 0 {
		return ""
	}
	return models[0]
}

func decodeMarketplaceCapabilities(raw string) gatewayschema.ResponsesCapabilities {
	var capabilities gatewayschema.ResponsesCapabilities
	_ = platformencoding.Unmarshal([]byte(raw), &capabilities)
	return capabilities
}

func marketplaceCapabilitiesNeedProbe(raw string, now time.Time) bool {
	if strings.TrimSpace(raw) == "" {
		return true
	}
	capabilities := decodeMarketplaceCapabilities(raw)
	states := []gatewayschema.CapabilityProbeState{capabilities.WebSocket, capabilities.NativeBackground, capabilities.BackgroundCreate, capabilities.BackgroundResume, capabilities.BackgroundCancel}
	if capabilities.BackgroundCreate.Status == "" && capabilities.BackgroundResume.Status == "" && capabilities.BackgroundCancel.Status == "" {
		states = states[:2]
	}
	for _, state := range states {
		if state.Status == "" || state.Status == gatewayschema.CapabilityStatusUnknown || state.Status == gatewayschema.CapabilityStatusPending {
			return true
		}
		if gatewaycapability.IsTransientProbeFailure(state) {
			return true
		}
		if state.CheckedAt <= 0 || now.Sub(time.Unix(state.CheckedAt, 0)) >= marketplaceCapabilityMaxAge {
			return true
		}
	}
	return false
}

func syncMarketplaceCapabilitiesToInternal(channel *marketplaceschema.Channel, capabilities gatewayschema.ResponsesCapabilities) error {
	if channel == nil || channel.InternalChannelID == nil {
		return nil
	}
	internal, err := gatewaystore.LoadChannelByID(*channel.InternalChannelID, true)
	if err != nil {
		return err
	}
	internal.ChannelInfo.ResponsesCapabilities = capabilities
	return gatewaystore.SaveChannelInfo(internal)
}

func marketplaceTransportFingerprint(channel *marketplaceschema.Channel) string {
	if channel == nil {
		return ""
	}
	return strings.Join([]string{
		channel.ProviderType,
		channel.BaseURLCiphertext,
		channel.CredentialCiphertext,
		channel.DeclaredModels,
		channel.AutoProbeModel,
	}, "\x00")
}
