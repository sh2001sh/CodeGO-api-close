package legacy

import (
	"math/big"
)

// Retired points are source evidence only. Filter before validating/converting
// amounts: even a valid v2 bigint may exceed the new monetary bigint limit.
func (d *fundingData) retiredFundingRow(name string, row commerceRow) bool {
	if name != "funding_lots" && name != "funding_allocations" {
		return false
	}
	accountID, err := row.text("account_id")
	if err != nil {
		return false
	}
	account, exists := d.accounts[accountID]
	return exists && retiredAccountKind(account.Kind)
}

func reportRetiredFunding(report *Report, name string, row commerceRow) {
	prefix := "retired_features.billing_" + name
	report.Counts[prefix]++
	fields := []string{"amount"}
	if name == "funding_lots" {
		fields = []string{"original_amount", "remaining_amount"}
	}
	for _, field := range fields {
		raw := row[field]
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		amount, ok := new(big.Int).SetString(string(raw), 10)
		if !ok {
			report.Counts[prefix+"."+field+"_unparseable"]++
			continue
		}
		key := prefix + "." + field + "_v2_units"
		total := new(big.Int)
		if previous := report.Amounts[key]; previous != "" {
			total.SetString(previous, 10)
		}
		report.Amounts[key] = total.Add(total, amount).String()
	}
}
