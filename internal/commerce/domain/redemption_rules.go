package domain

import (
	"errors"
	"strings"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
)

var (
	ErrRedemptionInvalid = errors.New("redemption.invalid")
	ErrRedemptionUsed    = errors.New("redemption.used")
	ErrRedemptionExpired = errors.New("redemption.expired")
	ErrRedemptionBusy    = errors.New("redemption.busy")
	ErrRedeemFailed      = ErrRedemptionBusy
)

func NormalizeRedemptionType(value string) string {
	switch strings.TrimSpace(value) {
	case commerceschema.RedemptionTypeSubscription:
		return commerceschema.RedemptionTypeSubscription
	case commerceschema.RedemptionTypeBlindBox:
		return commerceschema.RedemptionTypeBlindBox
	default:
		return commerceschema.RedemptionTypeQuota
	}
}
