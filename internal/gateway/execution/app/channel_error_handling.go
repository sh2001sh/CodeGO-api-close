package app

import (
	"fmt"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"net/http"
	"time"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	gatewaystream "github.com/sh2001sh/new-api/internal/gateway/stream"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformtext "github.com/sh2001sh/new-api/internal/platform/textx"
	"github.com/sh2001sh/new-api/types"
)

// ProcessChannelError applies shared disable/logging behavior for channel failures.
func ProcessChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError) {
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, platformtext.LocalLogPreview(err.Error())))

	modelName := c.GetString("original_model")
	failureClass := classifyUpstreamFailure(err)
	gatewayruntime.FinishRouteDecisionAttempt(c, false, err.StatusCode, string(failureClass), string(gatewaystream.AttemptStageFromContext(c)))
	localMaxDuration := isLocalStreamMaxDuration(c)
	clientGone := c.GetBool(string(constant.ContextKeyClientGone))
	if err.StatusCode == http.StatusTooManyRequests && !clientGone {
		c.Set(string(constant.ContextKeyRateLimitRetry), true)
	}
	modelScopedFailure := failureClass == upstreamFailureModelUnavailable || failureClass == upstreamFailureAccountExhausted
	credentialRejected := failureClass == upstreamFailureCredentialRejected
	preserveOnlyRoute := shouldPreserveOnlyRoute(c, channelError.ChannelId, modelName, modelScopedFailure, credentialRejected, err)
	retryFallbackChannel := shouldRetryCurrentChannelIfNoAlternative(c, err)
	if retryFallbackChannel {
		httpctx.SetContextKey(c, constant.ContextKeyRetryFallbackChannelID, channelError.ChannelId)
	}
	if !localMaxDuration && !clientGone && isRetryableChannelFailure(err) && !modelScopedFailure && !credentialRejected && !preserveOnlyRoute {
		// A retry must leave the complete upstream fault domain. Channel IDs
		// can represent different keys on the same provider host.
		gatewayruntime.ExcludeFaultDomain(c, c.GetString("channel_fault_domain"))
		gatewayruntime.InvalidateChannelAffinityForCurrentRequest(c)
	}
	if modelScopedFailure && modelName != "" && !clientGone {
		group := selectedChannelGroup(c)
		alternative, lookupErr := gatewaystore.HasAlternativeSelectableRoute(channelError.ChannelId, group, modelName)
		if lookupErr != nil {
			platformobservability.SysError(fmt.Sprintf("检查通道「%s」（#%d）的模型 %s 备用路由失败：%v", channelError.ChannelName, channelError.ChannelId, modelName, lookupErr))
		} else if alternative {
			cooling := coolModelScopedUpstreamFailure(channelError.ChannelId, modelName, c.GetString(constant.RequestIdKey), err, gatewayruntime.RequestTypeFromContext(c))
			c.Set("model_unavailable_with_alternative", true)
			// A prompt-cache affinity is valuable only while its selected route is
			// usable. Keeping it after a model-scoped rejection makes Codex return
			// the same 503 without ever reaching its configured fallback routes.
			gatewayruntime.InvalidateChannelAffinityForCurrentRequest(c)
			if cooling {
				platformobservability.SysLog(fmt.Sprintf("通道「%s」（#%d）的模型 %s 上游不可用，已临时冷却该模型路由并切换备用渠道", channelError.ChannelName, channelError.ChannelId, modelName))
			} else {
				platformobservability.SysLog(fmt.Sprintf("通道「%s」（#%d）的模型 %s 第 %d 次连续失败，保留试错空间", channelError.ChannelName, channelError.ChannelId, modelName, channelHealthFailureCount(c, channelError.ChannelId, modelName)))
			}
		} else {
			platformobservability.SysLog(fmt.Sprintf("通道「%s」（#%d）的模型 %s 是唯一可用路由，保留渠道与模型路由", channelError.ChannelName, channelError.ChannelId, modelName))
		}
	} else if !clientGone && !credentialRejected && !preserveOnlyRoute && ShouldDisableChannel(err) && channelError.AutoBan {
		gopool.Go(func() {
			DisableChannel(channelError, err.ErrorWithStatusCode())
		})
	}
	// A partial Responses stream must never be retried in the current request:
	// clients may have already received output. It still needs model-level
	// cooling so subsequent requests choose another healthy route.
	if failureClass == upstreamFailureIncompleteStream && !localMaxDuration && !clientGone && !preserveOnlyRoute {
		// A stream which ended before semantic output is safe to replay, but a
		// retry must leave the failed upstream domain instead of cycling through
		// different keys backed by the same provider path.
		if shouldExcludeFaultDomainForIncompleteStream(c) {
			gatewayruntime.ExcludeFaultDomain(c, c.GetString("channel_fault_domain"))
		}
		gatewayruntime.RecordUserIncompleteStreamFailure(c, modelName)
		recordChannelTransientFailure(c, channelError.ChannelId, modelName, err)
		if shouldRecordIncompleteStreamFaultDomainFailure(c, err) {
			gatewayruntime.RecordFaultDomainChannelFailure(c.GetString("channel_fault_domain"), modelName, channelError.ChannelId, c.GetString(constant.RequestIdKey), retryableFailureCooldown(c, err), gatewayruntime.RequestTypeFromContext(c))
		}
		gatewayruntime.InvalidateChannelAffinityForCurrentRequest(c)
	} else if credentialRejected && !localMaxDuration && !clientGone {
		gatewayruntime.RecordChannelCredentialFailure(channelError.ChannelId)
		gatewayruntime.InvalidateChannelAffinityForCurrentRequest(c)
		platformobservability.SysLog(fmt.Sprintf("通道「%s」（#%d）被上游拒绝访问，已进行凭据级短冷却并切换备用渠道", channelError.ChannelName, channelError.ChannelId))
	} else if !localMaxDuration && !clientGone && isRetryableChannelFailure(err) && !modelScopedFailure && !preserveOnlyRoute {
		cooldown := retryableFailureCooldown(c, err)
		recordChannelTransientFailure(c, channelError.ChannelId, c.GetString("original_model"), err)
		if shouldRecordFaultDomainFailure(c, err) {
			gatewayruntime.RecordFaultDomainChannelFailure(c.GetString("channel_fault_domain"), c.GetString("original_model"), channelError.ChannelId, c.GetString(constant.RequestIdKey), cooldown, gatewayruntime.RequestTypeFromContext(c))
		}
		gatewayruntime.InvalidateChannelAffinityForCurrentRequest(c)
	}

	if retryFallbackChannel {
		platformobservability.SysLog(fmt.Sprintf("通道「%s」（#%d）首包超时，当前分组无备用渠道时允许重试一次", channelError.ChannelName, channelError.ChannelId))
	}
	if preserveOnlyRoute {
		platformobservability.SysLog(fmt.Sprintf("通道「%s」（#%d）的模型 %s 是当前分组唯一可用路由，保留后续探测，不进入冷却", channelError.ChannelName, channelError.ChannelId, modelName))
	}

	if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
		userID := c.GetInt("id")
		tokenName := c.GetString("token_name")
		modelName := c.GetString("original_model")
		tokenID := c.GetInt("token_id")
		userGroup := c.GetString("group")
		channelID := c.GetInt("channel_id")
		other := make(map[string]interface{})
		if c.Request != nil && c.Request.URL != nil {
			other["request_path"] = c.Request.URL.Path
		}
		other["error_type"] = err.GetErrorType()
		other["error_code"] = err.GetErrorCode()
		other["status_code"] = err.StatusCode
		other["upstream_failure_class"] = failureClass
		if localMaxDuration {
			other["local_stream_max_duration"] = true
		}
		if clientGone {
			other["client_disconnected"] = true
		}
		other["channel_id"] = channelID
		other["channel_name"] = c.GetString("channel_name")
		other["channel_type"] = c.GetInt("channel_type")

		adminInfo := make(map[string]interface{})
		adminInfo["use_channel"] = c.GetStringSlice("use_channel")
		adminInfo["attempt_stage"] = gatewaystream.AttemptStageFromContext(c)
		if httpctx.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey) {
			adminInfo["is_multi_key"] = true
			adminInfo["multi_key_index"] = httpctx.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
		}
		gatewayruntime.AppendChannelAffinityAdminInfo(c, adminInfo)
		if decision, ok := gatewayruntime.GetRouteDecision(c); ok {
			adminInfo["route_decision"] = decision
		}
		if circuit, ok := gatewayruntime.UserStreamFailureCircuitAuditFromContext(c); ok {
			adminInfo["user_stream_failure_circuit"] = circuit
		}
		if lifecycle, ok := c.Get("responses_stream_lifecycle"); ok {
			adminInfo["responses_stream_lifecycle"] = lifecycle
		}
		other["admin_info"] = adminInfo

		startTime := httpctx.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		useTimeSeconds := int(time.Since(startTime).Seconds())
		auditapp.RecordErrorLog(
			c,
			userID,
			channelID,
			modelName,
			tokenName,
			err.MaskSensitiveErrorWithStatusCode(),
			tokenID,
			useTimeSeconds,
			httpctx.GetContextKeyBool(c, constant.ContextKeyIsStream),
			userGroup,
			other,
		)
	}
}

