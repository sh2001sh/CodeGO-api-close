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
	grants, err := commerceRenewableRows(ctx, source, ledgerTable)
	if err != nil {
		return err
	}
	quotaPerUnit, err := commerceRenewableQuotaPerUnit(d.options)
	if err != nil {
		return err
	}
	d.renewableBonuses, err = commerceCycleBonuses(d.rows["user_subscriptions"], members, accounts, grants, now.Unix(), quotaPerUnit)
	return err
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
	rows, err := loadRows(ctx, source, table)
	if err != nil {
		return nil, err
	}
	result := make([]commerceRow, len(rows))
	for i, raw := range rows {
		if err = json.Unmarshal(raw, &result[i]); err != nil {
			return nil, fmt.Errorf("legacy: invalid group renewal source: %w", err)
		}
	}
	return result, nil
}

func commerceCycleBonuses(subscriptions, members, accounts, grants []commerceRow, before int64, quotaPerUnit string) (map[int64]*big.Int, error) {
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
	for _, row := range grants {
		account, err := row.text("account_id")
		if err != nil {
			return nil, err
		}
		sub := accountSubs[account]
		if len(oldMarkers[sub]) == 0 {
			continue
		}
		reason, err := row.text("reason_code")
		if err != nil {
			return nil, err
		}
		key, err := row.text("idempotency_key")
		if err != nil {
			return nil, err
		}
		if reason != "subscription_bonus" || !strings.Contains(key, "group-buy:") {
			continue
		}
		created, err := fundingTime(row, "created_at")
		if err != nil {
			return nil, err
		}
		start, err := subs[sub].integer("start_time")
		if err != nil {
			return nil, err
		}
		// Unix truncates fractional timestamps to the original source's second
		// precision, equivalent to its exclusive before+1s SQL bound.
		if created.Before(time.Unix(start, 0)) || created.Unix() > before {
			continue
		}
		for _, marker := range oldMarkers[sub] {
			if strings.Contains(key, marker) {
				amount, err := row.integer("amount")
				if err != nil {
					return nil, err
				}
				if err = commerceAddRenewable(bonuses, sub, amount); err != nil {
					return nil, err
				}
				break
			}
		}
	}
	return bonuses, nil
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
