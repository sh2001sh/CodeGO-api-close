package legacy

func (d *marketplaceData) normalizeGroups() {
	plans := marketplaceIndex(d.source["subscription_plans"])
	groups := d.source["group_buy_orders"]
	if len(groups) == 0 {
		groups = d.source["group_buys"]
	} else if len(d.source["group_buys"]) > 0 {
		d.problem("group_buys", 0, "ambiguous_marketplace_source", "both group_buy_orders and group_buys are populated")
	}
	groupIndex := marketplaceIndex(groups)
	users := marketplaceIndex(d.source["users"])
	orders := marketplaceIndex(d.source["subscription_orders"])
	subs := marketplaceIndex(d.source["user_subscriptions"])
	counts := make(map[int64]int64)
	for _, r := range d.source["group_buy_members"] {
		counts[d.integer(r, "group_buy_id")]++
	}
	for _, r := range groups {
		id := r.id()
		planID := d.integer(r, "plan_id")
		plan := plans[planID]
		if plan == nil {
			d.problem("group_buy", id, "missing_group_plan", "subscription plan does not exist")
		}
		initiator := d.integer(r, "initiator_id")
		if users[initiator] == nil {
			d.problem("group_buy", id, "missing_group_user", "initiator does not exist")
		}
		target, current := d.integer(r, "target_count"), d.integer(r, "current_count")
		if target < 2 || target > 1000 || current < 1 || current > target || current != counts[id] {
			d.problem("group_buy", id, "invalid_group_members", "current_count must equal source memberships and stay within target_count")
		}
		status := r.text("status")
		settled := d.instant(r, "settled_at", false)
		if (status != "pending" && status != "completed" && status != "expired") || (status == "pending") != (settled == nil) {
			d.problem("group_buy", id, "invalid_group_state", "status and settlement timestamp disagree")
		}
		b2, b3, b5 := d.money(plan, "group_buy_bonus2", 1000000), d.money(plan, "group_buy_bonus3", 1000000), d.money(plan, "group_buy_bonus5", 1000000)
		// Historical schema aliases retain their explicit tier fields.
		if _, ok := r["bonus_at_2_micro"]; ok {
			b2 = d.integer(r, "bonus_at_2_micro")
		}
		if _, ok := r["bonus_at_3_micro"]; ok {
			b3 = d.integer(r, "bonus_at_3_micro")
		}
		if _, ok := r["bonus_at_5_micro"]; ok {
			b5 = d.integer(r, "bonus_at_5_micro")
		}
		d.add("group_buys", id, map[string]any{"initiator_id": initiator, "plan_id": planID, "target_count": target, "current_count": current, "bonus_micro": b5, "bonus_at_2_micro": b2, "bonus_at_3_micro": b3, "bonus_at_5_micro": b5, "status": status, "expires_at": d.instant(r, "expires_at", true), "settled_at": settled, "created_at": d.instant(r, "created_at", true), "updated_at": d.instant(r, "updated_at", true)})
	}
	for _, r := range d.source["group_buy_members"] {
		id := r.id()
		gid, user := d.integer(r, "group_buy_id"), d.integer(r, "user_id")
		if groupIndex[gid] == nil || users[user] == nil {
			d.problem("group_buy_member", id, "missing_group_membership_reference", "group or user does not exist")
		}
		orderID, subID := d.integer(r, "order_id"), d.integer(r, "user_subscription_id")
		if orderID < 0 || subID < 0 || (orderID == 0) != (subID == 0) {
			d.problem("group_buy_member", id, "invalid_ghost_membership", "order and subscription must both exist or both be absent")
		}
		if orderID > 0 && (orders[orderID] == nil || d.integer(orders[orderID], "user_id") != user) {
			d.problem("group_buy_member", id, "invalid_group_order", "order does not belong to member")
		}
		if subID > 0 && (subs[subID] == nil || d.integer(subs[subID], "user_id") != user) {
			d.problem("group_buy_member", id, "invalid_group_subscription", "subscription does not belong to member")
		}
		bonus := d.money(r, "bonus_amount_usd", 1000000)
		var targetOrder any = int64(0)
		var targetSub any
		if orderID > 0 {
			if orderID > (9223372036854775807-1)/2 {
				d.problem("group_buy_member", id, "invalid_group_order_id", "order ID remapping overflows int64")
			} else {
				targetOrder = orderID*2 + 1
			}
			targetSub = subID
		}
		d.add("group_buy_members", id, map[string]any{"group_buy_id": gid, "user_id": user, "order_id": targetOrder, "subscription_id": targetSub, "bonus_granted": r.flag("bonus_granted"), "bonus_amount_micro": bonus, "created_at": d.instant(r, "created_at", true)})
	}
}
