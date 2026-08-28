package app

import (
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/types"
)

type channelTestResult struct {
	context     *gin.Context
	localErr    error
	newAPIError *types.NewAPIError
	report      ChannelTestReport
}

// ChannelTestReport contains the billing and audit result of a user-initiated
// channel test. Automatic channel probes intentionally do not populate it.
type ChannelTestReport struct {
	QuotaCharged  int
	LogCreated    bool
	RequestID     string
	BillingSource string
}
