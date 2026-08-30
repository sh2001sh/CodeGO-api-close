package execution

import (
	"bytes"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	gatewayreasoning "github.com/sh2001sh/new-api/internal/gateway/execution/reasoning"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
)

func tryChatCompletionsOriginalBodyFastPath(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	original *dto.GeneralOpenAIRequest,
	prepared *dto.GeneralOpenAIRequest,
) (bool, error) {
	if c == nil || c.Request == nil || info == nil || info.ChannelMeta == nil || original == nil || prepared == nil {
		return false, nil
	}
	if info.RelayMode != gatewaycontract.RelayModeChatCompletions ||
		info.ApiType != constant.APITypeOpenAI ||
		info.ChannelType != constant.ChannelTypeOpenAI ||
		info.IsModelMapped || len(info.ParamOverride) > 0 ||
		info.ChannelSetting.SystemPrompt != "" ||
		shouldBridgeBeforeNative(info, bridgeChatToResponses) {
		return false, nil
	}
	if info.OriginModelName == "" || info.UpstreamModelName != info.OriginModelName ||
		original.Model != info.OriginModelName || prepared.Model != info.UpstreamModelName {
		return false, nil
	}
	if !sameStreamOptions(original.StreamOptions, prepared.StreamOptions) || chatRequestNeedsOpenAIRewrite(info.UpstreamModelName, prepared) {
		return false, nil
	}

	snapshot, err := platformhttpx.GetRequestBodySnapshot(c)
	if err != nil {
		return false, err
	}
	if snapshot == nil || snapshot.Model != info.OriginModelName {
		return false, nil
	}
	if requestBodySnapshotContainsDisabledFields(snapshot, info.ChannelOtherSettings) {
		return false, nil
	}
	return true, nil
}

func sameStreamOptions(left, right *dto.StreamOptions) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.IncludeUsage == right.IncludeUsage && left.IncludeObfuscation == right.IncludeObfuscation
}

func chatRequestNeedsOpenAIRewrite(model string, request *dto.GeneralOpenAIRequest) bool {
	if request == nil || (!strings.HasPrefix(model, "o") && !strings.HasPrefix(model, "gpt-5")) {
		return false
	}
	if lo.FromPtrOr(request.MaxCompletionTokens, uint(0)) == 0 && lo.FromPtrOr(request.MaxTokens, uint(0)) != 0 {
		return true
	}
	if strings.HasPrefix(model, "o") && request.Temperature != nil {
		return true
	}
	if strings.HasPrefix(model, "gpt-5") && (request.Temperature != nil || request.TopP != nil || request.LogProbs != nil) {
		return true
	}
	if effort, _ := gatewayreasoning.ParseOpenAIReasoningEffortFromModelSuffix(model); effort != "" {
		return true
	}
	return len(request.Messages) > 0 && request.Messages[0].Role == "system" &&
		!strings.HasPrefix(model, "o1-mini") && !strings.HasPrefix(model, "o1-preview")
}

func requestBodyContainsDisabledFields(body []byte, settings dto.ChannelOtherSettings) bool {
	if !settings.AllowServiceTier && bytes.Contains(body, []byte(`"service_tier"`)) {
		return true
	}
	if !settings.AllowInferenceGeo && bytes.Contains(body, []byte(`"inference_geo"`)) {
		return true
	}
	if !settings.AllowSpeed && bytes.Contains(body, []byte(`"speed"`)) {
		return true
	}
	if settings.DisableStore && bytes.Contains(body, []byte(`"store"`)) {
		return true
	}
	if !settings.AllowSafetyIdentifier && bytes.Contains(body, []byte(`"safety_identifier"`)) {
		return true
	}
	return !settings.AllowIncludeObfuscation && bytes.Contains(body, []byte(`"include_obfuscation"`))
}

func requestBodySnapshotContainsDisabledFields(snapshot *platformhttpx.RequestBodySnapshot, settings dto.ChannelOtherSettings) bool {
	if snapshot == nil {
		return false
	}
	if !settings.AllowServiceTier && len(snapshot.ServiceTier) > 0 {
		return true
	}
	if !settings.AllowInferenceGeo && len(snapshot.InferenceGeo) > 0 {
		return true
	}
	if !settings.AllowSpeed && len(snapshot.Speed) > 0 {
		return true
	}
	if settings.DisableStore && len(snapshot.Store) > 0 {
		return true
	}
	if !settings.AllowSafetyIdentifier && len(snapshot.SafetyIdentifier) > 0 {
		return true
	}
	return !settings.AllowIncludeObfuscation && bytes.Contains(snapshot.StreamOptions, []byte(`"include_obfuscation"`))
}
