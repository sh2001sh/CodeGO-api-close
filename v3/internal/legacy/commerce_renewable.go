package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Only the plan grant and verified current-cycle group rewards are renewable.
// Fuel is deliberately absent from this allowance, including delayed rewards
// whose membership belongs to a previous cycle but ledger credit does not.
func (d *commerceData) subscriptionRenewable(id, total, base int64) int64 {
	base = min(total, base)
	bonus := d.renewableBonuses[id]
	if bonus == nil || bonus.Sign() == 0 {
		return base
	}
	cap := big.NewInt(total - base)
	if bonus.Cmp(cap) >= 0 {
		return total
	}
	return base + bonus.Int64()
}

func (d *commerceData) loadRenewableBonuses(ctx context.Context, source pgx.Tx, sources map[string]string) error {
	table := sources["marketplace_group_buy_members"]
	if table == "" {
		table = sources["group_buy_members"]
	}
	members, err := commerceRenewableRows(ctx, source, table)
	if err != nil || len(members) == 0 {
		return err
	}
	var now time.Time
	if err = source.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&now); err != nil {
		return err
	}
	accounts, err := commerceRenewableRows(ctx, source, sources["accounts"])
	if err != nil {
		return err
	}
	ledgerTable := sources["billing_ledger_entries"]
	if ledgerTable == "" {
		ledgerTable = sources["ledger_entries"]
	}
	quotaPerUnit, err := commerceRenewableQuotaPerUnit(d.options)
	if err != nil {
		return err
	}
	cycle, err := commercePrepareCycleBonuses(d.rows["user_subscriptions"], members, accounts, now.Unix(), quotaPerUnit)
	if err != nil {
		return err
	}
	if err := cycle.walkDelayedGrants(ctx, source, ledgerTable, now.Unix()); err != nil {
		return err
	}
	d.renewableBonuses = cycle.bonuses
	return nil
}

func commerceRenewableQuotaPerUnit(options map[string]string) (string, error) {
	value, present := options["QuotaPerUnit"]
	if !present {
		return "500000", nil
	}
	value = strings.TrimSpace(value)
	number, err := strconv.ParseFloat(value, 64)
	exact, ok := new(big.Rat).SetString(value)
	if err != nil || number <= 0 || math.IsInf(number, 0) || math.IsNaN(number) || !ok || exact.Sign() <= 0 || !json.Valid([]byte(value)) {
		return "", fmt.Errorf("legacy: QuotaPerUnit must be a positive finite decimal")
	}
	return value, nil
}

func commerceRenewableRows(ctx context.Context, source pgx.Tx, table string) ([]commerceRow, error) {
	var result []commerceRow
	err := walkHistory(ctx, source, table, func(raw json.RawMessage) error {
		var row commerceRow
		if err := json.Unmarshal(raw, &row); err != nil {
			return fmt.Errorf("legacy: invalid group renewal source: %w", err)
		}
		result = append(result, row)
		return nil
	})
	return result, err
}

func commerceCycleBonuses(subscriptions, members, accounts, grants []commerceRow, before int64, quotaPerUnit string) (map[int64]*big.Int, error) {
	cycle, err := commercePrepareCycleBonuses(subscriptions, members, accounts, before, quotaPerUnit)
	if err != nil {
		return nil, err
	}
	for _, row := range grants {
		if err := cycle.addDelayedGrant(row, before); err != nil {
			return nil, err
		}
	}
	return cycle.bonuses, nil
}

type commerceBonusCycle struct {
	subs        map[int64]commerceRow
	bonuses     map[int64]*big.Int
	oldMarkers  map[int64][]string
	accountSubs map[string]int64
}

