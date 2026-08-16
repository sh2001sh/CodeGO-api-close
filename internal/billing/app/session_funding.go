package app

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
)

func isExternalBillingChannel(relayInfo *relaycommon.RelayInfo) bool {
	return relayInfo != nil && relayInfo.ChannelMeta != nil &&
		strings.EqualFold(strings.TrimSpace(relayInfo.ChannelMeta.ChannelScope), gatewayschema.ChannelScopeExternal)
}

func newFundingInsufficientError(source string, err error) *types.NewAPIError {
	var message string
	switch {
	case errors.Is(err, commercedomain.ErrBlindBoxInsufficientQuota):
		message = "blind box quota insufficient"
	case errors.Is(err, billingdomain.ErrInsufficientBalance):
		if source == BillingSourceSubscription {
			message = "subscription quota insufficient or unavailable"
		} else {
			message = "universal quota insufficient"
		}
	default:
		return nil
	}

	return types.NewErrorWithStatusCode(
		fmt.Errorf("%s: %s", message, err.Error()),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusForbidden,
		types.ErrOptionWithSkipRetry(),
		types.ErrOptionWithNoRecordErrorLog(),
	)
}

func (s *BillingSession) reserveFunding(delta int) error {
	if funding, ok := s.funding.(ReservableFundingSource); ok {
		return funding.ReserveAdditional(int64(delta))
	}
	if _, ok := s.funding.(*SubscriptionFunding); ok {
		return nil
	}
	return types.NewError(
		fmt.Errorf("unsupported funding source: %s", s.funding.Source()),
		types.ErrorCodeUpdateDataError,
		types.ErrOptionWithSkipRetry(),
	)
}

func (s *BillingSession) rollbackFundingReserve(delta int) {
	if _, ok := s.funding.(ReservableFundingSource); ok {
		return
	}
	_ = delta
}

// NewBillingSession selects and reserves the request's permitted funding source.
func NewBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.NewAPIError) {
	if relayInfo == nil {
		return nil, types.NewError(
			fmt.Errorf("relayInfo is nil"),
			types.ErrorCodeInvalidRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	pref := commercedomain.NormalizeBillingPreference(relayInfo.UserSetting.BillingPreference)
	monthlyPassMultiplierActive := httpctx.GetContextKeyBool(c, constant.ContextKeyMonthlyPassActive)
	fundingSourceOrder := commercedomain.NormalizeFundingSourceOrder(
		relayInfo.UserSetting.FundingSourceOrder,
		pref,
	)
	if monthlyPassMultiplierActive {
		fundingSourceOrder = []string{BillingSourceSubscription}
	}

	tryWallet := func() (*BillingSession, *types.NewAPIError) {
		userQuota, err := GetUserClaudeWalletQuota(relayInfo.UserId)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if userQuota <= 0 || userQuota-preConsumedQuota < 0 {
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("通用额度不足, 当前余额: %s, 本次所需: %s", logger.FormatQuota(userQuota), logger.FormatQuota(preConsumedQuota)),
				types.ErrorCodeInsufficientUserQuota,
				http.StatusForbidden,
				types.ErrOptionWithSkipRetry(),
				types.ErrOptionWithNoRecordErrorLog(),
			)
		}
		relayInfo.UserQuota = userQuota
		funding, err := NewLedgerRelayFundingWithInitialBalance(relayInfo.UserId, relayInfo.RequestId, BillingSourceWallet, &userQuota)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		session := &BillingSession{relayInfo: relayInfo, funding: funding}
		if apiErr := session.preConsume(c, preConsumedQuota); apiErr != nil {
			return nil, apiErr
		}
		return session, nil
	}

	if relayInfo.MarketplaceCreditPolicy == marketplacedomain.CreditPolicyUniversalOnly {
		session, apiErr := tryWallet()
		if apiErr != nil && apiErr.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("通用额度不足，市场渠道不会回退到月卡"),
				types.ErrorCodeInsufficientUserQuota,
				http.StatusForbidden,
				types.ErrOptionWithSkipRetry(),
				types.ErrOptionWithNoRecordErrorLog(),
			)
		}
		return session, apiErr
	}

	trySubscription := func() (*BillingSession, *types.NewAPIError) {
		if isExternalBillingChannel(relayInfo) {
			return nil, nil
		}
		policy := gatewaystore.GetSubscriptionGroupPolicy(relayInfo.UsingGroup)
		if !policy.Enabled {
			return nil, nil
		}

		groupRatio := relayInfo.PriceData.GroupRatioInfo.GroupRatio
		if groupRatio <= 0 {
			if specialRatio, ok := gatewaystore.GetGroupGroupRatio(relayInfo.UserGroup, relayInfo.UsingGroup); ok {
				groupRatio = specialRatio
			} else {
				groupRatio = gatewaystore.GetGroupRatio(relayInfo.UsingGroup)
			}
		}
		quotaScale := 1.0
		effectiveSubscriptionMultiplier := groupRatio
		if !monthlyPassMultiplierActive {
			quotaScale = policy.Multiplier
			effectiveSubscriptionMultiplier = policy.Multiplier
		}
		if !monthlyPassMultiplierActive && groupRatio > 0 {
			quotaScale /= groupRatio
		}

		session := &BillingSession{
			relayInfo:  relayInfo,
			quotaScale: quotaScale,
			funding: &SubscriptionFunding{
				requestID: relayInfo.RequestId,
				userID:    relayInfo.UserId,
				modelName: relayInfo.OriginModelName,
				amount:    int64(preConsumedQuota),
			},
		}
		relayInfo.SubscriptionGroupMultiplier = effectiveSubscriptionMultiplier
		relayInfo.SubscriptionQuotaScale = quotaScale
		relayInfo.SubscriptionGroupRatio = groupRatio
		if apiErr := session.preConsume(c, preConsumedQuota); apiErr != nil {
			return nil, apiErr
		}
		return session, nil
	}

	return selectBillingSessionByFundingOrder(fundingSourceOrder, trySubscription, tryWallet)
}

