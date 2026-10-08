package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
)

func (d *channelMarketData) loadAccountBalances(ctx context.Context, source pgx.Tx, sources map[string]string) error {
	if sources["accounts"] == "" || sources["balance_snapshots"] == "" {
		return nil
	}
	rows, err := source.Query(ctx, `SELECT a.owner_type,a.owner_id,a.account_type,s.available_balance,s.reserved_balance
		FROM `+sources["accounts"]+` a LEFT JOIN `+sources["balance_snapshots"]+` s ON s.account_id=a.account_id
		WHERE a.account_type IN ('marketplace_owner_pending','marketplace_platform_revenue')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := map[int64]bool{}
	for rows.Next() {
		var ownerType, kind string
		var owner int64
		var units, reserved *int64
		if err = rows.Scan(&ownerType, &owner, &kind, &units, &reserved); err != nil {
			return err
		}
		issue := func(detail string) {
			d.issues = append(d.issues, Issue{Entity: "marketplace_account", ID: owner, Code: "invalid_market_balance", Detail: detail})
		}
		if units == nil || reserved == nil {
			issue("market account has no canonical balance snapshot")
			continue
		}
		if *reserved != 0 {
			issue("market account has open reservations")
		}
		amount, e := OpeningBalance(*units)
		if e != nil {
			issue(e.Error())
			continue
		}
		if kind == "marketplace_owner_pending" {
			if ownerType != "user" {
				issue("pending market income account is not user-owned")
			}
			if found[owner] {
				issue("duplicate canonical pending market account")
			}
			found[owner] = true
			if int64(amount) != d.pending[owner] {
				issue("pending income balance differs from exact pending settlement sum")
			}
			if e := d.user(owner); e != nil {
				issue(e.Error())
			}
		} else {
			if ownerType != "system" || owner != 1 {
				issue("unsupported market platform revenue ownership")
			}
			if d.platformRevenue != nil {
				issue("duplicate platform revenue account")
			}
			value := int64(amount)
			d.platformRevenue = &value
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for owner, amount := range d.pending {
		if amount > 0 && !found[owner] {
			d.issues = append(d.issues, Issue{Entity: "marketplace_account", ID: owner, Code: "invalid_market_balance", Detail: "pending settlement has no canonical pending income account"})
		}
	}
	return nil
}

func (d *channelMarketData) prepareIncome() {
	for _, r := range d.rows["settlements"] {
		record, err := d.projectSettlement(r)
		if err == nil {
			err = d.addPendingSettlement(record)
		}
		if err != nil {
			d.issue("settlements", r, err)
			continue
		}
		d.records = append(d.records, record)
	}
	for _, r := range d.rows["income_reclaims"] {
		b := cmBuild()
		b.texts(r, "id", "status", "error_message")
		fingerprint, err := cmHex(r.text("fingerprint"))
		if err != nil {
			b.err = err
		}
		b.put("fingerprint", fingerprint)
		b.put("actor_user_id", nil)
		count := b.integer(r, "count", "count")
		amount := b.money(r, "amount", "amount_micro")
		b.integer(r, "batch_number", "batch_number")
		b.json(r, "filter", "filter", "{}")
		var filter map[string]any
		raw, e := r.structured("filter", "{}")
		if e == nil {
			e = json.Unmarshal(raw, &filter)
		}
		if e != nil {
			b.err = errors.New("invalid reclaim filter")
		}
		// Filter contains V2 units; convert the exact integer cap only.
		if len(raw) > 0 {
			var f map[string]json.RawMessage
			if err = json.Unmarshal(raw, &f); err == nil {
				for _, key := range []string{"MaxAmount", "max_amount"} {
					if len(f[key]) > 0 {
						var units int64
						if e = json.Unmarshal(f[key], &units); e != nil {
							b.err = errors.New("invalid reclaim maximum amount")
						} else {
							micro, e := FromV2Units(units)
							if e != nil {
								b.err = e
							}
							f[key], _ = json.Marshal(int64(micro))
						}
					}
				}
				payload, _ := json.Marshal(f)
				b.put("filter", json.RawMessage(payload))
			}
		}
		ownerRaw, e := r.structured("owner_amounts", "{}")
		var owners map[string]int64
		if e == nil {
			e = json.Unmarshal(ownerRaw, &owners)
		}
		if e != nil {
			b.err = errors.New("invalid reclaim owner amounts")
		}
		for owner, units := range owners {
			micro, e := FromV2Units(units)
			if e != nil || micro < 0 {
				b.err = errors.New("invalid reclaim owner amount")
			} else {
				owners[owner] = int64(micro)
			}
		}
		b.put("owner_amounts", owners)
		b.put("response", map[string]any{"count": count, "amount_micro": amount})
		b.times(r, "created_at", "updated_at")
		if status := r.text("status"); status != "completed" && status != "failed" {
			b.err = errors.New("in-flight income reclaim must finish before offline cutover")
		}
		d.record("v3_channelmarket.income_reclaims", []string{"id"}, b, r)
	}
}

func (d *channelMarketData) projectSettlement(r cmRow) (cmRecord, error) {
	b := cmBuild()
	b.texts(r, "id", "request_id", "billing_source", "status", "group_id")
	group, err := d.group(r.text("group_id"))
	if err != nil {
		b.err = err
	} else {
		c, e := d.channel(group.text("channel_id"))
		if e != nil {
			b.err = e
		} else {
			b.put("channel_id", c.catalogID)
			if c.owner != cmInt(r, "owner_user_id") {
				b.err = errors.New("settlement owner differs from channel owner")
			}
		}
	}
	owner := b.integer(r, "owner_user_id", "owner_user_id")
	consumer := b.integer(r, "consumer_user_id", "consumer_user_id")
	if err := d.user(owner); err != nil {
		b.err = err
	}
	if err := d.user(consumer); err != nil {
		b.err = err
	}
	b.money(r, "consumer_amount", "consumer_micro")
	gross := b.money(r, "settlement_gross_amount", "gross_micro")
	commission := b.money(r, "platform_commission", "commission_micro")
	fee := b.money(r, "transaction_fee", "fee_micro")
	net := b.money(r, "owner_net_amount", "net_micro")
	reclaimed := b.money(r, "reclaimed_amount", "reclaimed_micro")
	if commission > math.MaxInt64-fee || commission+fee > math.MaxInt64-net {
		b.err = errors.New("settlement component sum overflows")
	} else {
		// Earlier v2 settlements predate settlement_gross_amount; exact
		// components are the authoritative gross value for those rows.
		if gross == 0 {
			gross = commission + fee + net
			b.put("gross_micro", gross)
		}
		if gross != commission+fee+net {
			b.err = errors.New("settlement gross differs from exact component sum")
		}
	}
	if reclaimed > net {
		b.err = errors.New("reclaimed amount exceeds owner net")
	}
	b.factor(r, "multiplier", "multiplier_ppm", false)
	b.factor(r, "subscription_multiplier", "subscription_multiplier_ppm", true)
	b.times(r, "available_at", "released_at", "reclaimed_at", "forfeited_at", "created_at")
	if r.text("id") == "" || r.text("request_id") == "" {
		b.err = errors.New("settlement ID and request ID required")
	}
	status := r.text("status")
	switch status {
	case "pending":
		if reclaimed != 0 {
			b.err = errors.New("pending settlement has reclaimed amount")
		}

	case "released", "forfeited":
	case "reclaimed":
		if reclaimed != net {
			b.err = errors.New("fully reclaimed settlement amount differs from owner net")
		}
	default:
		b.err = fmt.Errorf("unknown settlement status %s", status)
	}
	return cmRecord{table: "v3_channelmarket.settlements", keys: []string{"id"}, values: b.values}, b.err
}
