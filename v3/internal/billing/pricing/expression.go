package pricing

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// Expression prices accept rules.expression (rules.expr is the migration
// alias). Coefficients are credits per million tokens, so p * 2.5 produces
// 2.5 micro-credits for one token, before the final rounding.
func expressionAmount(u gateway.Usage, p catalog.Price, request RequestInput) (*big.Rat, error) {
	source, _ := p.Rules["expression"].(string)
	if source == "" {
		source, _ = p.Rules["expr"].(string)
	}
	parts := strings.Split(source, "|||")
	body := strings.TrimSpace(parts[0])
	if strings.HasPrefix(body, "v1:") {
		body = strings.TrimSpace(body[3:])
	}
	if strings.HasPrefix(body, "v") && strings.Contains(body, ":") {
		return nil, fmt.Errorf("pricing: unsupported expression version")
	}
	root, used, err := parseExpression(body)
	if err != nil {
		return nil, err
	}
	env := expressionEnvironment{vars: tokenVariables(u, used), request: request}
	result, err := env.evaluate(root)
	if err != nil {
		return nil, err
	}
	amount, ok := result.(*big.Rat)
	if !ok || amount.Sign() < 0 {
		return nil, fmt.Errorf("pricing: expression must produce a nonnegative number")
	}
	for _, part := range parts[1:] {
		factor, err := requestRule(strings.TrimSpace(part), &env)
		if err != nil {
			return nil, err
		}
		amount.Mul(amount, factor)
	}
	return amount, nil
}

func tokenVariables(u gateway.Usage, used map[string]bool) map[string]*big.Rat {
	input, output := u.PromptTokens, u.CompletionTokens
	dimensions := map[string]int64{
		"cr": u.CachedTokens, "cc": u.CacheWriteTokens, "cc1h": u.CacheWrite1hTokens,
		"img": u.ImageInputTokens, "img_o": u.ImageOutputTokens, "ai": u.AudioInputTokens, "ao": u.AudioOutputTokens,
	}
	for _, name := range []string{"cr", "cc", "cc1h", "img", "ai"} {
		if used[name] {
			input -= min(input, dimensions[name])
		}
	}
	for _, name := range []string{"img_o", "ao"} {
		if used[name] {
			output -= min(output, dimensions[name])
		}
	}
	vars := make(map[string]*big.Rat, len(dimensions)+3)
	for name, count := range dimensions {
		vars[name] = new(big.Rat).SetInt64(count)
	}
	vars["p"] = new(big.Rat).SetInt64(input)
	vars["c"] = new(big.Rat).SetInt64(output)
	vars["len"] = new(big.Rat).SetInt64(u.PromptTokens)
	return vars
}

func requestRule(source string, env *expressionEnvironment) (*big.Rat, error) {
	n, _, err := parseExpression(source)
	if err != nil {
		return nil, err
	}
	if n.op != "binary" || n.text != "*" || n.children[0].op != "call" || n.children[0].text != "when" || len(n.children[0].children) != 1 {
		return nil, fmt.Errorf("pricing: request rule must be when(condition) * multiplier")
	}
	condition, err := env.evaluate(n.children[0].children[0])
	if err != nil {
		return nil, err
	}
	matched, ok := condition.(bool)
	if !ok {
		return nil, fmt.Errorf("pricing: request rule condition must be boolean")
	}
	if !matched {
		return new(big.Rat).SetInt64(1), nil
	}
	factor, err := env.evaluate(n.children[1])
	if err != nil {
		return nil, err
	}
	value, ok := factor.(*big.Rat)
	if !ok || value.Sign() < 0 {
		return nil, fmt.Errorf("pricing: request rule multiplier must be nonnegative")
	}
	return value, nil
}

// Validate checks price configuration before accepting it into a catalog.
// Request-dependent conditions are evaluated later with the real inputs;
// unknown identifiers and malformed expressions are rejected here.
func Validate(p catalog.Price) error {
	if _, err := MoneyQuantum(p); err != nil {
		return err
	}
	if err := validateMediaPrice(p); err != nil {
		return err
	}
	if err := validateToolPrices(p.Rules); err != nil {
		return err
	}
	switch p.Mode {
	case "per_token", "":
		_, err := tokenAmount(gateway.Usage{}, p)
		return err
	case "per_request":
		if p.PerRequest < 0 {
			return fmt.Errorf("pricing: negative per-request price")
		}
		return nil
	case "expression", "tiered_expr":
		source, _ := p.Rules["expression"].(string)
		if source == "" {
			source, _ = p.Rules["expr"].(string)
		}
		parts := strings.Split(source, "|||")
		for i, part := range parts {
			if i == 0 {
				part = strings.TrimPrefix(strings.TrimSpace(part), "v1:")
			}
			n, _, err := parseExpression(strings.TrimSpace(part))
			if err != nil {
				return err
			}
			if i > 0 && (n.op != "binary" || n.text != "*" || n.children[0].op != "call" || n.children[0].text != "when" || len(n.children[0].children) != 1) {
				return fmt.Errorf("pricing: malformed request rule")
			}
		}
		return nil
	}
	return fmt.Errorf("%w: %q", ErrUnsupportedMode, p.Mode)
}
