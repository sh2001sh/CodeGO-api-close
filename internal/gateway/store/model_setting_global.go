package store

import (
	"slices"
	"strings"

	"github.com/sh2001sh/new-api/setting/config"
)

type ProtocolBridgeMode string

const (
	ProtocolBridgeModeAuto     ProtocolBridgeMode = "auto"
	ProtocolBridgeModeForce    ProtocolBridgeMode = "force"
	ProtocolBridgeModeDisabled ProtocolBridgeMode = "disabled"
)

type ProtocolBridgePolicy struct {
	Mode          ProtocolBridgeMode `json:"mode,omitempty"`
	Enabled       bool               `json:"enabled"`
	AllChannels   bool               `json:"all_channels"`
	ChannelIDs    []int              `json:"channel_ids,omitempty"`
	ChannelTypes  []int              `json:"channel_types,omitempty"`
	ModelPatterns []string           `json:"model_patterns,omitempty"`
}

type ChatCompletionsToResponsesPolicy = ProtocolBridgePolicy

func (p ProtocolBridgePolicy) EffectiveMode() ProtocolBridgeMode {
	switch p.Mode {
	case ProtocolBridgeModeForce, ProtocolBridgeModeDisabled:
		return p.Mode
	case ProtocolBridgeModeAuto:
		return ProtocolBridgeModeAuto
	case "":
		if p.Enabled {
			return ProtocolBridgeModeForce
		}
	}
	return ProtocolBridgeModeAuto
}

func (p ProtocolBridgePolicy) MatchesChannel(channelID int, channelType int) bool {
	if p.AllChannels {
		return true
	}

	if channelID > 0 && len(p.ChannelIDs) > 0 && slices.Contains(p.ChannelIDs, channelID) {
		return true
	}
	if channelType > 0 && len(p.ChannelTypes) > 0 && slices.Contains(p.ChannelTypes, channelType) {
		return true
	}
	return false
}

func (p ProtocolBridgePolicy) IsChannelEnabled(channelID int, channelType int) bool {
	return p.EffectiveMode() == ProtocolBridgeModeForce && p.MatchesChannel(channelID, channelType)
}

type GlobalSettings struct {
	PassThroughRequestEnabled        bool                 `json:"pass_through_request_enabled"`
	ThinkingModelBlacklist           []string             `json:"thinking_model_blacklist"`
	ChatCompletionsToResponsesPolicy ProtocolBridgePolicy `json:"chat_completions_to_responses_policy"`
	ResponsesToChatCompletionsPolicy ProtocolBridgePolicy `json:"responses_to_chat_completions_policy"`
}

var defaultOpenAISettings = GlobalSettings{
	PassThroughRequestEnabled: false,
	ThinkingModelBlacklist: []string{
		"moonshotai/kimi-k2-thinking",
		"kimi-k2-thinking",
	},
	ChatCompletionsToResponsesPolicy: ChatCompletionsToResponsesPolicy{
		Mode:        ProtocolBridgeModeAuto,
		AllChannels: true,
	},
	ResponsesToChatCompletionsPolicy: ChatCompletionsToResponsesPolicy{
		Mode:        ProtocolBridgeModeAuto,
		AllChannels: true,
	},
}

var globalSettings = defaultOpenAISettings

func init() {
	config.GlobalConfig.Register("global", &globalSettings)
}

func GetGlobalSettings() *GlobalSettings {
	return &globalSettings
}

func ShouldPreserveThinkingSuffix(modelName string) bool {
	target := strings.TrimSpace(modelName)
	if target == "" {
		return false
	}

	for _, entry := range globalSettings.ThinkingModelBlacklist {
		if strings.TrimSpace(entry) == target {
			return true
		}
	}
	return false
}
