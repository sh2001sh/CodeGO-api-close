package legacy

import (
	"math/big"
)

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