func commercePrepareCycleBonuses(subscriptions, members, accounts []commerceRow, before int64, quotaPerUnit string) (*commerceBonusCycle, error) {
	subs := map[int64]commerceRow{}
	for _, row := range subscriptions {
		id, err := row.integer("id")
		if err != nil {
			return nil, err
		}
		subs[id] = row
	}
	bonuses, oldMarkers := map[int64]*big.Int{}, map[int64][]string{}
	for _, row := range members {
		granted, err := row.boolean("bonus_granted")
		if err != nil {
			return nil, err
		}
		if !granted {
			continue
		}
		sub, err := row.integer("user_subscription_id")
		if err != nil {
			return nil, err
		}
		if subs[sub] == nil {
			continue
		}
		owner, err := row.integer("user_id")
		if err != nil {
			return nil, err
		}
		user, err := subs[sub].integer("user_id")
		if err != nil || owner != user {
			return nil, fmt.Errorf("legacy: group renewal membership owner differs from subscription")
		}
		usd, err := row.decimal("bonus_amount_usd")
		if err != nil {
			return nil, err
		}
		value, ok := new(big.Rat).SetString(usd)
		if !ok {
			return nil, fmt.Errorf("legacy: invalid group renewal reward amount")
		}
		if value.Sign() == 0 {
			continue
		}
		created, err := row.integer("created_at")
		if err != nil || created < 0 {
			return nil, fmt.Errorf("legacy: invalid group renewal membership timestamp")
		}
		if created > before {
			continue
		}
		start, err := subs[sub].integer("start_time")
		if err != nil {
			return nil, err
		}
		if created >= start {
			amount, err := decimalProduct(usd, quotaPerUnit)
			if err != nil {
				return nil, err
			}
			if err = commerceAddRenewable(bonuses, sub, amount); err != nil {
				return nil, err
			}
		} else {
			id, err := row.integer("id")
			if err != nil || id <= 0 {
				return nil, fmt.Errorf("legacy: delayed group renewal membership requires positive ID")
			}
			oldMarkers[sub] = append(oldMarkers[sub], fmt.Sprintf(":member:%d:", id))
		}
	}
	accountSubs := map[string]int64{}
	for _, row := range accounts {
		fields, err := commerceFields(row, `kind:account_type:s owner:owner_type:s unit:quota_unit:s`)
		if err != nil {
			return nil, err
		}
		kind, owner, unit := fields["kind"], fields["owner"], fields["unit"]
		if kind == "subscription" && owner == "user_subscription" && unit == "quota" {
			id, err := row.integer("owner_id")
			if err != nil {
				return nil, err
			}
			account, err := row.text("account_id")
			if err != nil {
				return nil, err
			}
			accountSubs[account] = id
		}
	}
	return &commerceBonusCycle{subs, bonuses, oldMarkers, accountSubs}, nil
}

func (cycle *commerceBonusCycle) addDelayedGrant(row commerceRow, before int64) error {
	account, err := row.text("account_id")
	if err != nil {
		return err
	}
	sub := cycle.accountSubs[account]
	if len(cycle.oldMarkers[sub]) == 0 {
		return nil
	}
	reason, err := row.text("reason_code")
	if err != nil {
		return err
	}
	key, err := row.text("idempotency_key")
	if err != nil {
		return err
	}
	if reason != "subscription_bonus" || !strings.Contains(key, "group-buy:") {
		return nil
	}
	created, err := fundingTime(row, "created_at")
	if err != nil {
		return err
	}
	start, err := cycle.subs[sub].integer("start_time")
	if err != nil {
		return err
	}
	// The exclusive next-second SQL bound retains fractional source timestamps.
	if created.Before(time.Unix(start, 0)) || created.Unix() > before {
		return nil
	}
	for _, marker := range cycle.oldMarkers[sub] {
		if strings.Contains(key, marker) {
			amount, err := row.integer("amount")
			if err != nil {
				return err
			}
			return commerceAddRenewable(cycle.bonuses, sub, amount)
		}
	}
	return nil
}

func (cycle *commerceBonusCycle) walkDelayedGrants(ctx context.Context, source pgx.Tx, table string, before int64) error {
	if table == "" {
		return nil
	}
	accounts := make([]string, 0)
	var earliest int64
	for account, sub := range cycle.accountSubs {
		if len(cycle.oldMarkers[sub]) == 0 {
			continue
		}
		start, err := cycle.subs[sub].integer("start_time")
		if err != nil {
			return err
		}
		if len(accounts) == 0 || start < earliest {
			earliest = start
		}
		accounts = append(accounts, account)
	}
	if len(accounts) == 0 {
		return nil
	}
	// Filter before serializing source records: only delayed group credits on
	// relevant subscription accounts can affect this allowance. The full ledger
	// can contain tens of millions of unrelated entries and must stay in SQL.
	query := "SELECT to_jsonb(t) FROM " + table + ` t WHERE account_id=ANY($1::text[])
	 AND reason_code='subscription_bonus' AND strpos(idempotency_key,'group-buy:')>0
	 AND created_at >= $2 AND created_at < $3`
	rows, err := source.Query(ctx, query, accounts, time.Unix(earliest, 0).UTC(), time.Unix(before, 0).UTC().Add(time.Second))
	if err != nil {
		return fmt.Errorf("legacy: read delayed group renewal grants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		var row commerceRow
		if err := json.Unmarshal(raw, &row); err != nil {
			return fmt.Errorf("legacy: invalid group renewal grant: %w", err)
		}
		if err := cycle.addDelayedGrant(row, before); err != nil {
			return err
		}
	}
	return rows.Err()
}

func commerceAddRenewable(bonuses map[int64]*big.Int, sub, amount int64) error {
	micro, err := OpeningBalance(amount)
	if err != nil {
		return fmt.Errorf("legacy: group renewal grant: %w", err)
	}
	if bonuses[sub] == nil {
		bonuses[sub] = new(big.Int)
	}
	bonuses[sub].Add(bonuses[sub], big.NewInt(int64(micro)))
	return nil
}