func shouldPreserveOnlyRoute(c *gin.Context, channelID int, modelName string, modelScopedFailure bool, credentialRejected bool, err *types.NewAPIError) bool {
	if c == nil || err == nil || channelID <= 0 || modelName == "" || modelScopedFailure || credentialRejected ||
		c.GetBool(string(constant.ContextKeyClientGone)) || !isRetryableChannelFailure(err) {
		return false
	}
	alternative, lookupErr := gatewaystore.HasAlternativeSelectableRoute(channelID, selectedChannelGroup(c), modelName)
	if lookupErr != nil {
		platformobservability.SysError(fmt.Sprintf("检查通道 #%d 的模型 %s 唯一路由状态失败：%v", channelID, modelName, lookupErr))
		return false
	}
	return !alternative
}

const currentChannelRetryMaxElapsed = 3 * time.Second

// shouldRetryCurrentChannelIfNoAlternative enables one fallback only after
// the current group has no healthy alternate. A response-header timeout gets
// one final retry by request policy; other transport failures must be fast.
func shouldRetryCurrentChannelIfNoAlternative(c *gin.Context, err *types.NewAPIError) bool {
	if c == nil || err == nil || c.GetBool(string(constant.ContextKeyClientGone)) {
		return false
	}
	if err.GetErrorCode() != types.ErrorCodeChannelResponseTimeExceeded {
		startTime := httpctx.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() || time.Since(startTime) > currentChannelRetryMaxElapsed {
			return false
		}
		if err.GetErrorCode() != types.ErrorCodeDoRequestFailed && err.StatusCode != http.StatusBadGateway {
			return false
		}
	}
	if len(c.GetStringSlice("use_channel")) != 1 {
		return false
	}
	if httpctx.GetContextKeyBool(c, constant.ContextKeyResponseBodyDelivered) ||
		c.GetBool(string(constant.ContextKeyStreamContentDelivered)) {
		return false
	}
	return gatewaystream.AttemptStageFromContext(c) == gatewaystream.AttemptStageSelected
}

