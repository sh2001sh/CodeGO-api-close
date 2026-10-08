package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5"
)

type entitlementsData struct {
	rows map[string][]commerceRow
	refs map[string]map[int64]commerceRow
}

func loadEntitlements(ctx context.Context, source pgx.Tx, sources map[string]string) (*entitlementsData, error) {
	d := &entitlementsData{rows: map[string][]commerceRow{}, refs: map[string]map[int64]commerceRow{}}
	names := []string{"users", "user_subscriptions", "subscription_plans", "blind_box_open_records", "blind_box_orders", "top_ups", "subscription_orders"}
	for _, contract := range entitlementContracts {
		names = append(names, contract.source)
	}
	for name := range entitlementAuditExclusions {
		names = append(names, name)
	}
	for _, name := range names {
		rows, err := loadRows(ctx, source, sources[name])
		if err != nil {
			return nil, fmt.Errorf("legacy: load entitlement %s: %w", name, err)
		}
		d.refs[name] = map[int64]commerceRow{}
		for _, raw := range rows {
			r := commerceRow{}
			if err := json.Unmarshal(raw, &r); err != nil {
				return nil, fmt.Errorf("legacy: decode entitlement %s: %w", name, err)
			}
			d.rows[name] = append(d.rows[name], r)
			id, _ := r.integer("id")
			d.refs[name][id] = r
		}
	}
	return d, nil
}

func entitlementProject(contract entitlementContract, row commerceRow) (map[string]any, error) {
	id, err := row.integer("id")
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("positive source ID required")
	}
	fields, err := commerceFields(row, contract.spec)
	if err != nil {
		return nil, err
	}
	if contract.source == "subscription_lucky_rewards" {
		if fields["participation_type"] == "" {
			fields["participation_type"] = "subscription"
		}
		for _, name := range []string{"subscription_id", "open_record_id"} {
			if fields[name] == int64(0) {
				fields[name] = nil
			}
		}
	}
	if fields["related_user_id"] == int64(0) {
		fields["related_user_id"] = nil
	}
	if contract.source == "subscription_reset_opportunity_ledgers" {
		fields["related_subscription_id"] = nil
		// v2 overloaded RelatedUserId: use refers to a subscription, earn to
		// the invited user. Native columns separate both real foreign keys.
		if fields["change_type"] == "use" {
			fields["related_subscription_id"] = fields["related_user_id"]
			fields["related_user_id"] = nil
		}
	}
	if err = entitlementRules(contract.source, fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func (d *entitlementsData) validate(report *Report) {
	if report.Counts == nil {
		report.Counts = map[string]int64{}
	}
	if report.Amounts == nil {
		report.Amounts = map[string]string{}
	}
	d.reportExcludedAudits(report)
	for _, contract := range entitlementContracts {
		report.Counts[contract.source] = int64(len(d.rows[contract.source]))
		ids := map[int64]bool{}
		sums := map[string]*big.Int{}
		unique := map[string]bool{}
		for _, row := range d.rows[contract.source] {
			id, _ := row.integer("id")
			if ids[id] {
				d.issue(report, contract.source, id, "duplicate source ID")
			}
			ids[id] = true
			fields, err := entitlementProject(contract, row)
			if err != nil {
				d.issue(report, contract.source, id, err.Error())
				continue
			}
			if err = d.relations(contract.source, fields); err != nil {
				d.issue(report, contract.source, id, err.Error())
			}
			for _, spec := range strings.Fields(contract.spec) {
				parts := strings.Split(spec, ":")
				if parts[2] == "c" {
					if sums[parts[0]] == nil {
						sums[parts[0]] = new(big.Int)
					}
					sums[parts[0]].Add(sums[parts[0]], big.NewInt(fields[parts[0]].(int64)))
				}
			}
			uniqueColumns := entitlementUnique[contract.source]
			if contract.source == "subscription_lucky_rewards" {
				participant := "subscription_id"
				if fields["participation_type"] == "blind_box" {
					participant = "open_record_id"
				}
				uniqueColumns = []string{"draw_id," + participant}
			}
			for _, columns := range uniqueColumns {
				key := columns
				for _, column := range strings.Split(columns, ",") {
					key += "|" + fmt.Sprintf("%T:%v", fields[column], fields[column])
				}
				if unique[key] {
					d.issue(report, contract.source, id, "duplicate native identity "+columns)
				}
				unique[key] = true
			}
		}
		for column, sum := range sums {
			report.Amounts[contract.source+"."+column] = sum.String()
		}
	}
	d.validateResetBalances(report)
}

func (d *entitlementsData) issue(report *Report, table string, id int64, detail string) {
	report.Issues = append(report.Issues, Issue{table, id, "invalid_entitlement", detail})
}

var entitlementUnique = map[string][]string{
	"subscription_lucky_numbers":              {"subscription_id", "card_code"},
	"blind_box_daily_lucky_numbers":           {"open_record_id"},
	"subscription_lucky_draws":                {"draw_date"},
	"subscription_lucky_reward_notifications": {"reward_id"},
	"subscription_blind_box_benefit_cycles":   {"subscription_id,benefit_cycle", "idempotency_key"},
	"subscription_reset_opportunity_accounts": {"user_id"},
	"subscription_reset_opportunity_ledgers":  {"event_key"},
	"referral_purchase_rewards":               {"invitee_id"},
	"subscription_claude_conversions":         {"request_id"},
}
