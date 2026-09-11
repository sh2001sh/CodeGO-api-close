package http

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	securityaudit "github.com/sh2001sh/new-api/internal/gateway/securityaudit"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"github.com/sh2001sh/new-api/types"
)

func recordUpstreamCyberPolicyEvent(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) {
	if c == nil || info == nil || apiErr == nil || apiErr.GetErrorCode() != types.ErrorCodeCyberPolicy {
		return
	}
	body, _, _ := apiErr.RawResponse()
	cyber, _ := platformhttpx.DetectCyberPolicyError(body)
	promptBody := []byte(nil)
	if value, exists := c.Get(string(constant.ContextKeySecurityAuditPromptBody)); exists {
		if bytes, ok := value.([]byte); ok {
			promptBody = append([]byte(nil), bytes...)
		}
	}
	if storage, err := platformhttpx.GetBodyStorage(c); err == nil {
		if len(promptBody) == 0 {
			promptBody, _ = storage.Bytes()
		}
	}
	protocol := string(info.RelayFormat)
	if c.Request != nil && c.Request.URL != nil {
		protocol += ":" + strings.ToLower(c.Request.URL.Path)
	}
	_, err := securityaudit.RecordEvent(c.Request.Context(), securityaudit.EventInput{
		RequestID: c.GetString(constant.RequestIdKey), Source: securityaudit.EventSourceUpstreamCyberPolicy,
		Decision: "blocked", RiskCode: "cyber_policy", Severity: "high",
		UserID: info.UserId, TokenID: info.TokenId, TokenName: c.GetString("token_name"), ChannelID: info.ChannelId,
		MarketplaceGroupID: info.MarketplaceGroupID, OwnerUserID: info.MarketplaceOwnerID,
		Model: info.OriginModelName, Protocol: protocol, HTTPStatus: apiErr.StatusCode,
		UpstreamErrorType: cyber.Type, UpstreamErrorCode: "cyber_policy", UpstreamErrorMessage: defaultCyberMessage(cyber.Message, apiErr.Error()),
		UpstreamErrorBody: body, PromptBody: promptBody, BillingResult: securityaudit.BillingResultNotCharged,
	})
	if err != nil {
		platformobservability.SysError(fmt.Sprintf("record upstream cyber policy event failed: %s", err.Error()))
	}
}

func recordPromptGuardEvent(c *gin.Context, info *relaycommon.RelayInfo, relayFormat types.RelayFormat, body []byte, fallback string, decision securityaudit.Decision) {
	if c == nil || info == nil {
		return
	}
	riskCode := "prompt_guard"
	if decision.Result != nil && len(decision.Result.Categories) > 0 {
		riskCode = strings.Join(decision.Result.Categories, ",")
	}
	protocol := string(relayFormat)
	if c.Request != nil && c.Request.URL != nil {
		protocol += ":" + strings.ToLower(c.Request.URL.Path)
	}
	_, err := securityaudit.RecordEvent(c.Request.Context(), securityaudit.EventInput{
		RequestID: c.GetString(constant.RequestIdKey), Source: securityaudit.EventSourcePromptGuard,
		Decision: string(decision.Kind), RiskCode: riskCode, Severity: "high",
		UserID: info.UserId, TokenID: info.TokenId, TokenName: c.GetString("token_name"), ChannelID: info.ChannelId,
		MarketplaceGroupID: info.MarketplaceGroupID, OwnerUserID: info.MarketplaceOwnerID,
		Model: info.OriginModelName, Protocol: protocol, HTTPStatus: 403,
		UpstreamErrorType: "prompt_guard", UpstreamErrorCode: string(decision.ErrorCode),
		UpstreamErrorMessage: "提示词安全审计拒绝了该请求，请调整输入后重试",
		PromptBody:           body, PromptFallback: fallback, BillingResult: securityaudit.BillingResultNotCharged,
	})
	if err != nil {
		platformobservability.SysError(fmt.Sprintf("record prompt guard event failed: %s", err.Error()))
	}
}

func defaultCyberMessage(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
