package legacy

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func (m *Importer) checkOptions(ctx context.Context, target pgx.Tx, options map[string]string, prices map[string]catalog.Price, report *Report) error {
	for key, value := range options {
		encoded := []byte(value)
		if !json.Valid(encoded) {
			encoded, _ = json.Marshal(value)
		}
		var plaintext, ciphertext []byte
		var sensitive bool
		err := target.QueryRow(ctx, `SELECT value,ciphertext,sensitive FROM v3_platform.settings WHERE key=$1`, key).Scan(&plaintext, &ciphertext, &sensitive)
		if err == pgx.ErrNoRows {
			checkIssue(report, "setting", 0, "an imported setting is missing")
			continue
		}
		if err != nil {
			return err
		}
		if sensitive {
			plaintext, err = m.decrypt(ciphertext)
			if err != nil {
				checkIssue(report, "setting", 0, "an imported secret cannot be decrypted")
				continue
			}
		}
		var match bool
		if err = target.QueryRow(ctx, `SELECT $1::jsonb=$2::jsonb`, plaintext, encoded).Scan(&match); err != nil {
			return err
		}
		if !match {
			checkIssue(report, "setting", 0, "an imported setting differs from source")
		}
	}
	groups, err := numberMap(options["GroupRatio"])
	if err != nil {
		return err
	}
	for name, value := range groups {
		match, err := checkProjection(ctx, target, "v3_catalog.groups", map[string]any{"name": name, "multiplier": value.String()})
		if err != nil {
			return err
		}
		if !match {
			checkIssue(report, "group", 0, "official group multiplier differs from source")
		}
	}
	for model, price := range prices {
		rules, err := json.Marshal(price.Rules)
		if err != nil {
			return err
		}
		if price.Rules == nil {
			rules = []byte("{}")
		}
		fields := map[string]any{"model": model, "mode": price.Mode, "input_per_mtok": price.InputPerMTok, "output_per_mtok": price.OutputPerMTok, "cache_read_per_mtok": price.CacheReadPerMTok, "cache_write_per_mtok": price.CacheWritePerMTok, "per_request": price.PerRequest, "rules": rules}
		match, err := checkProjection(ctx, target, "v3_catalog.model_prices", fields)
		if err != nil {
			return err
		}
		if !match {
			checkIssue(report, "pricing", 0, "model price differs from source for "+strings.TrimSpace(model))
		}
	}
	report.Counts["check:settings"] = int64(len(options))
	report.Counts["check:prices"] = int64(len(prices))
	return nil
}
