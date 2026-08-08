package runtime

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/dto"
	"github.com/sh2001sh/new-api/types"
)

type RequestType string

const (
	RequestTypeChatShortStream RequestType = "chat_short_stream"
	RequestTypeChatLongStream  RequestType = "chat_long_stream"
	RequestTypeToolCallStream  RequestType = "tool_call_stream"
	RequestTypeChatNonStream   RequestType = "chat_non_stream"
	RequestTypeImageNonStream  RequestType = "image_non_stream"
	RequestTypeImageStream     RequestType = "image_stream"
	RequestTypeOther           RequestType = "embedding_or_other"
)

const requestProfileContextKey = "gateway_request_profile"

type PromptSizeBucket string

const (
	PromptSizeSmall     PromptSizeBucket = "small"
	PromptSizeMedium    PromptSizeBucket = "medium"
	PromptSizeLarge     PromptSizeBucket = "large"
	PromptSizeVeryLarge PromptSizeBucket = "very_large"
)

type MigrationCapability string

const (
	MigrationUnbound            MigrationCapability = "unbound"
	MigrationCacheAffinity      MigrationCapability = "cache_affinity"
	MigrationUpstreamStateBound MigrationCapability = "upstream_state_bound"
)

// RequestProfile is the immutable routing classification shared by selection,
// health, retry, and audit code. Refinement replaces the complete value.
type RequestProfile struct {
	RequestType          RequestType         `json:"request_type"`
	Protocol             string              `json:"protocol"`
	ModelFamily          string              `json:"model_family"`
	PromptSizeBucket     PromptSizeBucket    `json:"prompt_size_bucket"`
	HasTools             bool                `json:"has_tools"`
	HasConversationState bool                `json:"has_conversation_state"`
	IsStream             bool                `json:"is_stream"`
	MigrationCapability  MigrationCapability `json:"migration_capability"`
}

// RequestProfileHint contains body fields that are cheap to parse before the
// distributor performs the first channel selection.
type RequestProfileHint struct {
	IsStream         bool
	HasTools         bool
	HasCacheAffinity bool
	HasUpstreamState bool
}

func InitializeRequestProfile(c *gin.Context, model, path string, hint RequestProfileHint) RequestProfile {
	profile := RequestProfile{
		Protocol:             protocolFromPath(path),
		ModelFamily:          normalizeModelFamily(model),
		PromptSizeBucket:     PromptSizeSmall,
		HasTools:             hint.HasTools,
		HasConversationState: hint.HasCacheAffinity || hint.HasUpstreamState,
		IsStream:             hint.IsStream,
		MigrationCapability:  migrationCapability(hint.HasCacheAffinity, hint.HasUpstreamState),
	}
	profile.RequestType = classifyRequestType(profile, path, false)
	setRequestProfile(c, profile)
	return profile
}

func RefineRequestProfile(c *gin.Context, format types.RelayFormat, request dto.Request, promptTokens int) RequestProfile {
	profile, found := RequestProfileFromContext(c)
	if !found {
		profile = InitializeRequestProfile(c, "", requestPath(c), RequestProfileHint{})
	}
	profile.Protocol = protocolFromRelayFormat(format)
	profile.PromptSizeBucket = promptSizeBucket(promptTokens)
	profile.IsStream = request != nil && request.IsStream(c)
	profile.HasTools = requestHasTools(request)
	cacheAffinity, upstreamState := requestConversationState(request)
	profile.HasConversationState = cacheAffinity || upstreamState
	profile.MigrationCapability = migrationCapability(cacheAffinity, upstreamState)
	image := format == types.RelayFormatOpenAIImage || IsImageGenerationRequest(c)
	profile.RequestType = classifyRequestType(profile, requestPath(c), image)
	setRequestProfile(c, profile)
	return profile
}

func RequestProfileFromContext(c *gin.Context) (RequestProfile, bool) {
	if c == nil {
		return RequestProfile{}, false
	}
	value, found := c.Get(requestProfileContextKey)
	if !found {
		return RequestProfile{}, false
	}
	profile, ok := value.(RequestProfile)
	return profile, ok
}

func RequestTypeFromContext(c *gin.Context) RequestType {
	profile, found := RequestProfileFromContext(c)
	if !found || !isKnownRequestType(profile.RequestType) {
		return RequestTypeOther
	}
	return profile.RequestType
}

