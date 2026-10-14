package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// AcceptOnlineSamples records a distinct, explicitly requested acceptance
// policy. It compares bounded source projections in the same snapshot as the
// catch-up, without claiming full historical reconciliation or opening money.
func (m *Importer) AcceptOnlineSamples(ctx context.Context, opts OnlineOptions) (OnlineReport, error) {
	r := OnlineReport{RunID: opts.RunID, Tables: map[string]int64{}, HistoryVerification: "sampled"}
	if !opts.SampledAcceptance || opts.SourceAdmin == nil {
		return r, errors.New("legacy: explicit sampled acceptance and capture administrator required")
	}
	ctx = m.historyContext(ctx)
	r.LedgerHistoryMode, r.HistoryCutoff = ledgerHistoryMode(ctx), historyCutoffLabel(ctx)
	if err := ValidateOnlineCaptureSource(ctx, m.source, opts.SourceAdmin); err != nil {
		return r, err
	}
	conn, err := m.onlineConnection(ctx)
	if err != nil {
		return r, err
	}
	defer closeOnlineConnection(conn)
	source, err := m.source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return r, err
	}
	defer func() { _ = source.Rollback(ctx) }()
	target, err := conn.Begin(ctx)
	if err != nil {
		return r, err
	}
	defer func() { _ = target.Rollback(ctx) }()
	if err = onlineAuthorize(ctx, target, opts.RunID); err != nil {
		return r, err
	}
	sources, specs, err := onlineBindings(ctx, source, target, opts.RunID)
	if err != nil {
		return r, err
	}
	var complete bool
	if err = target.QueryRow(ctx, "SELECT phase,NOT EXISTS(SELECT 1 FROM v3_migration_online.progress WHERE NOT complete) FROM v3_migration_online.run WHERE singleton").Scan(&r.Phase, &complete); err != nil {
		return r, err
	}
	if !complete || (r.Phase != "copied" && r.Phase != "sampled") {
		return r, errors.New("legacy: sampled acceptance requires a complete copied baseline")
	}
	p, err := loadOnlineProjector(ctx, source, target, sources)
	if err != nil {
		return r, err
	}
	capture, err := ValidateOnlineCapture(ctx, source, opts.RunID)
	if err != nil {
		return r, err
	}
	ids, err := onlineProjectPending(ctx, source, target, sources, specs, p, capture, 16384, true, &r)
	if err != nil {
		return r, err
	}
	users, err := loadUsers(ctx, source, sources)
	if err != nil {
		return r, err
	}
	for _, u := range sampledWallets(users) {
		// Active reservations are checked at final drain. This online observation
		// checks available money and does not pretend that a live wallet is frozen.
		if u.wallet.SnapshotUnits != nil && *u.wallet.SnapshotUnits != u.WalletUnits {
			return r, errors.New("legacy: sampled canonical wallet differs from user projection")
		}
		amount, err := OpeningBalance(u.WalletUnits)
		if err != nil {
			return r, err
		}
		if int64(amount)/2 != u.WalletUnits || credits.PerCredit != 2*V2UnitsPerCredit {
			return r, errors.New("legacy: sampled wallet credit conversion differs")
		}
		if _, exists := p.mappings[fundingAccountKey{"user", "wallet", u.ID}]; !exists {
			return r, errors.New("legacy: sampled wallet identity missing")
		}
		r.Tables["sampled_user_wallets"]++
	}
	options, err := loadOptions(ctx, source, sources["options"])
	if err != nil {
		return r, err
	}
	prices, err := buildPrices(options)
	if err != nil {
		return r, err
	}
	priceProbes, err := checkSampledPriceCharges(options, prices)
	if err != nil {
		return r, err
	}
	r.Tables["legacy_equivalent_charge_probes"] = priceProbes
	for _, price := range prices {
		if err = pricing.Validate(price); err != nil {
			return r, err
		}
		quantum, err := pricing.MoneyQuantum(price)
		if err != nil || quantum != 2 {
			return r, errors.New("legacy: imported price lost quota rounding")
		}
		// The pricing engine must reject invalid usage instead of silently charging.
		if _, err := pricing.Price(gateway.Usage{PromptTokens: -1}, price, 1); err == nil {
			return r, errors.New("legacy: imported price accepts negative usage")
		}
		r.Tables["validated_model_prices"]++
	}
	for _, spec := range specs {
		if spec.name != "funding_lots" && spec.name != "funding_allocations" && spec.name != "market.settlements" {
			continue
		}
		order := make([]string, len(spec.keys))
		for i, key := range spec.keys {
			order[i] = "t." + pgx.Identifier{key}.Sanitize()
		}
		// Both ends of the primary-key index are bounded; no full-table sort.
		for _, direction := range []string{"ASC", "DESC"} {
			parts := make([]string, len(order))
			for i, key := range order {
				parts[i] = key + " " + direction
			}
			rows, err := source.Query(ctx, "SELECT to_jsonb(t) FROM "+spec.source+" t ORDER BY "+strings.Join(parts, ",")+" LIMIT 32")
			if err != nil {
				return r, err
			}
			var input []json.RawMessage
			for rows.Next() {
				var raw []byte
				if err = rows.Scan(&raw); err != nil {
					rows.Close()
					return r, err
				}
				input = append(input, json.RawMessage(raw))
			}
			rows.Close()
			if err = rows.Err(); err != nil {
				return r, err
			}
			for _, raw := range input {
				projected, err := p.project(ctx, spec, raw, nil)
				if err != nil {
					return r, err
				}
				for _, row := range projected {
					staged := onlineStageTx{Tx: target, tables: onlineSortedTables(specs)}
					matches, err := checkExactBulk(ctx, staged, row.Schema, row.Table, row.Keys, []map[string]any{row.Values})
					if err != nil {
						return r, err
					}
					if len(matches) != 1 || !matches[0] {
						return r, fmt.Errorf("legacy: sampled %s projection differs", spec.name)
					}
					r.Tables["sampled_"+spec.name]++
				}
			}
		}
	}
	// Incremental totals remain incremental evidence. Do not relabel them as an
	// independently rebuilt total or write the full financial verification marker.
	if _, err = target.Exec(ctx, "INSERT INTO v3_migration_online.totals(name,value) VALUES('acceptance.sampled',1) ON CONFLICT(name) DO UPDATE SET value=EXCLUDED.value"); err != nil {
		return r, err
	}
	for key, n := range r.Tables {
		if _, err = target.Exec(ctx, "INSERT INTO v3_migration_online.totals(name,value) VALUES($1,$2) ON CONFLICT(name) DO UPDATE SET value=EXCLUDED.value", "acceptance."+key, n); err != nil {
			return r, err
		}
	}
	if _, err = target.Exec(ctx, "UPDATE v3_migration_online.run SET phase='sampled',verified_at=NULL WHERE singleton"); err != nil {
		return r, err
	}
	if err = target.Commit(ctx); err != nil {
		return r, err
	}
	if err = source.Commit(ctx); err != nil {
		return r, err
	}
	if len(ids) > 0 {
		if err = AckOnlineCapture(ctx, opts.SourceAdmin, opts.RunID, ids); err != nil {
			return r, err
		}
	}
	r.Phase, r.Applied, r.Acknowledged = "sampled", true, int64(len(ids))
	return r, nil
}

