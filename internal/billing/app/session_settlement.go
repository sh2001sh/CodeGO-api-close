package app

import (
	"errors"
	"fmt"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

// Settle closes the billing reservation at the amount accepted by the ledger.
func (s *BillingSession) Settle(actualQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.settled {
		return nil
	}
	actualQuota = s.scaledSettlementQuota(actualQuota)
	if s.trusted && s.preConsumedQuota == 0 && actualQuota > 0 {
		if err := s.reserveTrustedSettlement(actualQuota); err != nil {
			return err
		}
	}

	originalPreConsumed := s.preConsumedQuota
	chargedQuota, err := s.reserveSettlementIncrease(actualQuota)
	if err != nil {
		return err
	}
	fundingDelta := chargedQuota - s.preConsumedQuota
	tokenDelta := chargedQuota - originalPreConsumed
	if !s.fundingSettled {
		if err := s.funding.Settle(fundingDelta); err != nil {
			return err
		}
		s.fundingSettled = true
	}

	var tokenErr error
	if !s.relayInfo.IsPlayground {
		tokenErr = AdjustTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, tokenDelta)
		if tokenErr != nil {
			platformobservability.SysLog(fmt.Sprintf(
				"error adjusting token quota after funding settled (userId=%d, tokenId=%d, delta=%d): %s",
				s.relayInfo.UserId, s.relayInfo.TokenId, tokenDelta, tokenErr.Error(),
			))
		}
	}

	if s.funding.Source() == BillingSourceSubscription {
		s.relayInfo.SubscriptionPostDelta += int64(fundingDelta)
	}
	s.relayInfo.BillingSettledQuota = chargedQuota
	s.relayInfo.BillingSettled = true
	s.settled = true
	return tokenErr
}

func (s *BillingSession) scaledSettlementQuota(actualQuota int) int {
	if actualQuota < 0 {
		actualQuota = 0
	}
	return s.scaleQuota(actualQuota, s.settlementQuotaScale())
}

func (s *BillingSession) reserveSettlementIncrease(actualQuota int) (int, error) {
	delta := actualQuota - s.preConsumedQuota
	if delta <= 0 {
		return actualQuota, nil
	}
	reservable, ok := s.funding.(ReservableFundingSource)
	if !ok {
		return actualQuota, nil
	}
	if _, subscription := s.funding.(*SubscriptionFunding); subscription && subscriptionFundingHooks.ReserveAdditional == nil {
		return actualQuota, nil
	}
	if err := reservable.ReserveAdditional(int64(delta)); err != nil {
		if !errors.Is(err, billingdomain.ErrInsufficientBalance) {
			return 0, err
		}
		if _, subscription := s.funding.(*SubscriptionFunding); subscription {
			return s.preConsumedQuota, nil
		}
		balanceSource, ok := s.funding.(fundingAvailableBalance)
		if !ok {
			return s.preConsumedQuota, nil
		}
		available, balanceErr := balanceSource.AvailableBalance()
		if balanceErr != nil {
			return 0, balanceErr
		}
		partial := min(int64(delta), available)
		if partial <= 0 {
			return s.preConsumedQuota, nil
		}
		if err := reservable.ReserveAdditional(partial); err != nil {
			if errors.Is(err, billingdomain.ErrInsufficientBalance) {
				return s.preConsumedQuota, nil
			}
			return 0, err
		}
		delta = int(partial)
	}
	s.preConsumedQuota += delta
	s.syncRelayInfo()
	return s.preConsumedQuota, nil
}

func (s *BillingSession) reserveTrustedSettlement(actualQuota int) error {
	if err := s.funding.PreConsume(actualQuota); err != nil {
		return err
	}
	if !s.relayInfo.IsPlayground {
		if err := PreConsumeTokenQuota(s.relayInfo, actualQuota); err != nil {
			if refundErr := s.funding.Refund(); refundErr != nil {
				return errors.Join(err, fmt.Errorf("refund trusted funding reservation: %w", refundErr))
			}
			return err
		}
	}
	s.preConsumedQuota = actualQuota
	s.tokenConsumed = actualQuota
	s.syncRelayInfo()
	return nil
}
