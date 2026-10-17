package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	"github.com/sh2001sh/new-api/types"
)

var ErrCreditPackChannelUnsupported = errors.New("channel does not accept credit packs")

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
		err := funding.ReserveAdditional(int64(delta))
		if insufficientErr := newFundingInsufficientError(s.funding.Source(), err); insufficientErr != nil {
			return insufficientErr
		}
		return err
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
	fundingSourceOrder := commercedomain.NormalizeFundingSourceOrder(
		relayInfo.UserSetting.FundingSourceOrder,
		pref,
	)
	tryWallet := func() (*BillingSession, *types.NewAPIError) {
		return newWalletBillingSession(c, relayInfo, preConsumedQuota)
	}

	if relayInfo.MarketplaceCreditPolicy == marketplacedomain.CreditPolicyUniversalOnly {
		// This market policy has always allowed the ordinary wallet even for
		// legacy subscription-only preferences. Keep that fallback intact.
		universalOrder := append([]string(nil), fundingSourceOrder...)
		walletIncluded := false
		for _, source := range universalOrder {
			walletIncluded = walletIncluded || source == BillingSourceWallet
		}
		if !walletIncluded {
			universalOrder = append(universalOrder, BillingSourceWallet)
		}
		tryPack := func() (*BillingSession, *types.NewAPIError) {
			return newCreditPackBillingSession(c, relayInfo, preConsumedQuota)
		}
		session, apiErr := selectBillingSessionByFundingOrder(universalOrder, tryPack, tryWallet)
		if apiErr != nil && apiErr.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
			apiErr = types.NewErrorWithStatusCode(
				fmt.Errorf("通用额度不足，市场渠道不会回退到月卡"),
				types.ErrorCodeInsufficientUserQuota,
				http.StatusForbidden,
				types.ErrOptionWithSkipRetry(),
				types.ErrOptionWithNoRecordErrorLog(),
			)
		}
		return creditPackChannelSelectionResult(relayInfo, fundingSourceOrder, session, apiErr)
	}

	trySubscription := func() (*BillingSession, *types.NewAPIError) {
		return newSubscriptionBillingSession(c, relayInfo, preConsumedQuota)
	}

	session, apiErr := selectBillingSessionByFundingOrder(fundingSourceOrder, trySubscription, tryWallet)
	return creditPackChannelSelectionResult(relayInfo, fundingSourceOrder, session, apiErr)
}

func creditPackChannelSelectionResult(info *relaycommon.RelayInfo, order []string, session *BillingSession, apiErr *types.NewAPIError) (*BillingSession, *types.NewAPIError) {
	insufficient := apiErr != nil && (apiErr.GetErrorCode() == types.ErrorCodeInsufficientUserQuota ||
		(apiErr.GetErrorCode() == types.ErrorCodePreConsumeTokenQuotaFailed && errors.Is(apiErr, identitystore.ErrTokenQuotaInsufficient)))
	if !insufficient ||
		info.MarketplaceGroupID == "" || creditPackChannelAllowed(info) || subscriptionFundingHooks.PreConsumeCreditPack == nil {
		return session, apiErr
	}
	for _, source := range order {
		if source == BillingSourceSubscription {
			return nil, types.NewErrorWithStatusCode(errors.Join(ErrCreditPackChannelUnsupported, apiErr), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden)
		}
	}
	return session, apiErr
}

func newWalletBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.NewAPIError) {
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	if err := ctx.Err(); err != nil {
		return nil, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	userQuota, accountID, err := getUserClaudeWalletFundingContext(ctx, relayInfo.UserId)
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
	funding, err := NewLedgerRelayFundingWithAccount(relayInfo.UserId, relayInfo.RequestId, BillingSourceWallet, &userQuota, accountID)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	funding.requestContext = ctx
	session := &BillingSession{relayInfo: relayInfo, funding: funding}
	apiErr := session.preConsume(c, preConsumedQuota)
	if funding.walletQueueWaitObserved {
		relayInfo.FirstByteTrace.AddWalletQueueWaitDuration(funding.walletQueueWaitDuration)
	}
	if apiErr != nil {
		return nil, apiErr
	}
	if err := ctx.Err(); err != nil {
		// The reservation may have committed just before cancellation. Complete
		// its compensating release before abandoning the HTTP billing session.
		return nil, types.NewError(errors.Join(err, session.RefundSync(c)), types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	return session, nil
}

func newSubscriptionBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.NewAPIError) {
	pack, packErr := newCreditPackBillingSession(c, relayInfo, preConsumedQuota)
	canTryLegacy := packErr == nil || packErr.GetErrorCode() == types.ErrorCodeInsufficientUserQuota ||
		(packErr.GetErrorCode() == types.ErrorCodePreConsumeTokenQuotaFailed && errors.Is(packErr, identitystore.ErrTokenQuotaInsufficient))
	if pack != nil || !canTryLegacy {
		return pack, packErr
	}
	legacy, legacyErr := newLegacySubscriptionBillingSession(c, relayInfo, preConsumedQuota)
	if legacy != nil || legacyErr != nil {
		return legacy, legacyErr
	}
	return nil, packErr
}

func creditPackChannelAllowed(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil {
		return false
	}
	if isExternalBillingChannel(info) {
		return info.MarketplaceGroupID != "" && info.ChannelMeta.CreditPackSupported
	}
	return true
}

func newCreditPackBillingSession(c *gin.Context, info *relaycommon.RelayInfo, quota int) (*BillingSession, *types.NewAPIError) {
	if !creditPackChannelAllowed(info) || subscriptionFundingHooks.PreConsumeCreditPack == nil {
		return nil, nil
	}
	// Pack credits carry the normal wallet/group price, with no legacy or card multiplier.
	// Refunding either source must not occupy the other source's idempotency record.
	digest := sha256.Sum256([]byte("credit-pack:" + info.RequestId))
	packRequestID := fmt.Sprintf("cp:%x", digest[:30])
	session := &BillingSession{relayInfo: info, quotaScale: 1, reservationQuotaScale: 1,
		funding: &SubscriptionFunding{requestID: packRequestID, userID: info.UserId,
			modelName: info.OriginModelName, creditPack: true},
	}
	if apiErr := session.preConsume(c, quota); apiErr != nil {
		return nil, apiErr
	}
	info.SubscriptionGroupMultiplier = subscriptionBillingGroupRatio(info)
	info.SubscriptionPackageMultiplier = 1
	info.SubscriptionQuotaScale = 1
	info.SubscriptionGroupRatio = subscriptionBillingGroupRatio(info)
	return session, nil
}

func newLegacySubscriptionBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.NewAPIError) {
	if isExternalBillingChannel(relayInfo) && relayInfo.MarketplaceGroupID == "" {
		return nil, nil
	}
	subscriptionMultiplier, enabled := subscriptionGroupMultiplier(relayInfo)
	if !enabled {
		return nil, nil
	}

	groupRatio := subscriptionBillingGroupRatio(relayInfo)
	baseQuotaScale := subscriptionMultiplier
	if groupRatio > 0 {
		baseQuotaScale /= groupRatio
	}
	var entitlement *MonthlyPassEntitlement
	// One policy switch controls multiplier-card eligibility for every channel.
	cardAllowed := relayInfo.ChannelMeta != nil && relayInfo.ChannelMeta.MultiplierCardUserEnabled
	if cardAllowed {
		var err error
		entitlement, err = getMonthlyPassEntitlement(relayInfo.UserId)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
	}
	packageMultiplier := 1.0
	quotaScale := baseQuotaScale
	if entitlement != nil {
		packageMultiplier = entitlement.Multiplier
		quotaScale *= packageMultiplier
	}

	session := &BillingSession{
		relayInfo:             relayInfo,
		quotaScale:            quotaScale,
		reservationQuotaScale: baseQuotaScale,
		monthlyPass:           entitlement,
		funding: &SubscriptionFunding{
			requestID: relayInfo.RequestId,
			userID:    relayInfo.UserId,
			modelName: relayInfo.OriginModelName,
			amount:    int64(preConsumedQuota),
		},
	}
	if apiErr := session.preConsume(c, preConsumedQuota); apiErr != nil {
		return nil, apiErr
	}
	relayInfo.SubscriptionGroupMultiplier = subscriptionMultiplier
	relayInfo.SubscriptionPackageMultiplier = packageMultiplier
	relayInfo.SubscriptionQuotaScale = quotaScale
	relayInfo.SubscriptionGroupRatio = groupRatio
	return session, nil
}

func subscriptionGroupMultiplier(relayInfo *relaycommon.RelayInfo) (float64, bool) {
	if relayInfo.MarketplaceGroupID != "" {
		multiplier := marketplacedomain.SubscriptionMultiplier(relayInfo.MarketplaceMultiplier)
		return multiplier, multiplier > 0
	}
	policy := gatewaystore.GetSubscriptionGroupPolicy(relayInfo.UsingGroup)
	return policy.Multiplier, policy.Enabled && policy.Multiplier > 0
}

func subscriptionBillingGroupRatio(relayInfo *relaycommon.RelayInfo) float64 {
	groupRatio := relayInfo.PriceData.GroupRatioInfo.GroupRatio
	if groupRatio > 0 {
		return groupRatio
	}
	if specialRatio, ok := gatewaystore.GetGroupGroupRatio(relayInfo.UserGroup, relayInfo.UsingGroup); ok {
		return specialRatio
	}
	return gatewaystore.GetGroupRatio(relayInfo.UsingGroup)
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