// Compare actual native charges with v2's original decimal coefficients and
// quota rounding, rather than merely asserting that a quote can be parsed.
func checkSampledPriceCharges(options map[string]string, prices map[string]catalog.Price) (int64, error) {
	defaults, err := pricingDefaults()
	if err != nil {
		return 0, err
	}
	maps := map[string]map[string]json.Number{}
	for _, key := range []string{"ModelRatio", "ModelPrice", "CompletionRatio", "CacheRatio", "CreateCacheRatio"} {
		values, err := numberMap(options[key])
		if err != nil {
			return 0, err
		}
		if _, exists := options[key]; !exists {
			for name, value := range defaults[key] {
				values[name] = json.Number(value)
			}
		}
		maps[key] = values
	}
	var probes int64
	for model, price := range prices {
		base := new(big.Rat)
		u := gateway.Usage{PromptTokens: 2048, CompletionTokens: 513, CachedTokens: 256, CacheWriteTokens: 128}
		add := func(count int64, coefficients ...string) error {
			term := new(big.Rat).SetInt64(count)
			for _, coefficient := range coefficients {
				factor, ok := new(big.Rat).SetString(coefficient)
				if !ok || factor.Sign() < 0 {
					return errors.New("legacy: invalid sampled price coefficient")
				}
				term.Mul(term, factor)
			}
			base.Add(base, term)
			return nil
		}
		switch price.Mode {
		case "per_token":
			ratio, exists := maps["ModelRatio"][model]
			if !exists {
				return probes, errors.New("legacy: sampled token price lacks original ratio")
			}
			read, write := "1", "1.25"
			if v, ok := maps["CacheRatio"][model]; ok {
				read = v.String()
			}
			if v, ok := maps["CreateCacheRatio"][model]; ok {
				write = v.String()
			}
			for _, row := range []struct {
				n       int64
				factors []string
			}{{1664, []string{ratio.String()}}, {513, []string{ratio.String(), completionRatio(model, maps["CompletionRatio"])}}, {256, []string{ratio.String(), read}}, {128, []string{ratio.String(), write}}} {
				if err = add(row.n, row.factors...); err != nil {
					return probes, err
				}
			}
		case "per_request":
			if unit, _ := price.Rules["billing_unit"].(string); unit != "" {
				continue
			}
			v, exists := maps["ModelPrice"][model]
			if !exists {
				return probes, errors.New("legacy: sampled call price lacks original amount")
			}
			if err = add(V2UnitsPerCredit, v.String()); err != nil {
				return probes, err
			}
			u = gateway.Usage{}
		default:
			// Expressions have request-specific variables; covered by their own
			// pricing validation and end-to-end probes after finalization.
			continue
		}
		for _, multiplier := range []int64{0, 500000, 1000000, 1200000} {
			actual, err := pricing.PriceForRequestPPM(u, price, multiplier, pricing.RequestInput{})
			if err != nil {
				return probes, err
			}
			exact := new(big.Rat).Mul(base, new(big.Rat).SetFrac64(multiplier, 1000000))
			q, rem := new(big.Int), new(big.Int)
			q.QuoRem(exact.Num(), exact.Denom(), rem)
			if new(big.Int).Lsh(rem, 1).Cmp(exact.Denom()) >= 0 {
				q.Add(q, big.NewInt(1))
			}
			q.Mul(q, big.NewInt(2))
			if !q.IsInt64() || int64(actual) != q.Int64() {
				return probes, fmt.Errorf("legacy: sampled model %s charge differs from original quota calculation", model)
			}
			probes++
		}
	}
	return probes, nil
}

func sampledWallets(users []sourceUser) []sourceUser {
	ordered := append([]sourceUser(nil), users...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].WalletUnits == ordered[j].WalletUnits {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].WalletUnits < ordered[j].WalletUnits
	})
	if len(ordered) <= 64 {
		return ordered
	}
	out := make([]sourceUser, 0, 64)
	for i := 0; i < 64; i++ {
		out = append(out, ordered[i*(len(ordered)-1)/63])
	}
	return out
}
