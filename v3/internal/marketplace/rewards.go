package marketplace

import (
	"encoding/json"
	"math"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type Reward struct {
	PlanSnapshot     json.RawMessage `json:"plan_snapshot,omitempty"`
	Kind             string          `json:"kind"`
	Title            string          `json:"title"`
	Weight           int64           `json:"weight"`
	Amount           credits.Micro   `json:"amount_micro"`
	Minimum          credits.Micro   `json:"minimum_micro,omitempty"`
	Maximum          credits.Micro   `json:"maximum_micro,omitempty"`
	Step             credits.Micro   `json:"step_micro,omitempty"`
	MultiplierPPM    int64           `json:"multiplier_ppm"`
	DurationSeconds  int64           `json:"duration_seconds"`
	PlanID           int64           `json:"plan_id"`
	PropType         string          `json:"prop_type,omitempty"`
	DiscountRatePPM  int64           `json:"discount_rate_ppm,omitempty"`
	MaxDiscountMicro credits.Micro   `json:"max_discount_micro,omitempty"`
	LegacyRewardType string          `json:"legacy_reward_type,omitempty"`
	RewardTier       string          `json:"reward_tier,omitempty"`
	WalletType       string          `json:"wallet_type,omitempty"`
}

type Pool struct {
	ID             int64          `json:"id"`
	Name           string         `json:"name"`
	Enabled        bool           `json:"enabled"`
	Price          credits.Micro  `json:"price_micro"`
	DailyLimit     int            `json:"daily_limit"`
	MonthlyLimit   int            `json:"monthly_limit"`
	DailyOpenLimit int            `json:"daily_open_limit"`
	Standard       StandardPolicy `json:"standard_policy"`
	Rewards        []Reward       `json:"rewards"`
	Guarantees     Guarantees     `json:"guarantees"`
	Scope          string         `json:"scope"`
}

func validatePool(p Pool) error {
	if p.ID < 0 || p.Name == "" || len(p.Name) > 200 || p.Price <= 0 || p.DailyLimit < 1 || p.DailyLimit > 10000 || p.MonthlyLimit < 0 || p.DailyOpenLimit < 0 || len(p.Rewards) == 0 || len(p.Rewards) > 1000 || (p.Scope != "" && p.Scope != "standard" && p.Scope != "credits") {
		return ErrInvalidInput
	}
	if err := validateStandard(p.Standard); err != nil {
		return err
	}
	if p.Standard.Enabled && p.Scope != "standard" {
		return ErrInvalidInput
	}
	var total int64
	for _, r := range p.Rewards {
		if r.Title == "" || r.Weight <= 0 || total > math.MaxInt64-r.Weight || r.MaxDiscountMicro < 0 {
			return ErrInvalidInput
		}
		total += r.Weight
		switch r.Kind {
		case "credits":
			if r.Amount < 0 || (r.Amount == 0 && (r.Minimum <= 0 || r.Maximum < r.Minimum || r.Step < 0)) {
				return ErrInvalidInput
			}
		case "multiplier":
			if r.MultiplierPPM < 0 || r.MultiplierPPM > 1000000 || r.DurationSeconds <= 0 || r.DurationSeconds > 366*24*3600 {
				return ErrInvalidInput
			}
			// Current admission has no native discount cap reservation. Historical
			// typed cards retain their informational max/used values on import.
			if r.PropType == "" && r.MaxDiscountMicro > 0 {
				return ErrInvalidInput
			}
		case "subscription":
			if r.PlanID <= 0 {
				return ErrInvalidInput
			}
		case "topup_discount", "subscription_discount":
			if r.DiscountRatePPM <= 0 || r.DiscountRatePPM > 1000000 {
				return ErrInvalidInput
			}
		case "extra_draw":
		default:
			return ErrInvalidInput
		}
	}
	return validateGuarantees(p.Guarantees)
}

func chooseReward(rewards []Reward, draw func(int64) (int64, error)) (Reward, error) {
	var total int64
	for _, r := range rewards {
		if r.Weight <= 0 || total > math.MaxInt64-r.Weight {
			return Reward{}, ErrInvalidInput
		}
		total += r.Weight
	}
	if total == 0 {
		return Reward{}, ErrInvalidInput
	}
	n, err := draw(total)
	if err != nil {
		return Reward{}, err
	}
	if n < 0 || n >= total {
		return Reward{}, ErrInvalidInput
	}
	for _, r := range rewards {
		if n < r.Weight {
			if r.Kind == "credits" && r.Amount == 0 {
				step := r.Step
				if step == 0 {
					step = 1
				}
				if r.Minimum <= 0 || r.Maximum < r.Minimum || step < 0 {
					return Reward{}, ErrInvalidInput
				}
				steps := (r.Maximum-r.Minimum)/step + 1
				v, e := draw(int64(steps))
				if e != nil {
					return Reward{}, e
				}
				if v < 0 || v >= int64(steps) {
					return Reward{}, ErrInvalidInput
				}
				r.Amount = r.Minimum + credits.Micro(v)*step
			}
			return r, nil
		}
		n -= r.Weight
	}
	return Reward{}, ErrInvalidInput
}
