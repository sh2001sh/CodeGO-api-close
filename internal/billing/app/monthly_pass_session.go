package app

import (
	"fmt"

	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

func (s *BillingSession) reservationScale() float64 {
	if s.reservationQuotaScale > 0 {
		return s.reservationQuotaScale
	}
	return s.quotaScale
}

func (s *BillingSession) settlementQuotaScale() float64 {
	if s.monthlyPass == nil {
		return s.quotaScale
	}
	valid := false
	if subscriptionFundingHooks.ValidateMonthlyPassEntitlement != nil {
		var err error
		valid, err = subscriptionFundingHooks.ValidateMonthlyPassEntitlement(s.relayInfo.UserId, *s.monthlyPass)
		if err != nil {
			platformobservability.SysError(fmt.Sprintf(
				"validate monthly pass entitlement: user=%d prop=%d: %v",
				s.relayInfo.UserId,
				s.monthlyPass.PropID,
				err,
			))
		}
	}
	if valid {
		return s.quotaScale
	}
	s.relayInfo.SubscriptionPackageMultiplier = 1
	s.relayInfo.SubscriptionQuotaScale = s.reservationScale()
	return s.reservationScale()
}
