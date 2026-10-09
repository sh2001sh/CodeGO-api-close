// Package pricing computes exact charges from immutable catalog prices.
package pricing

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"github.com/sh2001sh/new-api/v3/pkg/exactfactor"
	"github.com/tidwall/gjson"
)

var ErrUnsupportedMode = errors.New("pricing: unsupported price mode")

const perMillion = 1_000_000
const multiplierScale = 1_000_000

// FastModeRule is written only to an admitted private copy of a catalog price.
const FastModeRule = "codego_fast_mode"

// ServiceTierMultiplier uses the frozen admission when the upstream omits its
// tier. An explicit downgrade releases the premium reservation at settlement.
func ServiceTierMultiplier(p catalog.Price, usage gateway.Usage) int64 {
	fast, _ := p.Rules[FastModeRule].(bool)
	if !fast {
		return 1
	}
	switch usage.ServiceTier {
	case "default", "flex", "scale", "standard":
		return 1
	default:
		return 2
	}
}

// RequestInput freezes all request-dependent expression inputs at admission.
// Now must be the original request timestamp when time functions are used.
type RequestInput struct {
	Body    []byte
	Headers map[string]string
	Now     time.Time
}

func Price(usage gateway.Usage, p catalog.Price, multiplier float64) (credits.Micro, error) {
	return PriceForRequest(usage, p, multiplier, RequestInput{})
}

// PriceForRequest uses exact rational arithmetic and rounds once, half up.
// Expression coefficients retain the v2 meaning: credits per million tokens.
// Token and tool prices in the catalog are integer micro-credits.
func PriceForRequest(usage gateway.Usage, p catalog.Price, multiplier float64, request RequestInput) (credits.Micro, error) {
	ppm, err := MultiplierToPPM(multiplier)
	if err != nil {
		return 0, err
	}
	return PriceForRequestPPM(usage, p, ppm, request)
}

// MultiplierToPPM is the single quantization point for legacy float factors.
// Compiled routing factors already use int64 ppm and bypass this conversion.
func MultiplierToPPM(multiplier float64) (int64, error) {
	if multiplier < 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) || multiplier >= float64(math.MaxInt64)/multiplierScale {
		return 0, fmt.Errorf("pricing: invalid multiplier %v", multiplier)
	}
	return int64(math.Round(multiplier * multiplierScale)), nil
}

// PriceForRequestPPM preserves compiled routing multipliers exactly, including
// zero, without passing money or an integer factor through floating point.
func PriceForRequestPPM(usage gateway.Usage, p catalog.Price, multiplierPPM int64, request RequestInput) (credits.Micro, error) {
	return PriceForRequestExactPPM(usage, p, exactfactor.FromInt64(multiplierPPM), request)
}

// PriceForRequestExactPPM accepts an exact scaled PPM decimal. Imported market
// discounts may be smaller than one PPM; only the final money quantum is rounded.
func PriceForRequestExactPPM(usage gateway.Usage, p catalog.Price, multiplierPPM string, request RequestInput) (credits.Micro, error) {
	factor, err := exactfactor.ParsePPM(multiplierPPM)
	if err != nil {
		return 0, fmt.Errorf("pricing: invalid multiplier: %w", err)
	}
	if err := validateUsage(usage); err != nil {
		return 0, err
	}
	if err := validateToolPrices(p.Rules); err != nil {
		return 0, err
	}
	if err := validateMediaPrice(p); err != nil {
		return 0, err
	}
	var amount *big.Rat
	switch p.Mode {
	case "per_token", "":
		amount, err = tokenAmount(usage, p)
	case "per_request":
		amount, err = mediaAmount(usage, p)
	case "expression", "tiered_expr":
		amount, err = expressionAmount(usage, p, request)
	default:
		return 0, fmt.Errorf("%w: %q", ErrUnsupportedMode, p.Mode)
	}
	if err != nil {
		return 0, err
	}
	if amount.Sign() < 0 {
		return 0, errors.New("pricing: negative charge")
	}
	tools, err := toolAmount(usage, p)
	if err != nil {
		return 0, err
	}
	amount.Mul(amount, factor.Quo(factor, big.NewRat(multiplierScale, 1)))
	// v2 tool surcharges are absolute published prices; group discounts apply
	// to the model charge only. Add before rounding to avoid double rounding.
	amount.Add(amount, tools)
	if ServiceTierMultiplier(p, usage) == 2 {
		amount.Mul(amount, big.NewRat(2, 1))
	}
	quantum, err := MoneyQuantum(p)
	if err != nil {
		return 0, err
	}
	return roundChargeQuantum(amount, quantum)
}

func roundCharge(amount *big.Rat) (credits.Micro, error) {
	num := new(big.Int).Mul(amount.Num(), big.NewInt(2))
	num.Add(num, amount.Denom())
	num.Quo(num, new(big.Int).Mul(amount.Denom(), big.NewInt(2)))
	if !num.IsInt64() {
		return 0, credits.ErrOverflow
	}
	return credits.Micro(num.Int64()), nil
}

func tokenAmount(u gateway.Usage, p catalog.Price) (*big.Rat, error) {
	for _, price := range []int64{p.InputPerMTok, p.OutputPerMTok, p.CacheReadPerMTok, p.CacheWritePerMTok, p.PerRequest} {
		if price < 0 {
			return nil, errors.New("pricing: negative catalog price")
		}
	}
	cached := min(u.CachedTokens, u.PromptTokens)
	write := min(u.CacheWriteTokens+u.CacheWrite1hTokens, u.PromptTokens-cached)
	num := new(big.Int)
	addProduct(num, u.PromptTokens-cached-write, p.InputPerMTok)
	addProduct(num, cached, p.CacheReadPerMTok)
	addProduct(num, write, p.CacheWritePerMTok)
	addProduct(num, u.CompletionTokens, p.OutputPerMTok)
	return new(big.Rat).SetFrac(num, big.NewInt(perMillion)), nil
}

