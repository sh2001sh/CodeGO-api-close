package legacy

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (d *marketplaceData) normalizeBlindBoxes() {
	purchases := d.source["balance_blind_box_purchases"]
	items := d.source["balance_blind_box_items"]
	if len(purchases) == 0 {
		purchases = d.source["blind_box_purchases"]
	}
	if len(items) == 0 {
		items = d.source["blind_box_items"]
	}
	users := marketplaceIndex(d.source["users"])
	hasPoolConfiguration := false
	for _, option := range d.source["options"] {
		if strings.HasPrefix(option.text("key"), "blind_box_setting.") {
			hasPoolConfiguration = true
			break
		}
	}
	if hasPoolConfiguration || len(purchases)+len(items)+len(d.source["blind_box_open_records"])+len(d.source["blind_box_orders"])+len(d.source["blind_box_pity_states"])+len(d.source["balance_blind_box_pity_states"]) > 0 {
		d.normalizePools()
	}
	for _, r := range purchases {
		id := r.id()
		user := d.integer(r, "user_id")
		quantity := d.integer(r, "quantity")
		price := d.money(r, "unit_price_usd", 1000000)
		if users[user] == nil {
			d.problem("blind_box_purchase", id, "missing_blind_box_user", "purchaser does not exist")
		}
		if quantity < 1 || quantity > 2147483647 {
			d.problem("blind_box_purchase", id, "invalid_blind_box_quantity", "target purchase quantity must be a positive int32")
		}
		if status := r.text("status"); status != "" && status != "completed" {
			d.problem("blind_box_purchase", id, "invalid_blind_box_purchase_state", "only completed balance inventory purchases are representable")
		}
		if _, ok := r["unit_price_micro"]; ok {
			price = d.integer(r, "unit_price_micro")
		}
		if total := d.integer(r, "total_quota"); total > 0 && (price > 0 && quantity > 0 && total <= 9223372036854775807/2 && price <= 9223372036854775807/quantity && total*2 != price*quantity) {
			d.problem("blind_box_purchase", id, "blind_box_purchase_amount_mismatch", "paid total differs from unit price times quantity")
		}
		pool := d.integer(r, "pool_id")
		if pool == 0 {
			pool = 1
		}
		fields := map[string]any{"user_id": user, "pool_id": pool, "quantity": quantity, "unit_price_micro": price, "is_grant": r.flag("is_grant"), "purchase_date": r.text("purchase_date"), "created_at": d.instant(r, "created_at", true), "request_id": r.text("request_id"), "status": r.text("status")}
		var external any
		if orderID := d.externalOrderForPurchase(r.text("request_id")); orderID > 0 {
			external = orderID
		}
		fields["external_order_id"] = external
		total, conversionErr := FromV2Units(d.integer(r, "total_quota"))
		if conversionErr != nil {
			d.problem("blind_box_purchase", id, "invalid_blind_box_purchase_total", conversionErr.Error())
		}
		fields["total_micro"] = int64(total)
		d.add("blind_box_purchases", id, fields)
		if r.text("request_id") == "" {
			d.problem("blind_box_purchase", id, "missing_blind_box_request", "purchase request_id required for idempotence")
		}
	}
	purchaseIndex := marketplaceIndex(purchases)
	openItem := make(map[int64]marketplaceSourceRow)
	for _, r := range items {
		id := r.id()
		purchaseID := d.integer(r, "purchase_id")
		owner := d.integer(r, "owner_user_id")
		status := r.text("status")
		if purchaseIndex[purchaseID] == nil || users[owner] == nil {
			d.problem("blind_box_item", id, "invalid_blind_box_item_reference", "purchase or owner does not exist")
		}
		if status != "available" && status != "opened" && status != "revoked" {
			d.problem("blind_box_item", id, "invalid_blind_box_item_state", "unknown inventory state")
		}
		opened := d.instant(r, "opened_at", false)
		if (status == "opened") != (opened != nil) {
			d.problem("blind_box_item", id, "invalid_blind_box_open_time", "opened status and timestamp disagree")
		}
		pool := d.integer(r, "pool_id")
		if pool == 0 {
			pool = 1
		}
		var rewards any
		if r.text("pool_version") == "unified-box-v4-open-draw" && r.text("reward_type") == "" {
			rewards = d.poolRewards(pool)
		} else if raw := r["rewards"]; len(raw) > 0 {
			rewards = json.RawMessage(raw)
		} else {
			reward, err := d.reward(r)
			if err != nil {
				d.problem("blind_box_item", id, "unsupported_blind_box_reward", err.Error())
			}
			rewards = []map[string]any{reward}
		}
		payload, err := json.Marshal(rewards)
		if err != nil {
			d.problem("blind_box_item", id, "invalid_blind_box_rewards", err.Error())
		}
		guarantees := json.RawMessage(`{}`)
		if r.text("pool_version") == "unified-box-v4-open-draw" && r.text("reward_type") == "" {
			guarantees = d.poolGuarantees(pool)
		}
		drawCurrent := r.text("pool_version") == "unified-box-v4-open-draw" && r.text("reward_type") == ""
		var frozen any
		if !drawCurrent {
			var values []json.RawMessage
			if err := json.Unmarshal(payload, &values); err == nil && len(values) == 1 {
				frozen = values[0]
			}
		}
		d.add("blind_box_items", id, map[string]any{"purchase_id": purchaseID, "owner_user_id": owner, "purchase_user_id": d.integer(r, "purchase_user_id"), "pool_id": pool, "rewards": payload, "guarantees": guarantees, "draw_current_pool": drawCurrent, "frozen_reward": frozen, "status": status, "created_at": d.instant(r, "created_at", true), "opened_at": opened, "updated_at": d.instant(r, "updated_at", true), "pool_version": r.text("pool_version"), "reward_tier": r.text("reward_tier"), "reward_wallet_type": r.text("reward_wallet_type"), "guarantee_type": r.text("guarantee_type")})
		if openID := d.integer(r, "open_record_id"); openID > 0 {
			if openItem[openID] != nil {
				d.problem("blind_box_item", id, "duplicate_blind_box_open", "multiple items link one open record")
			}
			openItem[openID] = r
		}
	}
	for _, r := range d.source["blind_box_open_records"] {
		id := r.id()
		item := openItem[id]
		user := d.integer(r, "user_id")
		itemID := item.id()
		if users[user] == nil || (item != nil && d.integer(item, "owner_user_id") != user) {
			d.problem("blind_box_open_record", id, "invalid_blind_box_open_user", "open user differs from inventory owner")
		}
		reward, err := d.reward(r)
		if err != nil {
			d.problem("blind_box_open_record", id, "unsupported_blind_box_reward", err.Error())
		}
		payload, err := json.Marshal(reward)
		if err != nil {
			d.problem("blind_box_open_record", id, "invalid_blind_box_reward", err.Error())
		}
		request := r.text("request_id")
		if request == "" {
			request = fmt.Sprintf("v2-open:%d", id)
		}
		guarantee := item.text("guarantee_type")
		if guarantee == "" {
			guarantee = "none"
		}
		var targetItem any
		if itemID > 0 {
			targetItem = itemID
		}
		poolID := int64(1)
		if r.text("pool_type") == "standard" {
			poolID = 2
		}
		var targetOrder, targetSub any
		if order := d.integer(r, "order_id"); order > 0 {
			targetOrder = order
		}
		if sub := d.integer(r, "user_subscription_id"); sub > 0 {
			targetSub = sub
		}
		d.add("blind_box_open_records", id, map[string]any{"item_id": targetItem, "user_id": user, "created_at": d.instant(r, "create_time", true), "request_id": request, "reward": payload, "guarantee_type": guarantee, "is_pity": r.flag("is_pity"), "order_id": targetOrder, "pool_id": poolID, "pool_type": r.text("pool_type"), "reward_tier": r.text("reward_tier"), "reward_wallet_type": r.text("reward_wallet_type"), "subscription_id": targetSub})
	}
	d.normalizePity()
}