type billingSessionFactory func() (*BillingSession, *types.NewAPIError)

func selectBillingSessionByFundingOrder(
	fundingSourceOrder []string,
	trySubscription billingSessionFactory,
	tryWallet billingSessionFactory,
) (*BillingSession, *types.NewAPIError) {
	var lastInsufficientErr *types.NewAPIError
	insufficientSources := make(map[string]struct{}, len(fundingSourceOrder))
	for _, source := range fundingSourceOrder {
		var factory billingSessionFactory
		switch source {
		case BillingSourceSubscription:
			factory = trySubscription
		case BillingSourceWallet:
			factory = tryWallet
		default:
			continue
		}

		session, apiErr := factory()
		if apiErr != nil {
			if apiErr.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
				lastInsufficientErr = apiErr
				insufficientSources[source] = struct{}{}
				continue
			}
			return nil, apiErr
		}
		if session != nil {
			return session, nil
		}
	}

	return unavailableFundingError(lastInsufficientErr, insufficientSources)
}

func unavailableFundingError(lastInsufficientErr *types.NewAPIError, insufficientSources map[string]struct{}) (*BillingSession, *types.NewAPIError) {
	if lastInsufficientErr != nil {
		_, subscriptionInsufficient := insufficientSources[BillingSourceSubscription]
		_, walletInsufficient := insufficientSources[BillingSourceWallet]
		if subscriptionInsufficient && walletInsufficient {
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("月卡额度不足，且通用额度不足"),
				types.ErrorCodeInsufficientUserQuota,
				http.StatusForbidden,
				types.ErrOptionWithSkipRetry(),
				types.ErrOptionWithNoRecordErrorLog(),
			)
		}
		return nil, lastInsufficientErr
	}
	return nil, types.NewErrorWithStatusCode(
		fmt.Errorf("no available funding source"),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusForbidden,
		types.ErrOptionWithSkipRetry(),
		types.ErrOptionWithNoRecordErrorLog(),
	)
}
