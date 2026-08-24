package app

import (
	"context"
	"fmt"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	marketplacesettlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"github.com/sh2001sh/new-api/types"
)

func PreConsumeRelayBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	if relayInfo != nil && relayInfo.Billing != nil {
		if err := relayInfo.Billing.Reserve(preConsumedQuota); err != nil {
			if apiErr, ok := err.(*types.NewAPIError); ok {
				return apiErr
			}
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		return nil
	}

	session, apiErr := NewBillingSession(c, relayInfo, preConsumedQuota)
	if apiErr != nil {
		return apiErr
	}
	relayInfo.Billing = session
	return nil
}

func RefundRelayBillingSync(c *gin.Context, relayInfo *relaycommon.RelayInfo) error {
	if relayInfo == nil || relayInfo.Billing == nil {
		return nil
	}
	if session, ok := relayInfo.Billing.(*BillingSession); ok {
		if err := session.RefundSync(c); err != nil {
			return err
		}
	} else {
		relayInfo.Billing.Refund(c)
	}
	relayInfo.Billing = nil
	relayInfo.FinalPreConsumedQuota = 0
	relayInfo.BillingSource = ""
	return nil
}

func SettleRelayBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	if relayInfo.Billing != nil {
		preConsumed := relayInfo.Billing.GetPreConsumedQuota()
		delta := actualQuota - preConsumed

		if delta > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后补扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else if delta < 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后返还扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(-delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费与实际消耗一致，无需调整：%s（按次计费）",
				logger.FormatQuota(actualQuota),
			))
		}

		settleErr := relayInfo.Billing.Settle(actualQuota)
		if settleErr != nil && !relayInfo.BillingSettled {
			return settleErr
		}
		settledQuota := BillingQuotaForLog(relayInfo, actualQuota)
		recordSettledUsage(relayInfo, settledQuota)
		if session, ok := relayInfo.Billing.(*BillingSession); ok {
			if funding, ok := session.funding.(settlementProjectionFunding); ok {
				startRequestSettlementProjection(ctx, relayInfo, funding, actualQuota)
			}
		}

		if actualQuota != 0 {
			if relayInfo.BillingSource == BillingSourceSubscription {
				checkAndSendSubscriptionQuotaNotify(relayInfo)
			} else {
				checkAndSendQuotaNotify(relayInfo, actualQuota-preConsumed, preConsumed)
			}
		}
		return settleErr
	}

	quotaDelta := actualQuota - relayInfo.FinalPreConsumedQuota
	if quotaDelta != 0 {
		if err := PostConsumeQuota(relayInfo, quotaDelta, relayInfo.FinalPreConsumedQuota, true); err != nil {
			return err
		}
	}
	RecordUsageStats(relayInfo.UserId, relayChannelID(relayInfo), actualQuota)
	recordBlindBoxUsage(relayInfo.UserId, actualQuota)
	return nil
}

func recordSettledUsage(relayInfo *relaycommon.RelayInfo, settledQuota int) {
	RecordUsageStats(relayInfo.UserId, relayChannelID(relayInfo), settledQuota)
	recordBlindBoxUsage(relayInfo.UserId, settledQuota)
	if err := RecordRequestEconomics(relayInfo, settledQuota); err != nil {
		platformobservability.SysError("record request economics: " + err.Error())
	}
	if relayInfo.MarketplaceGroupID == "" || settledQuota <= 0 {
		return
	}
	if err := marketplacesettlement.Record(marketplaceSettlementParams(relayInfo, settledQuota)); err != nil {
		platformobservability.SysError("record marketplace settlement: " + err.Error())
	}
}

func relayChannelID(relayInfo *relaycommon.RelayInfo) int {
	if relayInfo == nil || relayInfo.ChannelMeta == nil {
		return 0
	}
	return relayInfo.ChannelId
}

func marketplaceSettlementParams(relayInfo *relaycommon.RelayInfo, consumerDebit int) marketplacesettlement.RecordParams {
	settlementGross := consumerDebit
	if relayInfo.BillingSource == BillingSourceSubscription {
		settlementGross = subscriptionSettlementGross(consumerDebit)
	}
	return marketplacesettlement.RecordParams{
		RequestID: relayInfo.RequestId, GroupID: relayInfo.MarketplaceGroupID,
		OwnerUserID: relayInfo.MarketplaceOwnerID, ConsumerUserID: relayInfo.UserId,
		BillingSource:          relayInfo.BillingSource,
		ConsumerDebitAmount:    int64(consumerDebit),
		SettlementGrossAmount:  int64(settlementGross),
		WalletMultiplier:       relayInfo.MarketplaceMultiplier,
		SubscriptionMultiplier: relayInfo.SubscriptionGroupMultiplier,
	}
}

func subscriptionSettlementGross(consumerDebit int) int {
	if consumerDebit <= 0 {
		return 0
	}
	quotient, remainder := consumerDebit/10, consumerDebit%10
	if remainder >= 5 {
		quotient++
	}
	return quotient
}

type settlementProjectionFunding interface {
	AccountID() string
	ReservationID() string
	SettlementID() string
}

func startRequestSettlementProjection(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, funding settlementProjectionFunding, actualQuota int) {
	if relayInfo == nil || funding == nil || funding.SettlementID() == "" {
		return
	}
	params := RequestSettlementWorkflowParams{
		RequestID:       relayInfo.RequestId,
		TraceID:         traceIDFromContext(ctx),
		UserID:          relayInfo.UserId,
		TokenID:         relayInfo.TokenId,
		AccountID:       funding.AccountID(),
		ReservationID:   funding.ReservationID(),
		SettlementID:    funding.SettlementID(),
		UsageEvidenceID: relayInfo.RequestId,
		ReservedAmount:  int64(relayInfo.FinalPreConsumedQuota),
		ActualAmount:    int64(actualQuota),
	}
	gopool.Go(func() {
		if err := StartRequestSettlementWorkflow(context.Background(), params); err != nil {
			platformobservability.SysError("schedule request settlement workflow: " + err.Error())
		}
	})
}

func traceIDFromContext(ctx *gin.Context) string {
	if ctx == nil {
		return ""
	}
	return ctx.GetString(constant.TraceIdKey)
}