func (d *marketplaceData) poolRewards(id int64) json.RawMessage {
	for _, r := range d.records {
		if r.Table == "blind_box_pools" && r.ID == id {
			if v, ok := r.Fields["rewards"].(json.RawMessage); ok {
				return v
			}
		}
	}
	return json.RawMessage(`[]`)
}
func (d *marketplaceData) poolGuarantees(id int64) json.RawMessage {
	for _, r := range d.records {
		if r.Table == "blind_box_pools" && r.ID == id {
			if v, ok := r.Fields["guarantees"].(json.RawMessage); ok {
				return v
			}
		}
	}
	return json.RawMessage(`{}`)
}

func (d *marketplaceData) reward(r marketplaceSourceRow) (map[string]any, error) {
	kind := r.text("reward_type")
	title := r.text("reward_title")
	if title == "" {
		title = r.text("reward_tier")
	}
	base := map[string]any{"title": title, "weight": int64(1), "legacy_reward_type": kind, "reward_tier": r.text("reward_tier"), "wallet_type": r.text("reward_wallet_type")}
	switch kind {
	case "quota", "claude_quota":
		units := d.integer(r, "credit_amount")
		amount, err := FromV2Units(units)
		if err != nil {
			return base, err
		}
		if units < 0 {
			return base, fmt.Errorf("negative reward credits")
		}
		if units == 0 {
			base["amount_micro"] = d.money(r, "reward_usd", 1000000)
		} else {
			base["amount_micro"] = int64(amount)
		}
		base["kind"] = "credits"
	case "subscription":
		base["kind"] = "subscription"
		plan := int64(0)
		subID := d.integer(r, "user_subscription_id")
		for _, sub := range d.source["user_subscriptions"] {
			if sub.id() == subID {
				plan = d.integer(sub, "plan_id")
				break
			}
		}
		if plan <= 0 {
			return base, fmt.Errorf("subscription reward has no source subscription plan")
		}
		base["plan_id"] = plan
	case "prop":
		if title == "再来一抽" {
			base["kind"] = "extra_draw"
			return base, nil
		}
		openID := r.id()
		if _, item := r["purchase_id"]; item {
			openID = d.integer(r, "open_record_id")
		}
		for _, prop := range d.source["blind_box_props"] {
			if openID > 0 && d.integer(prop, "open_record_id") == openID {
				reward, err := d.propReward(prop)
				for _, key := range []string{"legacy_reward_type", "reward_tier", "wallet_type"} {
					reward[key] = base[key]
				}
				return reward, err
			}
		}
		if named, ok := marketplaceNamedProp(title); ok {
			for _, key := range []string{"legacy_reward_type", "reward_tier", "wallet_type"} {
				named[key] = base[key]
			}
			return named, nil
		}
		return base, fmt.Errorf("prop reward is missing its typed source prop or supported title")
	default:
		return base, fmt.Errorf("unknown reward type %q", kind)
	}
	return base, nil
}

