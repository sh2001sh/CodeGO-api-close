package http

import (
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayexecutionapp "github.com/sh2001sh/new-api/internal/gateway/execution/app"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/sh2001sh/new-api/types"
)

func shouldRecordAutoGroupFailure(c *gin.Context, err *types.NewAPIError) bool {
	return c != nil && err != nil &&
		!c.GetBool(string(constant.ContextKeyClientGone)) &&
		!gatewayruntime.IsLocalStreamMaxDurationExceeded(c) &&
		gatewayruntime.HasRemainingCrossGroupRoute(c) &&
		gatewayexecutionapp.IsRetryableChannelFailure(err)
}
