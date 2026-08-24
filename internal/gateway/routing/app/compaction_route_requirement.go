package app

import (
	"encoding/json"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
)

// RemoteCompactionRequirement identifies the native remote compaction protocol
// a route must support before it is used for the current request.
type RemoteCompactionRequirement string

const (
	RemoteCompactionRequirementNone RemoteCompactionRequirement = ""
	RemoteCompactionRequirementV1   RemoteCompactionRequirement = "v1"
	RemoteCompactionRequirementV2   RemoteCompactionRequirement = "v2"
)

// SetRemoteCompactionRouteRequirement records the protocol required by the
// request before channel selection starts. It is deliberately request-local:
// capability state is persisted on each channel, while this requirement comes
// from the client's endpoint and headers.
func SetRemoteCompactionRouteRequirement(c *gin.Context, info *relaycommon.RelayInfo) {
	if c == nil {
		return
	}
	requirement := RemoteCompactionRequirementNone
	if info != nil {
		switch info.RelayMode {
		case gatewaycontract.RelayModeResponsesCompact:
			requirement = RemoteCompactionRequirementV1
		case gatewaycontract.RelayModeResponses:
			if c.Request != nil && gatewaycontract.HasRemoteCompactionV2(c.Request.Header) && requestHasCompactionTrigger(info.Request) {
				requirement = RemoteCompactionRequirementV2
			}
		}
	}
	httpctx.SetContextKey(c, constant.ContextKeyResponsesCompactionRequirement, string(requirement))
}

func remoteCompactionRequirement(c *gin.Context) RemoteCompactionRequirement {
	if c == nil {
		return RemoteCompactionRequirementNone
	}
	value := httpctx.GetContextKeyString(c, constant.ContextKeyResponsesCompactionRequirement)
	switch RemoteCompactionRequirement(strings.TrimSpace(value)) {
	case RemoteCompactionRequirementV1:
		return RemoteCompactionRequirementV1
	case RemoteCompactionRequirementV2:
		return RemoteCompactionRequirementV2
	default:
		return RemoteCompactionRequirementNone
	}
}

// remoteCompactionCapabilityRank returns -1 for a definitive incompatibility,
// 0 for a confirmed capability, and 1 for an unverified or transient result.
// Unknown routes remain eligible as a last resort while confirmed support is
// preferred, so a rolling backfill does not turn every request into a 503.
func remoteCompactionCapabilityRank(c *gin.Context, channel *gatewayschema.Channel, modelName string) int {
	if channel == nil {
		return -1
	}
	switch remoteCompactionRequirement(c) {
	case RemoteCompactionRequirementV1:
		return capabilityRank(channel.ChannelInfo.ResponsesCapabilities.RemoteCompactionV1, modelName)
	case RemoteCompactionRequirementV2:
		return capabilityRank(channel.ChannelInfo.ResponsesCapabilities.RemoteCompactionV2, modelName)
	default:
		return 0
	}
}

func capabilityRank(state gatewayschema.CapabilityProbeState, modelName string) int {
	if state.Status == gatewayschema.CapabilityStatusUnsupported {
		return -1
	}
	if state.Status != gatewayschema.CapabilityStatusSupported {
		return 1
	}
	if strings.TrimSpace(state.Model) != "" && strings.TrimSpace(modelName) != "" &&
		!strings.EqualFold(strings.TrimSpace(state.Model), strings.TrimSpace(modelName)) {
		// The compact capability probe stops at the first successful model. A
		// success for one model therefore provides no negative evidence for the
		// other advertised models; keep them eligible as last-resort candidates.
		return 1
	}
	return 0
}

func requestHasCompactionTrigger(request dto.Request) bool {
	responses, ok := request.(*dto.OpenAIResponsesRequest)
	if !ok || len(responses.Input) == 0 {
		return false
	}
	var items []struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(responses.Input, &items); err != nil {
		return false
	}
	for _, item := range items {
		if item.Type == "compaction_trigger" {
			return true
		}
	}
	return false
}
