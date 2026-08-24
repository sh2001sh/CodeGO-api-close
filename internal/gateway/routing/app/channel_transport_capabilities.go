package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sh2001sh/new-api/constant"
	gatewaycapability "github.com/sh2001sh/new-api/internal/gateway/capability"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

const (
	channelCapabilityMaxKeys    = 3
	channelCapabilityMaxModels  = 4
	channelCapabilityMaxAge     = 24 * time.Hour
	channelCapabilityRetryDelay = 15 * time.Minute
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
		if err := probeAndPersistChannelCapabilities(context.Background(), channelID); err != nil {
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
		WebSocket:          result.WebSocket,
		NativeBackground:   result.NativeBackground,
		BackgroundCreate:   result.BackgroundCreate,
		BackgroundResume:   result.BackgroundResume,
		BackgroundCancel:   result.BackgroundCancel,
		RemoteCompactionV1: result.RemoteCompactionV1,
		RemoteCompactionV2: result.RemoteCompactionV2,
	}
	if err := gatewaystore.SaveChannelInfo(channel); err != nil {
		return err
	}
	if responsesCapabilitiesHaveTransientFailure(channel.ChannelInfo.ResponsesCapabilities) {
		time.AfterFunc(channelCapabilityRetryDelay, func() {
			scheduleChannelCapabilityProbe(channelID)
		})
	}
	return nil
}

func responsesCapabilitiesHaveTransientFailure(capabilities gatewayschema.ResponsesCapabilities) bool {
	return gatewaycapability.IsTransientProbeFailure(capabilities.WebSocket) ||
		gatewaycapability.IsTransientProbeFailure(capabilities.NativeBackground) ||
		gatewaycapability.IsTransientProbeFailure(capabilities.BackgroundCreate) ||
		gatewaycapability.IsTransientProbeFailure(capabilities.BackgroundResume) ||
		gatewaycapability.IsTransientProbeFailure(capabilities.BackgroundCancel) ||
		gatewaycapability.IsTransientProbeFailure(capabilities.RemoteCompactionV1) ||
		gatewaycapability.IsTransientProbeFailure(capabilities.RemoteCompactionV2)
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
	protocol := channelProbeProtocol(channel)
	if protocol == gatewaycapability.ProbeProtocolNotApplicable || len(models) == 0 {
		reason := "protocol_not_applicable"
		if len(models) == 0 {
			reason = "no_responses_model"
		}
		return []gatewaycapability.ProbeInput{{
			Model: firstChannelModel(channel), KeyIndex: -1,
			Protocol: gatewaycapability.ProbeProtocolNotApplicable, SkipReason: reason,
		}}
	}
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
		responsesPath, compactPath := channelProbePaths(channel)
		probeKey, headers, ok := channelProbeCredentials(channel, key)
		if !ok {
			continue
		}
		for _, model := range models {
			candidates = append(candidates, gatewaycapability.ProbeInput{
				BaseURL: channel.GetBaseURL(), APIKey: probeKey, Model: model, KeyIndex: keyIndex,
				ResponsesPath: responsesPath, CompactPath: compactPath, Protocol: protocol, Headers: headers,
			})
		}
	}
	return candidates
}

func channelProbeProtocol(channel *gatewayschema.Channel) gatewaycapability.ProbeProtocol {
	if channel == nil {
		return gatewaycapability.ProbeProtocolNotApplicable
	}
	switch channel.Type {
	case constant.ChannelTypeOpenAI:
		return gatewaycapability.ProbeProtocolOpenAIResponses
	case constant.ChannelTypeCodex:
		return gatewaycapability.ProbeProtocolCodexResponses
	default:
		return gatewaycapability.ProbeProtocolNotApplicable
	}
}

func channelProbeCredentials(channel *gatewayschema.Channel, key string) (string, http.Header, bool) {
	if channel == nil || channel.Type != constant.ChannelTypeCodex {
		return strings.TrimSpace(key), nil, strings.TrimSpace(key) != ""
	}
	var credential struct {
		AccessToken string `json:"access_token"`
		AccountID   string `json:"account_id"`
	}
	if platformencoding.Unmarshal([]byte(key), &credential) != nil || strings.TrimSpace(credential.AccessToken) == "" || strings.TrimSpace(credential.AccountID) == "" {
		return "", nil, false
	}
	headers := make(http.Header)
	headers.Set("chatgpt-account-id", strings.TrimSpace(credential.AccountID))
	headers.Set("OpenAI-Beta", "responses=experimental")
	headers.Set("originator", "codex_cli_rs")
	return strings.TrimSpace(credential.AccessToken), headers, true
}

func channelProbePaths(channel *gatewayschema.Channel) (string, string) {
	if channel != nil && channel.Type == constant.ChannelTypeCodex {
		return "/backend-api/codex/responses", "/backend-api/codex/responses/compact"
	}
	return "/v1/responses", "/v1/responses/compact"
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
		if model == "" || !gatewaycapability.IsResponsesProbeModel(model) {
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

func firstChannelModel(channel *gatewayschema.Channel) string {
	if channel == nil {
		return ""
	}
	if channel.TestModel != nil && strings.TrimSpace(*channel.TestModel) != "" {
		return strings.TrimSpace(*channel.TestModel)
	}
	models := channel.GetModels()
	if len(models) == 0 {
		return ""
	}
	return strings.TrimSpace(models[0])
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
	if capabilities.BackgroundCreate.Status != "" || capabilities.BackgroundResume.Status != "" || capabilities.BackgroundCancel.Status != "" {
		states = append(states, capabilities.BackgroundCreate, capabilities.BackgroundResume, capabilities.BackgroundCancel)
	}
	states = append(states, capabilities.RemoteCompactionV1, capabilities.RemoteCompactionV2)
	for _, state := range states {
		if state.Status == "" || state.Status == gatewayschema.CapabilityStatusUnknown || state.Status == gatewayschema.CapabilityStatusPending {
			return true
		}
		if gatewaycapability.IsTransientProbeFailure(state) {
			return true
		}
		if state.CheckedAt <= 0 || now.Sub(time.Unix(state.CheckedAt, 0)) >= channelCapabilityMaxAge {
			return true
		}
	}
	return false
}
