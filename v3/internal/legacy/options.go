package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func loadOptions(ctx context.Context, tx pgx.Tx, table string) (map[string]string, error) {
	rows, err := loadRows(ctx, tx, table)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, row := range rows {
		var option struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err = json.Unmarshal(row, &option); err != nil {
			return nil, err
		}
		out[option.Key] = option.Value
	}
	return out, nil
}

func (m *Importer) importOptions(ctx context.Context, tx pgx.Tx, options map[string]string, prices map[string]catalog.Price) error {
	for key, value := range options {
		encoded, err := m.sourceOption(value)
		if err != nil {
			return err
		}
		lower := strings.ToLower(key)
		sensitive := false
		if strings.HasSuffix(lower, "key") {
			sensitive = true
		}
		for _, part := range []string{"secret", "password", "token", "credential", "private", "apikey", "api_key"} {
			if strings.Contains(lower, part) {
				sensitive = true
			}
		}
		var plaintext, ciphertext []byte
		if sensitive {
			var err error
			if ciphertext, err = m.crypto.Encrypt(encoded); err != nil {
				return err
			}
		} else {
			plaintext = encoded
		}
		if _, err := tx.Exec(ctx, `INSERT INTO v3_platform.settings(key,value,ciphertext,sensitive) VALUES($1,$2::jsonb,$3,$4)
			ON CONFLICT(key) DO NOTHING`, key, plaintext, ciphertext, sensitive); err != nil {
			return fmt.Errorf("legacy: import option %s: %w", key, err)
		}
	}
	groups, err := numberMap(options["GroupRatio"])
	if err != nil {
		return err
	}
	for name, value := range groups {
		if _, err = tx.Exec(ctx, `INSERT INTO v3_catalog.groups(name,multiplier) VALUES($1,$2::numeric)
			ON CONFLICT(name) DO UPDATE SET multiplier=EXCLUDED.multiplier`, name, value.String()); err != nil {
			return err
		}
	}
	for model, price := range prices {
		rules, marshalErr := json.Marshal(price.Rules)
		if marshalErr != nil {
			return marshalErr
		}
		if price.Rules == nil {
			rules = []byte("{}")
		}
		_, err = tx.Exec(ctx, `INSERT INTO v3_catalog.model_prices(model,mode,input_per_mtok,output_per_mtok,cache_read_per_mtok,cache_write_per_mtok,per_request,rules)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb) ON CONFLICT(model) DO NOTHING`, model, price.Mode, price.InputPerMTok, price.OutputPerMTok, price.CacheReadPerMTok, price.CacheWritePerMTok, price.PerRequest, rules)
		if err != nil {
			return err
		}
	}
	optionReport := Report{Counts: map[string]int64{}}
	if err := m.checkOptions(ctx, tx, options, prices, &optionReport); err != nil {
		return err
	}
	if len(optionReport.Issues) > 0 {
		return fmt.Errorf("legacy: imported option conflicts with target")
	}
	return nil
}

func numberMap(raw string) (map[string]json.Number, error) {
	result := map[string]json.Number{}
	if raw == "" {
		return result, nil
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("legacy: invalid numeric configuration: %w", err)
	}
	return result, nil
}

// decimalProduct rounds once, half-up, after multiplying exact rational
// coefficients. No floating-point values enter monetary conversion.
func decimalProduct(values ...string) (int64, error) {
	r := new(big.Rat).SetInt64(1)
	for _, value := range values {
		factor, ok := new(big.Rat).SetString(value)
		if !ok || factor.Sign() < 0 {
			return 0, fmt.Errorf("legacy: invalid nonnegative price coefficient")
		}
		r.Mul(r, factor)
	}
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(r.Num(), r.Denom(), remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(r.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, fmt.Errorf("legacy: monetary conversion overflow")
	}
	return quotient.Int64(), nil
}

func buildPrices(options map[string]string) (map[string]catalog.Price, error) {
	defaults, err := pricingDefaults()
	if err != nil {
		return nil, err
	}
	maps := map[string]map[string]json.Number{}
	for _, key := range []string{"ModelRatio", "ModelPrice", "CompletionRatio", "CacheRatio", "CreateCacheRatio"} {
		values, err := numberMap(options[key])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		maps[key] = values
		if _, persisted := options[key]; !persisted {
			for model, value := range defaults[key] {
				values[model] = json.Number(value)
			}
		}
	}
	prices := map[string]catalog.Price{}
	for model, input := range maps["ModelRatio"] {
		output := completionRatio(model, maps["CompletionRatio"])
		read, write := "1", "1.25"
		if value, ok := maps["CacheRatio"][model]; ok {
			read = value.String()
		}
		if value, ok := maps["CreateCacheRatio"][model]; ok {
			write = value.String()
		}
		p := catalog.Price{Model: model, Mode: "per_token"}
		var err error
		if p.InputPerMTok, err = decimalProduct(input.String(), "2000000"); err != nil {
			return nil, err
		}
		if p.OutputPerMTok, err = decimalProduct(input.String(), output, "2000000"); err != nil {
			return nil, err
		}
		if p.CacheReadPerMTok, err = decimalProduct(input.String(), read, "2000000"); err != nil {
			return nil, err
		}
		if p.CacheWritePerMTok, err = decimalProduct(input.String(), write, "2000000"); err != nil {
			return nil, err
		}
		prices[model] = p
	}
	for model, value := range maps["ModelPrice"] {
		amount, err := decimalProduct(value.String(), "1000000")
		if err != nil {
			return nil, err
		}
		price := catalog.Price{Model: model, Mode: "per_request", PerRequest: amount}
		if unit := legacyMediaUnit(model); unit != "" {
			price.Rules = map[string]any{"billing_unit": unit}
		}
		prices[model] = price
	}
	modes, expressions := map[string]string{}, map[string]string{}
	if raw := options["billing_setting.billing_mode"]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &modes); err != nil {
			return nil, err
		}
	}
	if raw := options["billing_setting.billing_expr"]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &expressions); err != nil {
			return nil, err
		}
	}
	for model, expression := range defaults["billing_setting.billing_expr"] {
		if _, ok := modes[model]; !ok {
			modes[model] = "tiered_expr"
		}
		if _, ok := expressions[model]; !ok {
			expressions[model] = expression
		}
	}
	for model, mode := range modes {
		if mode != "tiered_expr" {
			continue
		}
		p := catalog.Price{Model: model, Mode: "expression", Rules: map[string]any{"expression": expressions[model]}}
		if err := pricing.Validate(p); err != nil {
			return nil, fmt.Errorf("legacy: expression for %s: %w", model, err)
		}
		prices[model] = p
	}
	for model, price := range prices {
		if price.Rules == nil {
			price.Rules = map[string]any{}
		}
		// v2 rounds to integer quota units before converting stored amounts x2.
		price.Rules["money_quantum"] = int64(2)
		prices[model] = price
	}
	return prices, nil
}
