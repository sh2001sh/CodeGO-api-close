package legacy

import "math/big"

// Completed v2 wallet conversions and once-only migration audits stay in the
// source backup. Original amounts are evidence, not a new operational grant.
var entitlementAuditExclusions = map[string][]string{
	"unified_credit_user_migrations":        {"legacy_gpt_quota", "converted_unified_quota", "subscription_unified_quota"},
	"subscription_tier_settlements":         {"amount_total", "amount_used", "unused_amount", "settlement_quota"},
	"unified_credit_group_ratio_migrations": nil,
	"wallet_quota_conversions":              {"source_quota", "target_quota", "standard_quota_before", "standard_quota_after", "claude_quota_before", "claude_quota_after"},
}

func (d *entitlementsData) reportExcludedAudits(report *Report) {
	for table, fields := range entitlementAuditExclusions {
		rows := d.rows[table]
		prefix := "retired_features." + table
		report.Counts[prefix] = int64(len(rows))
		for _, field := range fields {
			total := new(big.Int)
			for _, row := range rows {
				value, err := row.integer(field)
				if err != nil {
					report.Counts[prefix+"."+field+"_unparseable"]++
					continue
				}
				total.Add(total, big.NewInt(value))
			}
			report.Amounts[prefix+"."+field+"_v2_units"] = total.String()
		}
	}
}
