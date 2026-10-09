package legacy

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The rows-only validator below is kept for tiny in-memory fixtures. Real
// snapshots stream once for exact projections and use PostgreSQL for global
// uniqueness/association checks instead of retaining millions of row keys.
func (d *fundingData) validateContext(ctx context.Context, report *Report) error {
	if d.source == nil {
		d.validate(report)
		return nil
	}
	if report.Counts == nil {
		report.Counts = map[string]int64{}
	}
	if report.Amounts == nil {
		report.Amounts = map[string]string{}
	}
	report.Issues = append(report.Issues, d.drains...)
	for name, count := range d.queues {
		report.Counts[name] = count
	}
	view := onlineViewFrom(ctx)
	if view != nil {
		// Retired values are evidence in original v2 units, never opening money.
		for key, value := range view.amounts {
			if !strings.HasPrefix(key, "retired_features.billing_funding_lots") && !strings.HasPrefix(key, "retired_features.billing_funding_allocations") {
				continue
			}
			if strings.HasSuffix(key, "_v2_units") {
				if _, ok := new(big.Int).SetString(value, 10); !ok {
					return fmt.Errorf("legacy: invalid verified retired funding amount %s", key)
				}
				report.Amounts[key] = value
			} else {
				count, err := strconv.ParseInt(value, 10, 64)
				if err != nil || count < 0 {
					return fmt.Errorf("legacy: invalid verified retired funding count %s", key)
				}
				report.Counts[key] = count
			}
		}
	}
	invalid := false
	for _, name := range fundingSourceNames {
		report.Counts["billing_"+name] = 0
		if view != nil && (name == "funding_lots" || name == "funding_allocations") {
			count, err := d.onlineFundingCount(view, name)
			if err != nil {
				return err
			}
			report.Counts["billing_"+name] = count
			if fundingSource(d.sources, name) == "" {
				continue
			}
			fields := []string{"amount"}
			if name == "funding_lots" {
				fields = []string{"original_amount", "remaining_amount"}
			}
			for _, field := range fields {
				key := "v3_billing." + name + "." + field
				value, exists := view.amounts[key]
				amount, ok := new(big.Int).SetString(value, 10)
				if !exists || !ok || amount.Sign() < 0 {
					return fmt.Errorf("legacy: missing or invalid verified funding amount %s", key)
				}
				report.Amounts["billing_"+name+"_"+field+"_micro_credits"] = amount.String()
			}
			continue
		}
		sums := map[string]*big.Int{}
		emitted := false
		err := d.walk(ctx, name, func(row commerceRow) error {
			if d.retiredFundingRow(name, row) {
				reportRetiredFunding(report, name, row)
				return nil
			}
			report.Counts["billing_"+name]++
			p, err := d.projectFunding(name, row)
			if err != nil {
				invalid = true
				// One representative issue per table keeps invalid snapshots bounded.
				if !emitted {
					report.Issues = append(report.Issues, Issue{"billing_" + name, 0, "invalid_funding_row", err.Error()})
					emitted = true
				}
				return nil
			}
			for _, field := range []string{"original_amount", "remaining_amount", "amount", "consumed_amount", "actual_amount"} {
				if amount, exists := p.values[field].(int64); exists {
					if sums[field] == nil {
						sums[field] = new(big.Int)
					}
					sums[field].Add(sums[field], big.NewInt(amount))
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("legacy: validate funding %s: %w", name, err)
		}
		for field, total := range sums {
			report.Amounts["billing_"+name+"_"+field+"_micro_credits"] = total.String()
		}
	}
	// Invalid projections already block import; SQL must not cast their malformed
	// values or attempt associations with a row that cannot be imported.
	if invalid {
		return nil
	}
	return d.validateFundingSQL(ctx, report)
}

func (d *fundingData) onlineFundingCount(view *onlineView, name string) (int64, error) {
	if fundingSource(d.sources, name) == "" {
		return 0, nil
	}
	key := "v3_billing." + name + ".rows"
	value, exists := view.amounts[key]
	count, err := strconv.ParseInt(value, 10, 64)
	if !exists || err != nil || count < 0 {
		return 0, fmt.Errorf("legacy: missing or invalid verified funding count %s", key)
	}
	return count, nil
}

func (d *fundingData) validateFundingSQL(ctx context.Context, report *Report) error {
	retired := d.retiredAccountIDs()
	active := func(name, alias string) string {
		if name == "funding_lots" || name == "funding_allocations" {
			return alias + ".account_id<>ALL($1::text[])"
		}
		return "true"
	}
	issueIf := func(name, code, detail, query string) error {
		var found bool
		// Queries without account filtering still consume the parameter so the
		// protocol never depends on implicit parameter type inference.
		if err := d.source.QueryRow(ctx, query, retired).Scan(&found); err != nil {
			return fmt.Errorf("legacy: funding %s %s: %w", name, code, err)
		}
		if found {
			report.Issues = append(report.Issues, Issue{"billing_" + name, 0, code, detail})
		}
		return nil
	}
	for _, name := range fundingSourceNames {
		if report.Counts["billing_"+name] == 0 {
			continue
		}
		table := fundingSource(d.sources, name)
		key := map[string]string{"funding_source_policies": "source", "funding_lots": "lot_id", "funding_allocations": "allocation_id", "wallet_reward_holds": "hold_id", "request_economics": "request_id"}[name]
		groups := [][]string{{key}}
		if name == "funding_lots" || name == "wallet_reward_holds" {
			groups = append(groups, []string{"idempotency_key"})
		}
		if name == "funding_allocations" {
			groups = append(groups, []string{"request_id", "lot_id"})
		}
		for i, fields := range groups {
			quoted := make([]string, len(fields))
			for j, field := range fields {
				quoted[j] = pgx.Identifier{field}.Sanitize()
			}
			code, detail := "duplicate_funding_id", "source funding IDs are duplicated"
			if i > 0 {
				code, detail = "duplicate_funding_idempotency", "source funding idempotency keys are duplicated"
				if name == "funding_allocations" {
					code, detail = "duplicate_request_lot", "request and lot allocation pair is duplicated"
				}
			}
			query := "SELECT EXISTS(SELECT 1 FROM " + table + " t WHERE " + active(name, "t") + " AND $1::text[] IS NOT NULL GROUP BY " + strings.Join(quoted, ",") + " HAVING count(*)>1)"
			if err := issueIf(name, code, detail, query); err != nil {
				return err
			}
		}
	}
	if report.Counts["billing_funding_allocations"] == 0 {
		return nil
	}
	alloc := fundingSource(d.sources, "funding_allocations")
	lot := fundingSource(d.sources, "funding_lots")
	if lot == "" {
		report.Issues = append(report.Issues, Issue{"billing_funding_allocations", 0, "missing_funding_lot", "allocation references an absent or invalid source lot"})
		return nil
	}
	join := " FROM " + alloc + " a LEFT JOIN " + lot + " l ON l.lot_id=a.lot_id AND " + active("funding_lots", "l") + " WHERE " + active("funding_allocations", "a")
	if err := issueIf("funding_allocations", "missing_funding_lot", "allocation references an absent or invalid source lot", "SELECT EXISTS(SELECT 1"+join+" AND l.lot_id IS NULL)"); err != nil {
		return err
	}
	if err := issueIf("funding_allocations", "funding_origin_mismatch", "allocation account, source or revenue multiplier differs from its lot", "SELECT EXISTS(SELECT 1"+join+" AND l.lot_id IS NOT NULL AND (a.account_id IS DISTINCT FROM l.account_id OR a.source IS DISTINCT FROM l.source OR round(coalesce(a.revenue_multiplier,0)*1000000) IS DISTINCT FROM round(coalesce(l.revenue_multiplier,0)*1000000)))"); err != nil {
		return err
	}
	// sum(bigint) is numeric in PostgreSQL, so both the aggregation and consumed
	// comparison remain exact even when totals exceed a signed bigint.
	query := "SELECT EXISTS(SELECT 1 FROM " + lot + " l JOIN (SELECT lot_id,sum(amount) total FROM " + alloc + " a WHERE " + active("funding_allocations", "a") + " GROUP BY lot_id) a USING(lot_id) WHERE " + active("funding_lots", "l") + " AND a.total > l.original_amount::numeric-l.remaining_amount::numeric)"
	return issueIf("funding_lots", "funding_allocation_exceeds_consumed", "settled allocations exceed consumed lot credits", query)
}

func (d *fundingData) validate(report *Report) {
	if report.Counts == nil {
		report.Counts = map[string]int64{}
	}
	if report.Amounts == nil {
		report.Amounts = map[string]string{}
	}
	report.Issues = append(report.Issues, d.drains...)
	for name, count := range d.queues {
		report.Counts[name] = count
	}
	lots := map[string]fundingProjection{}
	allocations := []fundingProjection{}
	for _, name := range fundingSourceNames {
		report.Counts["billing_"+name] = 0
		ids, idempotency, pairs := map[string]bool{}, map[string]bool{}, map[[2]string]bool{}
		sums := map[string]*big.Int{}
		for _, row := range d.rows[name] {
			if d.retiredFundingRow(name, row) {
				reportRetiredFunding(report, name, row)
				continue
			}
			report.Counts["billing_"+name]++
			projected, err := d.projectFunding(name, row)
			if err != nil {
				report.Issues = append(report.Issues, Issue{"billing_" + name, 0, "invalid_funding_row", err.Error()})
				continue
			}
			id := projected.values[projected.key].(string)
			if ids[id] {
				report.Issues = append(report.Issues, Issue{"billing_" + name, 0, "duplicate_funding_id", "source funding IDs are duplicated"})
			}
			ids[id] = true
			if key, exists := projected.values["idempotency_key"].(string); exists {
				if idempotency[key] {
					report.Issues = append(report.Issues, Issue{"billing_" + name, 0, "duplicate_funding_idempotency", "source funding idempotency keys are duplicated"})
				}
				idempotency[key] = true
			}
			if name == "funding_allocations" {
				pair := [2]string{projected.values["request_id"].(string), projected.values["lot_id"].(string)}
				if pairs[pair] {
					report.Issues = append(report.Issues, Issue{"billing_funding_allocations", 0, "duplicate_request_lot", "request and lot allocation pair is duplicated"})
				}
				pairs[pair] = true
				allocations = append(allocations, projected)
			}
			if name == "funding_lots" {
				lots[id] = projected
			}
			for _, field := range []string{"original_amount", "remaining_amount", "amount", "consumed_amount", "actual_amount"} {
				if amount, exists := projected.values[field].(int64); exists {
					if sums[field] == nil {
						sums[field] = new(big.Int)
					}
					sums[field].Add(sums[field], big.NewInt(amount))
				}
			}
		}
		for field, total := range sums {
			report.Amounts["billing_"+name+"_"+field+"_micro_credits"] = total.String()
		}
	}
	d.validateFundingAllocations(report, lots, allocations)
}

func (d *fundingData) validateFundingAllocations(report *Report, lots map[string]fundingProjection, allocations []fundingProjection) {
	sums := map[string]*big.Int{}
	for _, allocation := range allocations {
		lotID := allocation.values["lot_id"].(string)
		lot, exists := lots[lotID]
		if !exists {
			report.Issues = append(report.Issues, Issue{"billing_funding_allocations", 0, "missing_funding_lot", "allocation references an absent or invalid source lot"})
			continue
		}
		if allocation.values["source_account_id"] != lot.values["source_account_id"] || allocation.values["source"] != lot.values["source"] || allocation.values["revenue_multiplier_ppm"] != lot.values["revenue_multiplier_ppm"] {
			report.Issues = append(report.Issues, Issue{"billing_funding_allocations", 0, "funding_origin_mismatch", "allocation account, source or revenue multiplier differs from its lot"})
		}
		if sums[lotID] == nil {
			sums[lotID] = new(big.Int)
		}
		sums[lotID].Add(sums[lotID], big.NewInt(allocation.values["amount"].(int64)))
	}
	for id, total := range sums {
		lot := lots[id]
		consumed := lot.values["original_amount"].(int64) - lot.values["remaining_amount"].(int64)
		// Refunded lots can have an unallocated consumed remainder. Allocations
		// may not consume funds that the source still marks as available.
		if total.Cmp(big.NewInt(consumed)) > 0 {
			report.Issues = append(report.Issues, Issue{"billing_funding_lots", 0, "funding_allocation_exceeds_consumed", "settled allocations exceed consumed lot credits"})
		}
	}
}
