package legacy

import (
	"encoding/json"
	"math/big"
)

// The v2 "wallet" account backed users.quota (the old GPT wallet). Current
// monetary balances use claude_wallet. Preserve retired evidence in the source
// backup without applying monetary validation or creating historical accounts.
func retiredHistoryAccount(raw json.RawMessage) (string, bool) {
	var ref struct {
		ID   string `json:"account_id"`
		Kind string `json:"account_type"`
	}
	if json.Unmarshal(raw, &ref) != nil {
		return "", false
	}
	return ref.ID, retiredAccountKind(ref.Kind)
}

func (d *historyData) retiredHistoryEntry(raw json.RawMessage) bool {
	var ref struct {
		AccountID string `json:"account_id"`
	}
	return json.Unmarshal(raw, &ref) == nil && d.retiredAccounts[ref.AccountID]
}

func (d *historyData) reportRetiredHistoryEntry(raw json.RawMessage) {
	d.counts["retired:ledger_entries"]++
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return
	}
	for _, field := range []string{"amount", "balance_after"} {
		value := fields[field]
		if len(value) == 0 || string(value) == "null" {
			continue
		}
		amount, ok := new(big.Int).SetString(string(value), 10)
		if !ok {
			d.counts["retired:ledger_entries."+field+"_unparseable"]++
			continue
		}
		key := "retired:ledger_entries." + field + "_v2_units"
		if d.amounts[key] == nil {
			d.amounts[key] = new(big.Int)
		}
		d.amounts[key].Add(d.amounts[key], amount)
	}
}