func isLocalStreamMaxDuration(c *gin.Context) bool {
	return gatewayruntime.IsLocalStreamMaxDurationExceeded(c)
}

func recordChannelTransientFailure(c *gin.Context, channelID int, modelName string, err *types.NewAPIError) {
	if err != nil && !gatewayruntime.IsLongContextRequest(c) &&
		(isGatewayFailureStatus(err.StatusCode) || isUpstreamCapacityFailure(err)) {
		gatewayruntime.RecordChannelGatewayFailure(channelID, modelName, err.StatusCode, gatewayruntime.RequestTypeFromContext(c))
		return
	}
	gatewayruntime.RecordChannelRetryableFailureWithCooldown(channelID, modelName, retryableFailureCooldown(c, err), gatewayruntime.RequestTypeFromContext(c))
}

func isGatewayFailureStatus(statusCode int) bool {
	return statusCode == http.StatusBadGateway || statusCode == http.StatusGatewayTimeout || statusCode == 524
}

func retryableFailureCooldown(c *gin.Context, err *types.NewAPIError) time.Duration {
	if gatewayruntime.IsLongContextRequest(c) && err != nil && err.GetErrorCode() == types.ErrorCodeChannelResponseTimeExceeded {
		return gatewayruntime.LongContextTimeoutCooldown()
	}
	if err == nil {
		return gatewayruntime.RetryableFailureCooldown(0)
	}
	return gatewayruntime.RetryableFailureCooldown(err.StatusCode)
}

func shouldRecordFaultDomainFailure(c *gin.Context, err *types.NewAPIError) bool {
	return !gatewayruntime.IsLongContextRequest(c) && isProviderWideTransientFailure(err)
}

func shouldRecordIncompleteStreamFaultDomainFailure(c *gin.Context, err *types.NewAPIError) bool {
	if !isProviderWideTransientFailure(err) {
		return false
	}
	if !gatewayruntime.IsLongContextRequest(c) {
		return true
	}
	// A long prompt can fail for input-specific reasons after generation has
	// started. Before any content is sent, however, repeated disconnects are an
	// upstream-path failure and should protect the shared fault domain.
	return !c.GetBool(string(constant.ContextKeyStreamContentDelivered))
}

func shouldExcludeFaultDomainForIncompleteStream(c *gin.Context) bool {
	return c != nil && !c.GetBool(string(constant.ContextKeyStreamContentDelivered))
}

func isProviderWideTransientFailure(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	return isGatewayFailureStatus(err.StatusCode) || isUpstreamCapacityFailure(err)
}

func coolModelScopedUpstreamFailure(channelID int, modelName string, requestID string, err *types.NewAPIError, requestType gatewayruntime.RequestType) bool {
	if IsModelUnavailableError(err) {
		return gatewayruntime.RecordChannelModelUnavailable(channelID, modelName, requestID, requestType)
	}
	return gatewayruntime.CoolChannelModelForUpstreamFailure(channelID, modelName, requestType)
}

func channelHealthFailureCount(c *gin.Context, channelID int, modelName string) int {
	state, found := gatewayruntime.GetChannelHealth(channelID, modelName, gatewayruntime.RequestTypeFromContext(c))
	if !found {
		return 0
	}
	return state.ConsecutiveRetryableFailures
}

func selectedChannelGroup(c *gin.Context) string {
	group := httpctx.GetContextKeyString(c, constant.ContextKeyAutoGroup)
	if group != "" {
		return group
	}
	return httpctx.GetContextKeyString(c, constant.ContextKeyUsingGroup)
}
