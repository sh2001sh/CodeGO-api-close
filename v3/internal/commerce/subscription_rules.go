package commerce

import "time"

const planColumns = `id,name,price_minor,currency,credits,period_seconds,enabled,
group_buy_enabled,group_buy_target,group_buy_bonus,group_buy_lifetime_seconds,
period_credits,reset_period,reset_custom_seconds,internal_only,max_purchase_per_user,duration_unit,duration_value,custom_seconds,
group_buy_bonus2_micro,group_buy_bonus3_micro,group_buy_bonus5_micro,plan_type,fuel_enabled,fuel_unit_price_micro,fuel_min_credits,fuel_credit_step,membership_tier,upgrade_group,model_limits`

func scanPlan(row scanner) (Plan, error) {
	var p Plan
	err := row.Scan(&p.ID, &p.Name, &p.PriceMinor, &p.Currency, &p.Credits, &p.PeriodSeconds, &p.Enabled,
		&p.GroupBuyEnabled, &p.GroupBuyTarget, &p.GroupBuyBonus, &p.GroupBuyLifetimeSeconds,
		&p.PeriodCredits, &p.ResetPeriod, &p.ResetCustomSeconds, &p.InternalOnly, &p.MaxPurchasePerUser, &p.DurationUnit, &p.DurationValue, &p.CustomSeconds,
		&p.GroupBuyBonus2, &p.GroupBuyBonus3, &p.GroupBuyBonus5, &p.PlanType, &p.FuelEnabled, &p.FuelUnitPriceMicro, &p.FuelMinCredits, &p.FuelCreditStep, &p.MembershipTier, &p.UpgradeGroup, &p.ModelLimits)
	return p, err
}

func durationEnd(start time.Time, unit string, value int, custom, fallback int64) time.Time {
	switch unit {
	case "month":
		return start.AddDate(0, value, 0)
	case "year":
		return start.AddDate(value, 0, 0)
	case "day":
		return start.Add(time.Duration(value) * 24 * time.Hour)
	case "hour":
		return start.Add(time.Duration(value) * time.Hour)
	case "custom":
		if custom > 0 {
			return start.Add(time.Duration(custom) * time.Second)
		}
	}
	return start.Add(time.Duration(fallback) * time.Second)
}

func normalizePlanDuration(p *Plan) error {
	if p.DurationUnit == "" {
		p.DurationUnit = "custom"
		p.DurationValue = 1
		p.CustomSeconds = p.PeriodSeconds
	}
	if p.DurationUnit == "custom" && p.CustomSeconds == 0 {
		p.CustomSeconds = p.PeriodSeconds
	}
	var seconds int64
	switch p.DurationUnit {
	case "year":
		if p.DurationValue != 1 {
			return ErrInvalid
		}
		seconds = 31622400
	case "month":
		if p.DurationValue <= 0 || p.DurationValue > 12 {
			return ErrInvalid
		}
		seconds = int64(p.DurationValue) * 30 * 86400
	case "day":
		if p.DurationValue <= 0 || p.DurationValue > 366 {
			return ErrInvalid
		}
		seconds = int64(p.DurationValue) * 86400
	case "hour":
		if p.DurationValue <= 0 || p.DurationValue > 8784 {
			return ErrInvalid
		}
		seconds = int64(p.DurationValue) * 3600
	case "custom":
		seconds = p.CustomSeconds
	default:
		return ErrInvalid
	}
	if seconds < 60 || seconds > 31622400 {
		return ErrInvalid
	}
	p.PeriodSeconds = seconds
	return nil
}

func validReset(period string, seconds int64) bool {
	switch period {
	case "never", "daily", "weekly", "monthly":
		return seconds >= 0
	case "custom":
		return seconds >= 60 && seconds <= 31622400
	default:
		return false
	}
}

// Calendar cycles use the process clock's location, matching existing daily
// midnight and monthly first-day rules. Duration cycles stay anchored at start.
func nextReset(base time.Time, period string, seconds int64, end time.Time) *time.Time {
	var next time.Time
	switch period {
	case "daily":
		next = time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, base.Location()).AddDate(0, 0, 1)
	case "weekly":
		next = base.AddDate(0, 0, 7)
	case "monthly":
		next = time.Date(base.Year(), base.Month(), 1, 0, 0, 0, 0, base.Location()).AddDate(0, 1, 0)
	case "custom":
		if seconds <= 0 {
			return nil
		}
		next = base.Add(time.Duration(seconds) * time.Second)
	default:
		return nil
	}
	if !next.Before(end) {
		return nil
	}
	return &next
}

// Missed cycles advance the clock without granting each missed allowance.
func advanceReset(due time.Time, period string, seconds int64, now, end time.Time) (time.Time, *time.Time) {
	last := due.In(now.Location())
	if period == "custom" && seconds > 0 {
		step := time.Duration(seconds) * time.Second
		last = last.Add(time.Duration(now.Sub(last)/step) * step)
		return last, nextReset(last, period, seconds, end)
	}
	for next := nextReset(last, period, seconds, end); next != nil; next = nextReset(last, period, seconds, end) {
		if next.After(now) {
			return last, next
		}
		last = *next
	}
	return last, nil
}
