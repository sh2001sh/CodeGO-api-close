package schema

import (
	"database/sql/driver"
	"encoding/json"
	"github.com/sh2001sh/new-api/constant"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"strings"
)

const (
	ChannelScopeOfficial = "official"
	ChannelScopeExternal = "external"
)

type Channel struct {
	Id                               int     `json:"id"`
	Type                             int     `json:"type" gorm:"default:0"`
	Key                              string  `json:"key" gorm:"not null"`
	OpenAIOrganization               *string `json:"openai_organization"`
	TestModel                        *string `json:"test_model"`
	Status                           int     `json:"status" gorm:"default:1"`
	Name                             string  `json:"name" gorm:"index"`
	ChannelScope                     string  `json:"channel_scope" gorm:"column:channel_scope;type:varchar(16);not null;default:'official';index"`
	MarketplaceMaxConcurrency        int     `json:"marketplace_max_concurrency" gorm:"column:marketplace_max_concurrency;not null;default:0"`
	MarketplaceUserMaxConcurrency    int     `json:"marketplace_user_max_concurrency" gorm:"column:marketplace_user_max_concurrency;not null;default:0"`
	SensitiveWordInterceptionEnabled *bool   `json:"sensitive_word_interception_enabled" gorm:"column:sensitive_word_interception_enabled;default:true"`
	Weight                           *uint   `json:"weight" gorm:"default:0"`
	CreatedTime                      int64   `json:"created_time" gorm:"bigint"`
	TestTime                         int64   `json:"test_time" gorm:"bigint"`
	ResponseTime                     int     `json:"response_time"` // in milliseconds
	BaseURL                          *string `json:"base_url" gorm:"column:base_url;default:''"`
	Other                            string  `json:"other"`
	Balance                          float64 `json:"balance"` // in USD
	BalanceUpdatedTime               int64   `json:"balance_updated_time" gorm:"bigint"`
	Models                           string  `json:"models"`
	Group                            string  `json:"group" gorm:"type:varchar(64);default:'default'"`
	UsedQuota                        int64   `json:"used_quota" gorm:"bigint;default:0"`
	ModelMapping                     *string `json:"model_mapping" gorm:"type:text"`
	//MaxInputTokens     *int    `json:"max_input_tokens" gorm:"default:0"`
	StatusCodeMapping *string `json:"status_code_mapping" gorm:"type:varchar(1024);default:''"`
	Priority          *int64  `json:"priority" gorm:"bigint;default:0"`
	AutoBan           *int    `json:"auto_ban" gorm:"default:1"`
	OtherInfo         string  `json:"other_info"`
	Tag               *string `json:"tag" gorm:"index"`
	Setting           *string `json:"setting" gorm:"type:text"` // 渠道额外设置
	ParamOverride     *string `json:"param_override" gorm:"type:text"`
	HeaderOverride    *string `json:"header_override" gorm:"type:text"`
	Remark            *string `json:"remark" gorm:"type:varchar(255)" validate:"max=255"`
	// add after v0.8.5
	ChannelInfo ChannelInfo `json:"channel_info" gorm:"type:json"`

	OtherSettings string `json:"settings" gorm:"column:settings"` // 其他设置，存储azure版本等不需要检索的信息，详见dto.ChannelOtherSettings

	// cache info
	Keys []string `json:"-" gorm:"-"`
}

func (channel *Channel) IsOfficial() bool {
	return strings.ToLower(strings.TrimSpace(channel.ChannelScope)) != ChannelScopeExternal
}

func (channel *Channel) ShouldInterceptSensitiveWords() bool {
	return channel == nil || channel.SensitiveWordInterceptionEnabled == nil || *channel.SensitiveWordInterceptionEnabled
}

type ChannelInfo struct {
	IsMultiKey             bool                  `json:"is_multi_key"`                        // 是否多Key模式
	MultiKeySize           int                   `json:"multi_key_size"`                      // 多Key模式下的Key数量
	MultiKeyStatusList     map[int]int           `json:"multi_key_status_list"`               // key状态列表，key index -> status
	MultiKeyDisabledReason map[int]string        `json:"multi_key_disabled_reason,omitempty"` // key禁用原因列表，key index -> reason
	MultiKeyDisabledTime   map[int]int64         `json:"multi_key_disabled_time,omitempty"`   // key禁用时间列表，key index -> time
	MultiKeyPollingIndex   int                   `json:"multi_key_polling_index"`             // 多Key模式下轮询的key索引
	MultiKeyMode           constant.MultiKeyMode `json:"multi_key_mode"`
	ResponsesCapabilities  ResponsesCapabilities `json:"responses_capabilities,omitempty"`
}

const (
	CapabilityStatusUnknown     = "unknown"
	CapabilityStatusPending     = "pending"
	CapabilityStatusSupported   = "supported"
	CapabilityStatusUnsupported = "unsupported"
	CapabilityStatusError       = "error"
)

type CapabilityProbeState struct {
	Status      string `json:"status,omitempty"`
	CheckedAt   int64  `json:"checked_at,omitempty"`
	Model       string `json:"model,omitempty"`
	ErrorClass  string `json:"error_class,omitempty"`
	HTTPStatus  int    `json:"http_status,omitempty"`
	ProbeKeyIdx int    `json:"probe_key_index,omitempty"`
}