func setRequestProfile(c *gin.Context, profile RequestProfile) {
	if c != nil {
		c.Set(requestProfileContextKey, profile)
		AttachRouteDecisionProfile(c, profile)
	}
}

func classifyRequestType(profile RequestProfile, path string, image bool) RequestType {
	path = strings.ToLower(path)
	if image || strings.Contains(path, "/images/") {
		if profile.IsStream {
			return RequestTypeImageStream
		}
		return RequestTypeImageNonStream
	}
	if strings.Contains(path, "embedding") || strings.Contains(path, "rerank") || strings.Contains(path, "audio") || strings.Contains(path, "realtime") {
		return RequestTypeOther
	}
	if !profile.IsStream {
		return RequestTypeChatNonStream
	}
	if profile.HasTools {
		return RequestTypeToolCallStream
	}
	if profile.HasConversationState || profile.PromptSizeBucket == PromptSizeLarge || profile.PromptSizeBucket == PromptSizeVeryLarge {
		return RequestTypeChatLongStream
	}
	return RequestTypeChatShortStream
}

func protocolFromPath(path string) string {
	path = strings.ToLower(path)
	switch {
	case strings.Contains(path, "/responses"):
		return string(types.RelayFormatOpenAIResponses)
	case strings.Contains(path, "/messages"):
		return string(types.RelayFormatClaude)
	case strings.Contains(path, "gemini"):
		return string(types.RelayFormatGemini)
	case strings.Contains(path, "/images/"):
		return string(types.RelayFormatOpenAIImage)
	case strings.Contains(path, "embedding"):
		return string(types.RelayFormatEmbedding)
	case strings.Contains(path, "rerank"):
		return string(types.RelayFormatRerank)
	default:
		return string(types.RelayFormatOpenAI)
	}
}

func protocolFromRelayFormat(format types.RelayFormat) string {
	if format == "" {
		return string(types.RelayFormatOpenAI)
	}
	return string(format)
}

func normalizeModelFamily(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return "unknown"
	}
	return model
}

func promptSizeBucket(tokens int) PromptSizeBucket {
	switch {
	case tokens >= VeryLongContextPromptTokens:
		return PromptSizeVeryLarge
	case tokens >= LongContextPromptTokenThreshold:
		return PromptSizeLarge
	case tokens >= 32_000:
		return PromptSizeMedium
	default:
		return PromptSizeSmall
	}
}

func migrationCapability(cacheAffinity, upstreamState bool) MigrationCapability {
	if upstreamState {
		return MigrationUpstreamStateBound
	}
	if cacheAffinity {
		return MigrationCacheAffinity
	}
	return MigrationUnbound
}

func requestHasTools(request dto.Request) bool {
	switch value := request.(type) {
	case *dto.GeneralOpenAIRequest:
		return len(value.Tools) > 0 || hasJSONValue(value.Functions)
	case *dto.OpenAIResponsesRequest:
		return hasJSONValue(value.Tools)
	case *dto.ClaudeRequest:
		return len(value.GetTools()) > 0
	case *dto.GeminiChatRequest:
		return len(value.GetTools()) > 0
	default:
		return false
	}
}

func requestConversationState(request dto.Request) (cacheAffinity bool, upstreamState bool) {
	switch value := request.(type) {
	case *dto.OpenAIResponsesRequest:
		return hasJSONValue(value.PromptCacheKey), strings.TrimSpace(value.PreviousResponseID) != "" || hasJSONValue(value.Conversation)
	case *dto.OpenAIResponsesCompactionRequest:
		return false, strings.TrimSpace(value.PreviousResponseID) != ""
	default:
		return false, false
	}
}

func requestPath(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	return c.Request.URL.Path
}

func normalizedRequestType(values ...RequestType) RequestType {
	if len(values) > 0 && isKnownRequestType(values[0]) {
		return values[0]
	}
	return RequestTypeOther
}

func isKnownRequestType(value RequestType) bool {
	switch value {
	case RequestTypeChatShortStream, RequestTypeChatLongStream, RequestTypeToolCallStream,
		RequestTypeChatNonStream, RequestTypeImageNonStream, RequestTypeImageStream, RequestTypeOther:
		return true
	default:
		return false
	}
}
