package app

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/sh2001sh/new-api/types"
)

type BillingSession struct {
	relayInfo             *relaycommon.RelayInfo
	funding               FundingSource
	preConsumedQuota      int
	tokenConsumed         int
	trusted               bool
	quotaScale            float64
	reservationQuotaScale float64
	monthlyPass           *MonthlyPassEntitlement
	fundingSettled        bool
	settled               bool
	refunded              bool
	mu                    sync.Mutex
}

func (s *BillingSession) Refund(c *gin.Context) {
	s.mu.Lock()
	if s.settled || s.refunded || !s.needsRefundLocked() {
		s.mu.Unlock()
		return
	}
	s.refunded = true
	s.mu.Unlock()

	logger.LogInfo(c, fmt.Sprintf(
		"user %d request failed, refund pre-consume (token_quota=%s, funding=%s)",
		s.relayInfo.UserId,
		logger.FormatQuota(s.tokenConsumed),
		s.funding.Source(),
	))

	tokenID := s.relayInfo.TokenId
	tokenKey := s.relayInfo.TokenKey
	isPlayground := s.relayInfo.IsPlayground
	tokenConsumed := s.tokenConsumed
	funding := s.funding

	gopool.Go(func() {
		if err := funding.Refund(); err != nil {
			platformobservability.SysLog("error refunding billing source: " + err.Error())
		}

		if tokenConsumed > 0 && !isPlayground {
			if err := AdjustTokenQuota(tokenID, tokenKey, -tokenConsumed); err != nil {
				platformobservability.SysLog("error refunding token quota: " + err.Error())
			}
		}
	})
}

func (s *BillingSession) RefundSync(c *gin.Context) error {
	s.mu.Lock()
	if s.settled || s.refunded || !s.needsRefundLocked() {
		s.mu.Unlock()
		return nil
	}
	s.refunded = true
	s.mu.Unlock()

	logger.LogInfo(c, fmt.Sprintf(
		"user %d request failed, refund pre-consume (token_quota=%s, funding=%s)",
		s.relayInfo.UserId,
		logger.FormatQuota(s.tokenConsumed),
		s.funding.Source(),
	))

	tokenID := s.relayInfo.TokenId
	tokenKey := s.relayInfo.TokenKey
	isPlayground := s.relayInfo.IsPlayground
	tokenConsumed := s.tokenConsumed
	funding := s.funding

	var errs []error
	if err := funding.Refund(); err != nil {
		errs = append(errs, fmt.Errorf("refund funding source: %w", err))
	}

	if tokenConsumed > 0 && !isPlayground {
		if err := AdjustTokenQuota(tokenID, tokenKey, -tokenConsumed); err != nil {
			errs = append(errs, fmt.Errorf("refund token quota: %w", err))
		}
	}

	return errors.Join(errs...)
}

func (s *BillingSession) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.needsRefundLocked()
}

func (s *BillingSession) needsRefundLocked() bool {
	if s.settled || s.refunded || s.fundingSettled {
		return false
	}
	if s.tokenConsumed > 0 {
		return true
	}
	if sub, ok := s.funding.(*SubscriptionFunding); ok && sub.preConsumed > 0 {
		return true
	}
	return false
}

func (s *BillingSession) GetPreConsumedQuota() int {
	return s.preConsumedQuota
}

func (s *BillingSession) Reserve(targetQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if targetQuota < 0 {
		targetQuota = 0
	}
	targetQuota = s.scaleQuota(targetQuota, s.reservationScale())
	if s.settled || s.refunded || s.trusted || targetQuota <= s.preConsumedQuota {
		return nil
	}

	delta := targetQuota - s.preConsumedQuota
	if delta <= 0 {
		return nil
	}

	if err := s.reserveFunding(delta); err != nil {
		return err
	}
	if err := s.reserveToken(delta); err != nil {
		s.rollbackFundingReserve(delta)
		return err
	}

	s.preConsumedQuota += delta
	s.tokenConsumed += delta
	s.syncRelayInfo()
	return nil
}