type ResponsesCapabilities struct {
	WebSocket        CapabilityProbeState `json:"websocket,omitempty"`
	NativeBackground CapabilityProbeState `json:"native_background,omitempty"`
	BackgroundCreate CapabilityProbeState `json:"background_create,omitempty"`
	BackgroundResume CapabilityProbeState `json:"background_resume,omitempty"`
	BackgroundCancel CapabilityProbeState `json:"background_cancel,omitempty"`
}

func (capabilities ResponsesCapabilities) SupportsWebSocket() bool {
	return capabilities.WebSocket.Status == CapabilityStatusSupported
}

func (capabilities ResponsesCapabilities) SupportsNativeBackground() bool {
	if capabilities.BackgroundCreate.Status == "" && capabilities.BackgroundResume.Status == "" && capabilities.BackgroundCancel.Status == "" {
		return capabilities.NativeBackground.Status == CapabilityStatusSupported
	}
	return capabilities.SupportsNativeBackgroundFor("", -1)
}

func (capabilities ResponsesCapabilities) SupportsWebSocketFor(model string, keyIndex int) bool {
	return capabilityStateSupports(capabilities.WebSocket, model, keyIndex)
}

func (capabilities ResponsesCapabilities) SupportsNativeBackgroundFor(model string, keyIndex int) bool {
	if capabilities.BackgroundCreate.Status == "" && capabilities.BackgroundResume.Status == "" && capabilities.BackgroundCancel.Status == "" {
		return capabilityStateSupports(capabilities.NativeBackground, model, keyIndex)
	}
	state := capabilities.NativeBackground
	if state.Status == "" {
		state = capabilities.BackgroundCreate
	}
	return capabilityStateSupports(state, model, keyIndex) &&
		capabilityStateSupports(capabilities.BackgroundCreate, model, keyIndex) &&
		capabilityStateSupports(capabilities.BackgroundResume, model, keyIndex) &&
		capabilityStateSupports(capabilities.BackgroundCancel, model, keyIndex)
}

func capabilityStateSupports(state CapabilityProbeState, model string, keyIndex int) bool {
	if state.Status != CapabilityStatusSupported {
		return false
	}
	if strings.TrimSpace(model) != "" && strings.TrimSpace(state.Model) != "" && !strings.EqualFold(strings.TrimSpace(model), strings.TrimSpace(state.Model)) {
		return false
	}
	return keyIndex < 0 || state.ProbeKeyIdx < 0 || state.ProbeKeyIdx == keyIndex
}

// Value implements driver.Valuer interface
func (c ChannelInfo) Value() (driver.Value, error) {
	return platformencoding.Marshal(&c)
}

// Scan implements sql.Scanner interface
func (c *ChannelInfo) Scan(value interface{}) error {
	bytesValue, _ := value.([]byte)
	return platformencoding.Unmarshal(bytesValue, c)
}

func (channel *Channel) GetKeys() []string {
	return channelKeysFromRaw(channel.keySource())
}

func (channel *Channel) GetModels() []string {
	if channel.Models == "" {
		return []string{}
	}
	rawModels := strings.Split(strings.Trim(channel.Models, ","), ",")
	models := make([]string, 0, len(rawModels))
	for _, model := range rawModels {
		if model = strings.TrimSpace(model); model != "" {
			models = append(models, model)
		}
	}
	return models
}

func (channel *Channel) GetGroups() []string {
	if channel.Group == "" {
		return []string{}
	}
	rawGroups := strings.Split(strings.Trim(channel.Group, ","), ",")
	groups := make([]string, 0, len(rawGroups))
	for _, group := range rawGroups {
		if group = strings.TrimSpace(group); group != "" {
			groups = append(groups, group)
		}
	}
	return groups
}

func (channel *Channel) GetTag() string {
	if channel.Tag == nil {
		return ""
	}
	return *channel.Tag
}

func (channel *Channel) SetTag(tag string) {
	channel.Tag = &tag
}

func (channel *Channel) GetAutoBan() bool {
	if channel.AutoBan == nil {
		return false
	}
	return *channel.AutoBan == 1
}

func (channel *Channel) GetPriority() int64 {
	if channel.Priority == nil {
		return 0
	}
	return *channel.Priority
}

func (channel *Channel) GetWeight() int {
	if channel.Weight == nil {
		return 0
	}
	return int(*channel.Weight)
}

func (channel *Channel) GetBaseURL() string {
	if channel.BaseURL == nil {
		return ""
	}
	url, err := platformsecurity.DecryptSecret(*channel.BaseURL)
	if err != nil {
		return ""
	}
	if url == "" {
		url = constant.ChannelBaseURLs[channel.Type]
	}
	return url
}

func (channel *Channel) GetModelMapping() string {
	if channel.ModelMapping == nil {
		return ""
	}
	return *channel.ModelMapping
}

func (channel *Channel) GetStatusCodeMapping() string {
	if channel.StatusCodeMapping == nil {
		return ""
	}
	return *channel.StatusCodeMapping
}

func (channel *Channel) keySource() string {
	if len(channel.Keys) > 0 {
		return strings.Join(channel.Keys, "\n")
	}
	key, err := platformsecurity.DecryptSecret(channel.Key)
	if err != nil {
		return ""
	}
	return key
}

func channelKeysFromRaw(raw string) []string {
	if raw == "" {
		return []string{}
	}

	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "[") {
		var arr []json.RawMessage
		if err := platformencoding.Unmarshal([]byte(trimmed), &arr); err == nil {
			keys := make([]string, len(arr))
			for index, value := range arr {
				keys[index] = string(value)
			}
			return keys
		}
	}

	return strings.Split(strings.Trim(raw, "\n"), "\n")
}
