package legacy

import (
	"fmt"
	"math/big"
	"sort"
)

func (d *entitlementsData) relations(table string, fields map[string]any) error {
	for field, source := range map[string]string{
		"user_id": "users", "inviter_id": "users", "invitee_id": "users", "related_user_id": "users",
		"subscription_id": "user_subscriptions", "related_subscription_id": "user_subscriptions", "plan_id": "subscription_plans",
		"open_record_id": "blind_box_open_records", "draw_id": "subscription_lucky_draws", "reward_id": "subscription_lucky_rewards",
	} {
		id, exists := fields[field].(int64)
		if !exists {
			continue
		}
		if id <= 0 || d.refs[source][id] == nil {
			return fmt.Errorf("%s references absent source %s", field, source)
		}
		if field == "subscription_id" || field == "related_subscription_id" || field == "open_record_id" || field == "reward_id" {
			owner, _ := d.refs[source][id].integer("user_id")
			if owner != fields["user_id"] {
				return fmt.Errorf("%s source owner differs from row user", field)
			}
		}
	}
	if table == "referral_purchase_rewards" {
		if err := d.orderOrigin(fields["order_source_type"].(string), fields["order_source_id"].(string), fields["invitee_id"].(int64)); err != nil {
			return err
		}
	}
	if table == "subscription_reset_opportunity_ledgers" {
		if fields["change_type"] == "use" {
			if fields["source_ref"] != fmt.Sprint(fields["related_subscription_id"]) {
				return fmt.Errorf("reset use source reference differs from subscription")
			}
		} else {
			invitee, _ := fields["related_user_id"].(int64)
			if invitee <= 0 {
				return fmt.Errorf("reset earn requires invited user reference")
			}
			return d.orderOrigin(fields["source_type"].(string), fields["source_ref"].(string), invitee)
		}
	}
	return nil
}

func (d *entitlementsData) orderOrigin(kind, trade string, userID int64) error {
	table := map[string]string{"subscription_order": "subscription_orders", "blind_box_order": "blind_box_orders", "topup": "top_ups"}[kind]
	if table == "" || trade == "" {
		return fmt.Errorf("unsupported or absent referral purchase origin")
	}
	for _, row := range d.rows[table] {
		value, _ := row.text("trade_no")
		owner, _ := row.integer("user_id")
		if value == trade && owner == userID {
			return nil
		}
	}
	return fmt.Errorf("referral purchase origin is missing or belongs to another user")
}

func (d *entitlementsData) validateResetBalances(report *Report) {
	byUser := map[int64][]commerceRow{}
	for _, ledger := range d.rows["subscription_reset_opportunity_ledgers"] {
		userID, _ := ledger.integer("user_id")
		byUser[userID] = append(byUser[userID], ledger)
	}
	for _, account := range d.rows["subscription_reset_opportunity_accounts"] {
		id, _ := account.integer("id")
		userID, _ := account.integer("user_id")
		ledgers := byUser[userID]
		delete(byUser, userID)
		sort.SliceStable(ledgers, func(i, j int) bool {
			a, _ := ledgers[i].integer("created_at")
			b, _ := ledgers[j].integer("created_at")
			if a == b {
				a, _ = ledgers[i].integer("id")
				b, _ = ledgers[j].integer("id")
			}
			return a < b
		})
		balance, earned, used := new(big.Int), new(big.Int), new(big.Int)
		lastUsedMonth := ""
		usedMonths := map[string]bool{}
		for _, ledger := range ledgers {
			delta, _ := ledger.integer("delta")
			balance.Add(balance, big.NewInt(delta))
			if delta > 0 {
				earned.Add(earned, big.NewInt(delta))
			} else {
				used.Sub(used, big.NewInt(delta))
				month, _ := ledger.text("used_month")
				if usedMonths[month] {
					d.issue(report, "subscription_reset_opportunity_accounts", id, "multiple reset uses in the same month")
				}
				usedMonths[month] = true
				lastUsedMonth = month
			}
			stored, _ := ledger.integer("balance_after")
			if balance.Sign() < 0 || balance.Cmp(big.NewInt(stored)) != 0 {
				d.issue(report, "subscription_reset_opportunity_accounts", id, "reset ledger sequence has inconsistent balance")
				break
			}
		}
		storedMonth, _ := account.text("last_used_month")
		if storedMonth != lastUsedMonth {
			d.issue(report, "subscription_reset_opportunity_accounts", id, "reset last used month differs from ledger")
		}
		for field, computed := range map[string]*big.Int{"earned_total": earned, "used_total": used, "available_total": balance} {
			stored, _ := account.integer(field)
			if computed.Cmp(big.NewInt(stored)) != 0 {
				d.issue(report, "subscription_reset_opportunity_accounts", id, "reset account "+field+" differs from ledger")
			}
		}
	}
	for _, rows := range byUser {
		id, _ := rows[0].integer("id")
		d.issue(report, "subscription_reset_opportunity_ledgers", id, "reset account missing")
	}
}
