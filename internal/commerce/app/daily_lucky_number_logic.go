package app

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"

	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	luckysettings "github.com/sh2001sh/new-api/internal/commerce/luckysettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
)

const luckyCardAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func generateLuckyCardCode(luckySuffix string) (string, error) {
	if err := validateLuckyNumber(luckySuffix); err != nil {
		return "", err
	}
	var result strings.Builder
	result.Grow(14)
	result.WriteString("CG-")
	for index := 0; index < 6; index++ {
		value, err := randomInt(len(luckyCardAlphabet))
		if err != nil {
			return "", err
		}
		result.WriteByte(luckyCardAlphabet[value])
	}
	result.WriteByte('-')
	result.WriteString(luckySuffix)
	return result.String(), nil
}

func generateLuckySuffix() (string, error) {
	var result strings.Builder
	result.Grow(4)
	for index := 0; index < 4; index++ {
		value, err := randomInt(10)
		if err != nil {
			return "", err
		}
		result.WriteByte('0' + byte(value))
	}
	return result.String(), nil
}

func generateLuckyNumber() (string, error) {
	value, err := randomInt(10000)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%04d", value), nil
}

func randomInt(max int) (int, error) {
	if max <= 0 {
		return 0, errors.New("random upper bound must be positive")
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, err
	}
	return int(value.Int64()), nil
}

func luckyMembershipTier(plan *commerceschema.SubscriptionPlan) string {
	if plan == nil {
		return commerceschema.SubscriptionMembershipTierNone
	}
	return commercedomain.NormalizeSubscriptionMembershipTier(plan.MembershipTier)
}

func luckyTierMultiplier(tier string, setting luckysettings.Setting) float64 {
	switch commercedomain.NormalizeSubscriptionMembershipTier(tier) {
	case commerceschema.SubscriptionMembershipTierLite:
		return setting.MultiplierLite
	case commerceschema.SubscriptionMembershipTierStandard:
		return setting.MultiplierStandard
	case commerceschema.SubscriptionMembershipTierPro:
		return setting.MultiplierPro
	case commerceschema.SubscriptionMembershipTierUltra:
		return setting.MultiplierUltra
	default:
		return 1
	}
}

func luckyMatchDigits(luckySuffix, winningNumber string) int {
	if len(luckySuffix) != 4 || len(winningNumber) != 4 {
		return 0
	}
	for matched := 4; matched >= 1; matched-- {
		if luckySuffix[4-matched:] == winningNumber[4-matched:] {
			return matched
		}
	}
	return 0
}

func luckyBaseRewardUSD(matched int, setting luckysettings.Setting) float64 {
	switch matched {
	case 1:
		return setting.BaseReward1USD
	case 2:
		return setting.BaseReward2USD
	case 3:
		return setting.BaseReward3USD
	case 4:
		return setting.BaseReward4USD
	default:
		return 0
	}
}

func luckyRewardQuota(rewardUSD float64) int64 {
	if rewardUSD <= 0 {
		return 0
	}
	return int64(math.Round(rewardUSD * platformruntime.QuotaPerUnit))
}

func luckyJackpotAfter(before float64, fullMatchCount int, setting luckysettings.Setting) float64 {
	if fullMatchCount > 0 {
		return setting.JackpotInitialUSD
	}
	after := before + setting.JackpotIncrementUSD
	if after > setting.JackpotCapUSD {
		after = setting.JackpotCapUSD
	}
	return after
}

func validateLuckyNumber(number string) error {
	if len(number) != 4 {
		return errors.New("lucky number must be four digits")
	}
	for _, item := range number {
		if item < '0' || item > '9' {
			return errors.New("lucky number must contain digits only")
		}
	}
	return nil
}
