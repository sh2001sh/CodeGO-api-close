package http

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	securityaudit "github.com/sh2001sh/new-api/internal/gateway/securityaudit"
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
	protocol := string(relayFormat)
	if c != nil && c.Request != nil && c.Request.URL != nil {
		protocol += ":" + strings.ToLower(c.Request.URL.Path)
	}
	decision := service.Check(c.Request.Context(), securityaudit.Request{
		RequestID: c.GetString(constant.RequestIdKey), Group: group, Protocol: protocol,
		Model: model, Body: body, FallbackText: fallbackText, Stage: "http",
	})
	if decision.AllowNextStage {
		return nil
	}
	switch decision.Kind {
	case securityaudit.DecisionBlock:
		return types.NewErrorWithStatusCode(
			errors.New("提示词安全审计拒绝了该请求，请调整输入后重试"),
			types.ErrorCodePromptGuardBlocked,
			http.StatusForbidden,
			types.ErrOptionWithSkipRetry(),
		)
	case securityaudit.DecisionInvalid:
		return types.NewErrorWithStatusCode(
			errors.New("提示词安全审计返回无效结果，请稍后重试"),
			types.ErrorCodePromptGuardInvalid,
			http.StatusServiceUnavailable,
			types.ErrOptionWithSkipRetry(),
		)
	default:
		return types.NewErrorWithStatusCode(
			errors.New("提示词安全审计暂时不可用，请稍后重试"),
			types.ErrorCodePromptGuardUnavailable,
			http.StatusServiceUnavailable,
			types.ErrOptionWithSkipRetry(),
		)
	}
}
