package legacy

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

func marketplaceCashRequest(trade string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(trade)))
	return fmt.Sprintf("cash:%x", digest[:24])
}
func (d *marketplaceData) externalOrderForPurchase(request string) int64 {
	for _, r := range d.source["blind_box_orders"] {
		if marketplaceCashRequest(r.text("trade_no")) == request {
			return r.id()
		}
	}
	return 0
}
func (d *marketplaceData) hasOrderInventory(order marketplaceSourceRow) bool {
	for _, r := range d.source["balance_blind_box_purchases"] {
		if r.text("request_id") == marketplaceCashRequest(order.text("trade_no")) {
			return true
		}
	}
	return false
}

// External orders that already issued cash:<trade hash> balance inventory must
// never allocate it again. Only remaining legacy stock without that issuance is
// synthesized; negative IDs cannot collide with any positive original v2 ID.
func (d *marketplaceData) normalizeBoxOrderInventory() {
	for _, r := range d.source["blind_box_orders"] {
		if r.text("status") != "success" && r.text("status") != "completed" {
			continue
		}
		if d.hasOrderInventory(r) {
			continue
		}
		quantity, opened := d.integer(r, "quantity"), d.integer(r, "opened_count")
		remaining := quantity - opened
		if remaining <= 0 {
			continue
		}
		if quantity > 1000000 {
			d.problem("blind_box_order", r.id(), "excessive_external_inventory", "remaining external stock exceeds one million items per order; explicit source review required")
			continue
		}
		if r.id() <= 0 || r.id() > 2147483647 || quantity > 2147483647 {
			d.problem("blind_box_order", r.id(), "invalid_external_inventory_id", "synthetic inventory requires original int32 order and quantity IDs")
			continue
		}
		created := d.instant(r, "create_time", true)
		if created == nil {
			continue
		}
		user := d.integer(r, "user_id")
		purchaseID := -r.id()
		d.add("blind_box_purchases", purchaseID, map[string]any{"user_id": user, "pool_id": int64(2), "quantity": quantity, "unit_price_micro": int64(0), "total_micro": int64(0), "is_grant": r.text("source") != "purchase", "status": "completed", "purchase_date": created.In(time.FixedZone("UTC+8", 8*3600)).Format("2006-01-02"), "created_at": created, "request_id": fmt.Sprintf("v2-order:%d", r.id()), "external_order_id": r.id()})
		for i := opened; i < quantity; i++ {
			id := -(r.id()*2147483648 + i + 1)
			d.add("blind_box_items", id, map[string]any{"purchase_id": purchaseID, "owner_user_id": user, "purchase_user_id": user, "pool_id": int64(2), "rewards": d.poolRewards(2), "guarantees": d.poolGuarantees(2), "draw_current_pool": true, "frozen_reward": nil, "status": "available", "opened_at": nil, "created_at": created, "updated_at": created, "expires_at": d.instant(r, "expires_at", false), "pool_version": "v2-standard-order", "reward_tier": "", "reward_wallet_type": "", "guarantee_type": "none"})
		}
	}
}