func (s *BillingSession) preConsume(c *gin.Context, quota int) *types.NewAPIError {
	effectiveQuota := s.scaleQuota(quota, s.reservationScale())

	if s.shouldTrust(c) {
		s.trusted = true
		effectiveQuota = 0
		logger.LogInfo(c, fmt.Sprintf(
			"user %d has enough trusted quota, skipping pre-consume (funding=%s)",
			s.relayInfo.UserId,
			s.funding.Source(),
		))
	} else if effectiveQuota > 0 {
		logger.LogInfo(c, fmt.Sprintf(
			"user %d pre-consume %s (funding=%s)",
			s.relayInfo.UserId,
			logger.FormatQuota(effectiveQuota),
			s.funding.Source(),
		))
	}

	tokenErr, fundingErr := s.preConsumeSources(effectiveQuota)
	if tokenErr == nil && effectiveQuota > 0 {
		s.tokenConsumed = effectiveQuota
	}
	if tokenErr != nil {
		// Funding may have completed concurrently. Release that reservation before
		// returning so both sources remain balanced on a failed request.
		if fundingErr == nil {
			if rollbackErr := s.funding.Refund(); rollbackErr != nil {
				platformobservability.SysLog(fmt.Sprintf("error rolling back funding after token pre-consume failure: %s", rollbackErr.Error()))
			}
		}
		return types.NewErrorWithStatusCode(
			tokenErr,
			types.ErrorCodePreConsumeTokenQuotaFailed,
			http.StatusForbidden,
			types.ErrOptionWithSkipRetry(),
			types.ErrOptionWithNoRecordErrorLog(),
		)
	}

	if fundingErr != nil {
		if s.tokenConsumed > 0 && !s.relayInfo.IsPlayground {
			if rollbackErr := AdjustTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, -s.tokenConsumed); rollbackErr != nil {
				platformobservability.SysLog(fmt.Sprintf(
					"error rolling back token quota (userId=%d, tokenId=%d, amount=%d, fundingErr=%s): %s",
					s.relayInfo.UserId,
					s.relayInfo.TokenId,
					s.tokenConsumed,
					fundingErr.Error(),
					rollbackErr.Error(),
				))
			}
			s.tokenConsumed = 0
		}

		if insufficientErr := newFundingInsufficientError(s.funding.Source(), fundingErr); insufficientErr != nil {
			return insufficientErr
		}

		errMsg := fundingErr.Error()
		if strings.Contains(errMsg, "no active subscription") || strings.Contains(errMsg, "subscription quota insufficient") {
			return types.NewErrorWithStatusCode(
				fmt.Errorf("subscription quota insufficient or unavailable: %s", errMsg),
				types.ErrorCodeInsufficientUserQuota,
				http.StatusForbidden,
				types.ErrOptionWithSkipRetry(),
				types.ErrOptionWithNoRecordErrorLog(),
			)
		}

		return types.NewError(fundingErr, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}

	s.preConsumedQuota = effectiveQuota
	s.syncRelayInfo()
	return nil
}

// preConsumeSources performs independent token and funding reservations in
// parallel when the database can service separate transactions concurrently.
// SQLite deployments use a single writer lock, so keeping them serial avoids
// introducing contention without changing production behavior.
func (s *BillingSession) preConsumeSources(amount int) (tokenErr, fundingErr error) {
	if amount <= 0 || platformdb.UsingSQLite {
		tokenErr = PreConsumeTokenQuota(s.relayInfo, amount)
		if tokenErr == nil {
			fundingErr = s.funding.PreConsume(amount)
		}
		return tokenErr, fundingErr
	}
	fundingDone := make(chan error, 1)
	go func() {
		fundingDone <- s.funding.PreConsume(amount)
	}()
	tokenErr = PreConsumeTokenQuota(s.relayInfo, amount)
	fundingErr = <-fundingDone
	return tokenErr, fundingErr
}

func (s *BillingSession) scaleQuota(quota int, scale float64) int {
	if quota <= 0 || scale <= 0 || scale == 1 {
		return quota
	}
	scaled := int(math.Round(float64(quota) * scale))
	if scaled < 1 {
		return 1
	}
	return scaled
}

func (s *BillingSession) reserveToken(delta int) error {
	if delta <= 0 || s.relayInfo.IsPlayground {
		return nil
	}
	if err := PreConsumeTokenQuota(s.relayInfo, delta); err != nil {
		return types.NewErrorWithStatusCode(
			err,
			types.ErrorCodePreConsumeTokenQuotaFailed,
			http.StatusForbidden,
			types.ErrOptionWithSkipRetry(),
			types.ErrOptionWithNoRecordErrorLog(),
		)
	}
	return nil
}

func (s *BillingSession) shouldTrust(c *gin.Context) bool {
	if s.relayInfo.ForcePreConsume {
		return false
	}

	trustQuota := platformruntime.GetTrustQuota()
	if trustQuota <= 0 {
		return false
	}

	tokenTrusted := s.relayInfo.TokenUnlimited
	if !tokenTrusted {
		tokenQuota := c.GetInt("token_quota")
		tokenTrusted = tokenQuota > trustQuota
	}
	if !tokenTrusted {
		return false
	}

	switch s.funding.Source() {
	case BillingSourceWallet:
		return false
	case BillingSourceSubscription:
		return false
	default:
		return false
	}
}

func (s *BillingSession) syncRelayInfo() {
	info := s.relayInfo
	info.FinalPreConsumedQuota = s.preConsumedQuota
	info.BillingSource = s.funding.Source()

	if sub, ok := s.funding.(*SubscriptionFunding); ok {
		info.SubscriptionId = sub.subscriptionID
		info.SubscriptionPreConsumed = sub.preConsumed
		info.SubscriptionPostDelta = 0
		info.SubscriptionAmountTotal = sub.AmountTotal
		info.SubscriptionAmountUsedAfterPreConsume = sub.AmountUsedAfter
		info.SubscriptionPlanId = sub.PlanId
		info.SubscriptionPlanTitle = sub.PlanTitle
	} else {
		info.SubscriptionId = 0
		info.SubscriptionPreConsumed = 0
		info.SubscriptionPostDelta = 0
		info.SubscriptionAmountTotal = 0
		info.SubscriptionAmountUsedAfterPreConsume = 0
		info.SubscriptionPlanId = 0
		info.SubscriptionPlanTitle = ""
	}
	info.BlindBoxRequestId = ""
}
