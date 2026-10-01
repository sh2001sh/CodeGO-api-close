package routing

import (
	"math"
	"strconv"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func officialCostFloat(cost string) float64 {
	value, err := strconv.ParseFloat(cost, 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
		return math.MaxFloat64
	}
	if value <= 0 {
		return math.SmallestNonzeroFloat64
	}
	return value
}

func officialConservativeSuccess(s officialStats) float64 {
	if s.w5.requests <= 0 {
		return 0
	}
	a, b := 19+float64(s.w5.successes), 1+float64(s.w5.requests-s.w5.successes)
	total := a + b
	return math.Max(0, a/total-1.96*math.Sqrt(a*b/(total*total*(total+1)))) * 100
}

func officialReliabilityPenalty(rate float64) float64 {
	switch {
	case rate >= 98:
		return 1
	case rate >= 95:
		return 1.15 + (98-rate)*0.12
	case rate >= 90:
		return 2.5 + (95-rate)*0.3
	default:
		return math.Min(15, 5+(90-rate)*0.2)
	}
}

func officialScore(pool catalog.OfficialPool, cost string, s officialStats, now time.Time) float64 {
	price := officialCostFloat(cost)
	base := price
	if s.w5.requests < 20 {
		base *= 1.10
	}
	rate := officialConservativeSuccess(s)
	if s.w5.requests >= 5 {
		base *= officialReliabilityPenalty(rate)
	}
	if s.failures >= 2 {
		base *= 1.35
	}
	if s.failures > 0 {
		base *= math.Pow(1.25, float64(min(s.failures, 100)))
	}
	mw, tw, cw, sw := pool.MultiplierWeight, pool.TTFTWeight, pool.CacheWeight, pool.SuccessWeight
	if mw+tw+cw+sw == 0 {
		mw, tw, cw, sw = 35, 25, 15, 25
	}
	factor := 1 + (price-1)*float64(mw)/100
	if ttft := s.p95(now); ttft > 0 {
		factor *= 1 + math.Min(4, ttft/500)*float64(tw)/100
	}
	if rate > 0 {
		factor *= 1 + ((100-rate)/100)*float64(sw)/100*2
	}
	if s.cacheRate > 0 {
		factor *= 1 - (s.cacheRate/100)*float64(cw)/100*0.35
	}
	score := base * factor
	if math.IsInf(score, 0) || math.IsNaN(score) {
		return math.MaxFloat64
	}
	return math.Max(0.01, score)
}

func officialReliabilityTier(s officialStats) int {
	if s.failures >= 2 {
		return 2
	}
	if s.w5.requests >= 10 {
		if rate := officialConservativeSuccess(s); rate < 85 {
			return 2
		} else if rate < 90 {
			return 1
		}
	}
	if s.w15.requests >= 30 {
		if rate := float64(s.w15.successes) / float64(s.w15.requests) * 100; rate < 90 {
			return 2
		} else if rate < 94 {
			return 1
		}
	}
	return 0
}
