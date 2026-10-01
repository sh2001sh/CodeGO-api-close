package catalog

import (
	"encoding/json"
	"fmt"
	"math/big"
)

type marketDecimalPrice struct {
	Mode         string       `json:"billing_mode"`
	PerCall      json.Number  `json:"price_per_call"`
	Input        json.Number  `json:"input_price_per_million"`
	Output       json.Number  `json:"output_price_per_million"`
	Read         *json.Number `json:"cache_read_price_per_million"`
	Write        *json.Number `json:"cache_write_price_per_million"`
	MoneyQuantum int64        `json:"money_quantum"`
}

func marketPriceMicro(value json.Number) (int64, error) {
	if value == "" {
		return 0, nil
	}
	r, ok := new(big.Rat).SetString(string(value))
	if !ok || r.Sign() < 0 {
		return 0, fmt.Errorf("catalog: invalid market price")
	}
	r.Mul(r, big.NewRat(1000000, 1))
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(r.Num(), r.Denom(), rem)
	if new(big.Int).Lsh(rem, 1).Cmp(r.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() {
		return 0, fmt.Errorf("catalog: invalid market price")
	}
	return q.Int64(), nil
}

// marketPriceMode normalizes the billing_mode field and applies the
// money_quantum rule, if any, onto m.
func marketPriceMode(m *Price, p marketDecimalPrice) error {
	if p.MoneyQuantum < 0 || p.MoneyQuantum > 2 {
		return fmt.Errorf("catalog: money_quantum must be 1 or 2")
	}
	if p.MoneyQuantum == 2 {
		m.Rules = map[string]any{"money_quantum": int64(2)}
	}
	switch m.Mode {
	case "", "token":
		m.Mode = "per_token"
	case "per_call":
		m.Mode = "per_request"
	default:
		return fmt.Errorf("catalog: invalid market price")
	}
	return nil
}

// marketCacheReadDefault returns the v2 default cache-read price (input * 0.1,
// rounded) when no explicit cache read price is given.
func marketCacheReadDefault(inputPerMTok int64) int64 {
	return new(big.Int).Quo(new(big.Int).Add(big.NewInt(inputPerMTok), big.NewInt(5)), big.NewInt(10)).Int64()
}

// marketCacheWriteDefault returns the v2 default cache-write price (input *
// 1.25, rounded) when no explicit cache write price is given.
func marketCacheWriteDefault(inputPerMTok int64) (int64, error) {
	n := new(big.Int).Add(new(big.Int).Mul(big.NewInt(inputPerMTok), big.NewInt(5)), big.NewInt(2))
	n.Quo(n, big.NewInt(4))
	if !n.IsInt64() {
		return 0, fmt.Errorf("catalog: invalid market price")
	}
	return n.Int64(), nil
}

// marketPriceAmounts fills in the input/output/per-request/cache amounts for
// m from p, applying the v2 cache price defaults when p.Read/p.Write are nil.
func marketPriceAmounts(m *Price, p marketDecimalPrice) error {
	var err error
	if m.InputPerMTok, err = marketPriceMicro(p.Input); err != nil {
		return err
	}
	if m.OutputPerMTok, err = marketPriceMicro(p.Output); err != nil {
		return err
	}
	if m.PerRequest, err = marketPriceMicro(p.PerCall); err != nil {
		return err
	}
	if p.Read != nil {
		if m.CacheReadPerMTok, err = marketPriceMicro(*p.Read); err != nil {
			return err
		}
	} else {
		m.CacheReadPerMTok = marketCacheReadDefault(m.InputPerMTok)
	}
	if p.Write != nil {
		if m.CacheWritePerMTok, err = marketPriceMicro(*p.Write); err != nil {
			return err
		}
	} else {
		if m.CacheWritePerMTok, err = marketCacheWriteDefault(m.InputPerMTok); err != nil {
			return err
		}
	}
	return nil
}

// ParseMarketPrices converts owner-facing credits prices to exact micro units.
// Missing cache prices retain the v2 defaults of input * 0.1 and * 1.25.
func ParseMarketPrices(raw json.RawMessage) (map[string]Price, error) {
	var data map[string]marketDecimalPrice
	if json.Unmarshal(raw, &data) != nil || data == nil {
		return nil, fmt.Errorf("catalog: invalid market price")
	}
	out := map[string]Price{}
	for model, p := range data {
		if model == "" || len(model) > 255 {
			return nil, fmt.Errorf("catalog: invalid market price")
		}
		m := Price{Model: model, Mode: p.Mode}
		if err := marketPriceMode(&m, p); err != nil {
			return nil, err
		}
		if err := marketPriceAmounts(&m, p); err != nil {
			return nil, err
		}
		if m.Mode == "per_request" && m.PerRequest <= 0 {
			return nil, fmt.Errorf("catalog: invalid market price")
		}
		out[model] = m
	}
	return out, nil
}
