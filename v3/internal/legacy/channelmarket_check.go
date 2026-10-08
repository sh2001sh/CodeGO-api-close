package legacy

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

// Check uses the same explicit typed projections as import, but performs no
// mutations. Counts alone would miss a dropped row replaced with another ID or
// a modified amount, so every source key and field is compared independently.
func (m *Importer) checkChannelMarket(ctx context.Context, target pgx.Tx, data *channelMarketData, report *Report) error {
	if data == nil {
		return nil
	}
	if report.Counts == nil {
		report.Counts = map[string]int64{}
	}
	if report.Amounts == nil {
		report.Amounts = map[string]string{}
	}
	issue := func(entity string, id int64, detail string) {
		checkIssue(report, entity, id, detail)
	}
	expectedCounts := map[string]int64{}
	for _, r := range data.records {
		fields, err := cmCheckFields(r)
		if err != nil {
			return err
		}
		matches, err := checkProjection(ctx, target, r.table, fields)
		if err != nil {
			return err
		}
		expectedCounts[r.table]++
		if !matches {
			id, _ := r.values["id"].(int64)
			issue(r.table, id, "missing row or changed native projection for source key "+fmt.Sprint(r.values[r.keys[0]]))
		}
		report.Counts["check:channelmarket"]++
	}
	for _, table := range channelMarketSourceTables {
		if !cmStreamedTable(table) || data.streamTables[table] == "" {
			continue
		}
		native := "v3_channelmarket." + table
		expectedCounts[native] = 0
		emitted := false
		if err := data.streamBatches(ctx, table, func(records []cmRecord) error {
			values := make([]map[string]any, len(records))
			for i, record := range records {
				fields, err := cmCheckFields(record)
				if err != nil {
					return err
				}
				values[i] = fields
			}
			matches, err := checkExactBulk(ctx, target, "v3_channelmarket", table, records[0].keys, values)
			if err != nil {
				return err
			}
			for i, match := range matches {
				if !match {
					report.Counts["mismatched:"+native]++
					if !emitted {
						issue(native, 0, "missing row or changed native projection for source key "+fmt.Sprint(records[i].values[records[i].keys[0]]))
						emitted = true
					}
				}
			}
			expectedCounts[native] += int64(len(records))
			report.Counts["check:channelmarket"] += int64(len(records))
			return nil
		}); err != nil {
			return err
		}
	}
	for table, want := range expectedCounts {
		var actual int64
		if err := target.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&actual); err != nil {
			return err
		}
		report.Counts["check:"+table] = actual
		if actual != want {
			issue(table, 0, fmt.Sprintf("source projection count=%d target=%d", want, actual))
		}
	}
	for _, c := range data.channels {
		var owner int64
		var scope string
		var storedSettings json.RawMessage
		var concurrency, userConcurrency int64
		var cardSupported bool
		err := target.QueryRow(ctx, `SELECT COALESCE(owner_user_id,0),scope,settings,max_concurrency,max_user_concurrency,multiplier_card_supported FROM v3_catalog.channels WHERE id=$1`, c.catalogID).Scan(&owner, &scope, &storedSettings, &concurrency, &userConcurrency, &cardSupported)
		if err == pgx.ErrNoRows {
			issue("marketplace_channels", c.catalogID, "mapped gateway channel missing")
			continue
		}
		if err != nil {
			return err
		}
		if owner != c.owner || scope != "marketplace" || concurrency != cmInt(c.row, "max_concurrency") || userConcurrency != cmInt(c.row, "user_max_concurrency") {
			issue("marketplace_channels", c.catalogID, "mapped ownership/scope/concurrency differs")
		}
		var metadata map[string]json.RawMessage
		if err = json.Unmarshal(storedSettings, &metadata); err != nil {
			return err
		}
		if cardSupported != (c.row.text("multiplier_card_supported") == "true") {
			issue("marketplace_channels", c.catalogID, "multiplier-card capability differs")
		}
		if len(c.row["multiplier_card_user_enabled"]) > 0 {
			var market map[string]json.RawMessage
			var enabled bool
			if json.Unmarshal(metadata["market"], &market) != nil || json.Unmarshal(market["multiplier_card_user_enabled"], &enabled) != nil || enabled != (c.row.text("multiplier_card_user_enabled") == "true") {
				issue("marketplace_channels", c.catalogID, "multiplier-card activation differs")
			}
		}
		var community map[string]json.RawMessage
		if err = json.Unmarshal(metadata["community"], &community); err != nil {
			issue("marketplace_channels", c.catalogID, "public community metadata missing")
		} else {
			var public string
			_ = json.Unmarshal(community["id"], &public)
			if public != c.publicID {
				issue("marketplace_channels", c.catalogID, "public channel ID was changed")
			}
		}
		var groupFactor bool
		factor, err := cmFactor(c.group.text("multiplier"), false)
		if err != nil {
			return err
		}
		if err = target.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_catalog.groups WHERE name=$1 AND multiplier=$2::numeric/1000000)`, c.group.text("internal_group_name"), factor).Scan(&groupFactor); err != nil {
			return err
		}
		if !groupFactor {
			issue("marketplace_groups", c.catalogID, "internal group multiplier changed")
		}
		for _, model := range c.models {
			var exists bool
			if err = target.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_catalog.channel_models WHERE channel_id=$1 AND model=$2)`, c.catalogID, model).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				issue("marketplace_channels", c.catalogID, "declared model missing: "+model)
			}
		}
		if c.newCatalog {
			decrypter, ok := m.crypto.(catalog.Decrypter)
			if !ok {
				return fmt.Errorf("legacy: target credential decrypter required for market check")
			}
			var encrypted []byte
			var url, provider, kind string
			if err = target.QueryRow(ctx, `SELECT secret,base_url,provider,kind FROM v3_catalog.channels c JOIN v3_catalog.channel_credentials k ON k.channel_id=c.id WHERE c.id=$1 ORDER BY k.id LIMIT 1`, c.catalogID).Scan(&encrypted, &url, &provider, &kind); err == pgx.ErrNoRows {
				issue("marketplace_channels", c.catalogID, "credential missing")
			} else if err != nil {
				return err
			} else {
				plain, e := decrypter.Decrypt(encrypted)
				if e != nil || string(plain) != c.credential || url != c.url || provider != rProvider(c.row.text("provider_type")) || kind != c.credentialKind {
					issue("marketplace_channels", c.catalogID, "upstream credential/URL/provider differs")
				}
			}
		}
	}
	for owner, want := range data.pending {
		if err := cmCheckAccount(ctx, target, "user", owner, "marketplace_pending", want, report); err != nil {
			return err
		}
	}
	for _, binding := range data.keyBindings {
		expected := data.targetKeyGroup(binding.Group, binding.UserID)
		if expected == binding.Group {
			continue
		}
		var matched bool
		if err := target.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_identity.api_keys WHERE id=$1 AND user_id=$2 AND group_name=$3)`, binding.ID, binding.UserID, expected).Scan(&matched); err != nil {
			return err
		}
		if !matched {
			checkIssue(report, "api_key", binding.ID, "legacy market key binding was not mapped to its native internal group")
		}
	}
	if data.platformRevenue != nil {
		if err := cmCheckAccount(ctx, target, "platform", 1, "platform_revenue", *data.platformRevenue, report); err != nil {
			return err
		}
	}
	return nil
}

func cmCheckFields(r cmRecord) (map[string]any, error) {
	fields := make(map[string]any, len(r.values))
	for key, value := range r.values {
		fields[key] = value
	}
	for _, key := range []string{"token_hash", "fingerprint"} {
		if encoded, ok := fields[key].(string); ok {
			value, err := hex.DecodeString(strings.TrimPrefix(encoded, "\\x"))
			if err != nil {
				return nil, err
			}
			fields[key] = value
		}
	}
	for _, key := range []string{"score", "raw_success_rate", "wilson_success_rate", "avg_ttft_ms", "attempt_ttft_p50_ms", "attempt_ttft_p95_ms", "e2e_ttft_p50_ms", "e2e_ttft_p95_ms", "avg_latency_ms", "avg_tps", "cache_hit_rate"} {
		switch value := fields[key].(type) {
		case json.RawMessage:
			var metric float64
			if err := json.Unmarshal(value, &metric); err != nil {
				return nil, err
			}
			fields[key] = metric
		case int:
			fields[key] = float64(value)
		}
	}
	return fields, nil
}

func cmCheckAccount(ctx context.Context, target pgx.Tx, owner string, id int64, kind string, want int64, report *Report) error {
	var balance int64
	err := target.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE owner_type=$1 AND owner_id=$2 AND kind=$3`, owner, id, kind).Scan(&balance)
	if err == pgx.ErrNoRows {
		checkIssue(report, kind, id, "opening account missing")
		return nil
	}
	if err != nil {
		return err
	}
	report.Amounts["check:"+kind+":"+strconv.FormatInt(id, 10)] = strconv.FormatInt(balance, 10)
	if balance != want {
		checkIssue(report, kind, id, fmt.Sprintf("opening balance source=%d target=%d", want, balance))
	}
	return nil
}