func addProduct(acc *big.Int, tokens, price int64) {
	acc.Add(acc, new(big.Int).Mul(big.NewInt(tokens), big.NewInt(price)))
}

func validateUsage(u gateway.Usage) error {
	counts := []int64{u.PromptTokens, u.CompletionTokens, u.CachedTokens, u.CacheWriteTokens, u.CacheWrite1hTokens,
		u.ImageInputTokens, u.ImageOutputTokens, u.AudioInputTokens, u.AudioOutputTokens,
		u.ImageCount, u.AudioDurationMicros, u.AudioCharacters, u.VideoDurationMicros}
	for _, count := range counts {
		if count < 0 {
			return errors.New("pricing: negative token count")
		}
	}
	for _, count := range u.ToolCalls {
		if count < 0 {
			return errors.New("pricing: negative tool call count")
		}
	}
	if u.CacheWriteTokens > math.MaxInt64-u.CacheWrite1hTokens {
		return credits.ErrOverflow
	}
	return nil
}

// tool_prices maps tool names to integer micro-credits per 1000 calls.
func toolAmount(u gateway.Usage, p catalog.Price) (*big.Rat, error) {
	amount := new(big.Rat)
	if len(u.ToolCalls) == 0 {
		return amount, nil
	}
	prices, _ := p.Rules["tool_prices"].(map[string]any)
	for tool, count := range u.ToolCalls {
		if count == 0 {
			continue
		}
		price, ok := toolPrice(prices, tool, p.Model)
		if !ok {
			return nil, fmt.Errorf("pricing: missing tool price for %q", tool)
		}
		value, err := catalogNumber(price)
		if err != nil || value.Sign() < 0 || !value.IsInt() || !value.Num().IsInt64() {
			return nil, fmt.Errorf("pricing: invalid tool price for %q", tool)
		}
		value.Mul(value, new(big.Rat).SetFrac64(count, 1000))
		amount.Add(amount, value)
	}
	return amount, nil
}

func toolPrice(prices map[string]any, tool, model string) (any, bool) {
	best := -1
	var value any
	for key, price := range prices {
		prefix, ok := strings.CutPrefix(key, tool+":")
		if !ok {
			continue
		}
		prefix = strings.TrimSuffix(prefix, "*")
		if strings.HasPrefix(model, prefix) && len(prefix) > best {
			best = len(prefix)
			value = price
		}
	}
	if best >= 0 {
		return value, true
	}
	if value, ok := prices[tool]; ok {
		return value, true
	}
	switch tool {
	case "web_search":
		return int64(10_000_000), true
	case "web_search_preview":
		if strings.HasPrefix(model, "gpt-4o") || strings.HasPrefix(model, "gpt-4.1") {
			return int64(25_000_000), true
		}
		return int64(10_000_000), true
	case "file_search":
		return int64(2_500_000), true
	case "google_search":
		return int64(14_000_000), true
	}
	return nil, false
}

func validateToolPrices(rules map[string]any) error {
	value, exists := rules["tool_prices"]
	if !exists {
		return nil
	}
	prices, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("pricing: tool_prices must be an object")
	}
	for name, price := range prices {
		amount, err := catalogNumber(price)
		if name == "" || err != nil || amount.Sign() < 0 || !amount.IsInt() || !amount.Num().IsInt64() {
			return fmt.Errorf("pricing: invalid tool price for %q", name)
		}
	}
	return nil
}

func catalogNumber(value any) (*big.Rat, error) {
	var s string
	switch n := value.(type) {
	case string:
		s = n
	case int:
		s = strconv.Itoa(n)
	case int64:
		s = strconv.FormatInt(n, 10)
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) >= 1<<53 {
			return nil, fmt.Errorf("pricing: JSON numeric value must fit exact float integer range; use a decimal string")
		}
		s = strconv.FormatFloat(n, 'f', -1, 64)
	default:
		s = fmt.Sprint(value)
	}
	return exactNumber(s)
}

func exactNumber(s string) (*big.Rat, error) {
	if len(s) > 256 {
		return nil, fmt.Errorf("pricing: numeric literal is too long")
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(s[i+1:])
		if err != nil || exponent < -128 || exponent > 128 {
			return nil, fmt.Errorf("pricing: numeric exponent is out of range")
		}
	}
	if n, ok := new(big.Rat).SetString(s); ok {
		if n.Num().BitLen() > 512 || n.Denom().BitLen() > 512 {
			return nil, fmt.Errorf("pricing: numeric literal is too large")
		}
		return n, nil
	}
	return nil, fmt.Errorf("pricing: invalid number %q", s)
}

type EstimateConfig struct{ DefaultMaxOutput int64 }

func EstimateUsage(body []byte, cfg EstimateConfig) gateway.Usage {
	if cfg.DefaultMaxOutput <= 0 {
		cfg.DefaultMaxOutput = 4096
	}
	maxOut := cfg.DefaultMaxOutput
	for _, path := range []string{"max_completion_tokens", "max_tokens", "max_output_tokens", "generationConfig.maxOutputTokens", "generation_config.max_output_tokens"} {
		if v := gjson.GetBytes(body, path); v.Exists() && v.Int() > 0 {
			maxOut = v.Int()
			break
		}
	}
	return gateway.Usage{PromptTokens: int64(len(body)+3) / 4, CompletionTokens: maxOut, Estimated: true}
}