func (d *marketplaceData) normalizePity() {
	opened := make(map[int64]int64)
	for _, r := range d.source["blind_box_open_records"] {
		if p := r.text("pool_type"); p == "unified" || p == "balance_15" {
			opened[d.integer(r, "user_id")]++
		}
	}
	for _, r := range d.source["blind_box_pity_states"] {
		var count int64
		user := d.integer(r, "user_id")
		low := d.integer(r, "consecutive_low_rewards")
		if user <= 0 || low < 0 {
			d.problem("blind_box_pity", r.id(), "invalid_blind_box_pity", "pity requires a positive user and nonnegative counter")
		}
		for _, o := range d.source["blind_box_open_records"] {
			if d.integer(o, "user_id") == user && o.text("pool_type") == "standard" {
				count++
			}
		}
		d.records = append(d.records, marketplaceRecord{"blind_box_pity", user, map[string]any{"user_id": user, "pool_id": int64(2), "legacy_id": r.id(), "opened": count, "small_progress": low, "big_progress": int64(0), "updated_at": d.instant(r, "updated_at", true)}})
	}
	for _, r := range d.source["balance_blind_box_pity_states"] {
		user := d.integer(r, "user_id")
		small, big := d.integer(r, "consecutive_under6_usd"), d.integer(r, "consecutive_under35_usd")
		if _, ok := r["consecutive_under_6_usd"]; ok {
			small = d.integer(r, "consecutive_under_6_usd")
		}
		if _, ok := r["consecutive_under_35_usd"]; ok {
			big = d.integer(r, "consecutive_under_35_usd")
		}
		if user <= 0 || small < 0 || big < 0 {
			d.problem("blind_box_pity", r.id(), "invalid_blind_box_pity", "pity user and counts must be nonnegative")
		}
		d.records = append(d.records, marketplaceRecord{"blind_box_pity", user, map[string]any{"user_id": user, "pool_id": int64(1), "legacy_id": r.id(), "updated_at": d.instant(r, "updated_at", true), "opened": opened[user], "small_progress": small, "big_progress": big}})
	}
}
