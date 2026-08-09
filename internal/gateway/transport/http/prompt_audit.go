package http

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	securityaudit "github.com/sh2001sh/new-api/internal/gateway/securityaudit"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/types"
)

func checkPromptAudit(c *gin.Context, relayFormat types.RelayFormat, request dto.Request, info *relaycommon.RelayInfo) *types.NewAPIError {
	return checkPromptAuditWithService(c, relayFormat, request, info, securityaudit.DefaultService())
}

func checkPromptAuditWithService(
	c *gin.Context,
	relayFormat types.RelayFormat,
	request dto.Request,
	info *relaycommon.RelayInfo,
	service *securityaudit.Service,
) *types.NewAPIError {
	if service == nil || service.Mode() == securityaudit.ModeOff {
		return nil
	}
	model, group := "", ""
	if info != nil {
		model = info.OriginModelName
		group = info.TokenGroup
	}
	// Image generation is non-streaming and commonly has a long first byte;
	// never put it behind synchronous prompt auditing.
	if relayFormat == types.RelayFormatOpenAIImage || gatewaycontract.IsImageGenerationModel(model) {
		return nil
	}
	if relayFormat == types.RelayFormatOpenAIRealtime && service.IsBlockingForGroup(group) {
		return types.NewErrorWithStatusCode(
			errors.New("Realtime 暂不支持阻断式提示词安全审计，请稍后重试"),
			types.ErrorCodePromptGuardUnavailable,
			http.StatusServiceUnavailable,
			types.ErrOptionWithSkipRetry(),
		)
	}
	var body []byte
	if storage, err := platformhttpx.GetBodyStorage(c); err == nil {
		body, _ = storage.Bytes()
	}
	fallbackText := ""
	if request != nil {
		if meta := request.GetTokenCountMeta(); meta != nil {
			fallbackText = meta.CombineText
		}
	}
	if shouldSkipPromptAudit(body, fallbackText) {
		return nil
	}
	protocol := string(relayFormat)
	if c != nil && c.Request != nil && c.Request.URL != nil {
		protocol += ":" + strings.ToLower(c.Request.URL.Path)
	}
	decision := service.Check(c.Request.Context(), securityaudit.Request{
		RequestID: c.GetString(constant.RequestIdKey), Group: group, Protocol: protocol,
		Model: model, Body: body, FallbackText: fallbackText, Stage: "http",
	})
	switch decision.Kind {
	case securityaudit.DecisionBlock:
		return types.NewErrorWithStatusCode(
			errors.New("提示词安全审计拒绝了该请求，请调整输入后重试"),
			types.ErrorCodePromptGuardBlocked,
			http.StatusForbidden,
			types.ErrOptionWithSkipRetry(),
		)
	default:
		// Guard is a secondary control. Fail open on timeout, invalid output,
		// or an unavailable audit endpoint so it cannot create gateway 503s.
		return nil
	}
}

func shouldSkipPromptAudit(body []byte, fallbackText string) bool {
	if len(body) > 0 {
		if matched, _ := identityapp.ShouldReviewPromptWithGuard(string(body)); matched {
			return false
		}
	}
	if fallbackText != "" {
		if matched, _ := identityapp.ShouldReviewPromptWithGuard(fallbackText); matched {
			return false
		}
	}
	return true
}
