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
	gatewayruntime.RecordAIHubHealthFailure(c, channelError.ChannelId, modelName, err.StatusCode, string(failureClass))
	modelScopedFailure := failureClass == upstreamFailureModelUnavailable || failureClass == upstreamFailureAccountExhausted
	if modelScopedFailure && modelName != "" {
		group := selectedChannelGroup(c)
		alternative, lookupErr := gatewaystore.HasAlternativeEnabledAbility(channelError.ChannelId, group, modelName)
		if lookupErr != nil {
			platformobservability.SysError(fmt.Sprintf("检查通道「%s」（#%d）的模型 %s 备用路由失败：%v", channelError.ChannelName, channelError.ChannelId, modelName, lookupErr))
		} else if alternative {
			cooling := coolModelScopedUpstreamFailure(channelError.ChannelId, modelName, c.GetString(constant.RequestIdKey), err)
			c.Set("model_unavailable_with_alternative", true)
			if cooling {
				platformobservability.SysLog(fmt.Sprintf("通道「%s」（#%d）的模型 %s 上游不可用，已临时冷却该模型路由并切换备用渠道", channelError.ChannelName, channelError.ChannelId, modelName))
			} else {
				platformobservability.SysLog(fmt.Sprintf("通道「%s」（#%d）的模型 %s 第 %d 次连续失败，保留试错空间", channelError.ChannelName, channelError.ChannelId, modelName, channelHealthFailureCount(channelError.ChannelId, modelName)))
			}
		} else {
			platformobservability.SysLog(fmt.Sprintf("通道「%s」（#%d）的模型 %s 是唯一可用路由，保留渠道与模型路由", channelError.ChannelName, channelError.ChannelId, modelName))
		}
	} else if ShouldDisableChannel(err) && channelError.AutoBan {
		gopool.Go(func() {
			DisableChannel(channelError, err.ErrorWithStatusCode())
		})
	}
	// A partial Responses stream must never be retried in the current request:
	// clients may have already received output. It still needs model-level
	// cooling so subsequent requests choose another healthy route.
	if failureClass == upstreamFailureIncompleteStream {
		gatewayruntime.RecordUserIncompleteStreamFailure(c, modelName)
		recordChannelTransientFailure(c, channelError.ChannelId, modelName, err)
		if shouldRecordIncompleteStreamFaultDomainFailure(c, err) {
			gatewayruntime.RecordFaultDomainChannelFailure(c.GetString("channel_fault_domain"), modelName, channelError.ChannelId, c.GetString(constant.RequestIdKey), retryableFailureCooldown(c, err))
		}
		gatewayruntime.InvalidateChannelAffinityForCurrentRequest(c)
	} else if isRetryableChannelFailure(err) && !modelScopedFailure {
		cooldown := retryableFailureCooldown(c, err)
		recordChannelTransientFailure(c, channelError.ChannelId, c.GetString("original_model"), err)
		if shouldRecordFaultDomainFailure(c, err) {
			gatewayruntime.RecordFaultDomainChannelFailure(c.GetString("channel_fault_domain"), c.GetString("original_model"), channelError.ChannelId, c.GetString(constant.RequestIdKey), cooldown)
		}
		gatewayruntime.InvalidateChannelAffinityForCurrentRequest(c)
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
		other["channel_id"] = channelID
		other["channel_name"] = c.GetString("channel_name")
		other["channel_type"] = c.GetInt("channel_type")

		adminInfo := make(map[string]interface{})
		adminInfo["use_channel"] = c.GetStringSlice("use_channel")
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

func recordChannelTransientFailure(c *gin.Context, channelID int, modelName string, err *types.NewAPIError) {
	if err != nil && !gatewayruntime.IsLongContextRequest(c) &&
		(isGatewayFailureStatus(err.StatusCode) || isUpstreamCapacityFailure(err)) {
		gatewayruntime.RecordChannelGatewayFailure(channelID, modelName, err.StatusCode)
		return
	}
	gatewayruntime.RecordChannelRetryableFailureWithCooldown(channelID, modelName, retryableFailureCooldown(c, err))
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

func isProviderWideTransientFailure(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	return isGatewayFailureStatus(err.StatusCode) || isUpstreamCapacityFailure(err)
}

func coolModelScopedUpstreamFailure(channelID int, modelName string, requestID string, err *types.NewAPIError) bool {
	if IsModelUnavailableError(err) {
		return gatewayruntime.RecordChannelModelUnavailable(channelID, modelName, requestID)
	}
	return gatewayruntime.CoolChannelModelForUpstreamFailure(channelID, modelName)
}

func channelHealthFailureCount(channelID int, modelName string) int {
	state, found := gatewayruntime.GetChannelHealth(channelID, modelName)
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
