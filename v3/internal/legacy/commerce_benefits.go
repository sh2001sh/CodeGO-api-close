package legacy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (d *commerceData) projectRedemption(r commerceRow) (commerceProjection, error) {
	values, err := commerceFields(r, `id:id:i creator_id:user_id:i name:name:s credits:quota:c redeem_type:redeem_type:s plan_id:plan_id:i plan_title:plan_title:s
		blind_box_quantity:blind_box_quantity:i wallet_type:wallet_type:s created_at:created_time:t claimed_at:redeemed_time:T expires_at:expired_time:T`)
	if err != nil {
		return commerceProjection{}, err
	}
	key, err := r.text("key")
	if err != nil || len(key) < 8 || len(key) > 256 {
		return commerceProjection{}, fmt.Errorf("redemption key length is unsupported")
	}
	digest := sha256.Sum256([]byte(key))
	values["code_hash"] = digest[:]
	status, err := r.integer("status")
	if err != nil {
		return commerceProjection{}, err
	}
	switch status {
	case 1:
		values["state"] = "active"
	case 2:
		values["state"] = "revoked"
	case 3:
		values["state"] = "used"
		values["claimed_by"], err = r.integer("used_user_id")
		if err != nil || values["claimed_by"].(int64) <= 0 || values["claimed_at"] == nil {
			return commerceProjection{}, fmt.Errorf("used redemption requires claiming user and timestamp")
		}
	default:
		return commerceProjection{}, fmt.Errorf("unknown redemption status")
	}
	if values["state"] != "used" {
		values["claimed_at"] = nil
	}
	if values["redeem_type"] == "" || values["redeem_type"] == "quota" {
		values["redeem_type"] = "credits"
	}
	switch values["redeem_type"] {
	case "credits":
		if values["credits"].(int64) <= 0 {
			return commerceProjection{}, fmt.Errorf("quota redemption requires positive credits")
		}
	case "subscription":
		if d.plans[values["plan_id"].(int64)] == nil {
			return commerceProjection{}, fmt.Errorf("redemption subscription plan missing")
		}
	case "blind_box":
		if values["blind_box_quantity"].(int64) <= 0 {
			return commerceProjection{}, fmt.Errorf("blind box redemption requires positive quantity")
		}
	default:
		return commerceProjection{}, fmt.Errorf("unknown redemption type")
	}
	if raw := r["deleted_at"]; len(raw) > 0 && string(raw) != "null" {
		var deleted string
		if err = json.Unmarshal(raw, &deleted); err != nil {
			return commerceProjection{}, fmt.Errorf("invalid redemption deletion timestamp")
		}
		stamp, err := time.Parse(time.RFC3339Nano, deleted)
		if err != nil {
			return commerceProjection{}, fmt.Errorf("invalid redemption deletion timestamp")
		}
		values["deleted_at"] = stamp.UTC()
		if values["state"] == "active" {
			values["state"] = "revoked"
		}
	}
	return commerceProjection{"redemption_codes", values, []string{"code_hash"}}, nil
}

func (d *commerceData) projectInvoice(r commerceRow, item bool) (commerceProjection, error) {
	values, err := commerceFields(r, `id:id:i user_id:user_id:i source_type:source_type:s trade_no:trade_no:s currency:currency:s order_title:order_title:s order_amount:order_amount:n`)
	if err != nil {
		return commerceProjection{}, err
	}
	if values["currency"] == "" {
		values["currency"] = "cny"
	}
	values["currency"] = strings.ToLower(values["currency"].(string))
	values["order_amount_minor"], err = commerceMinor(r, "order_amount", values["currency"].(string))
	if err != nil {
		return commerceProjection{}, err
	}
	var specs, table string
	if item {
		table = "invoice_items"
		specs = `invoice_id:invoice_id:i paid_at:paid_at:t`
	} else {
		table = "invoices"
		specs = `order_count:order_count:i invoice_type:invoice_type:s title:title:s tax_number:tax_number:s email:email:s remark:remark:s
			status:status:s invoice_number:invoice_number:s delivery_method:delivery_method:s document_url:document_url:s admin_note:admin_note:s
			handled_by:handled_by:i issued_at:issued_at:T created_at:created_at:t updated_at:updated_at:t`
	}
	more, err := commerceFields(r, specs)
	if err != nil {
		return commerceProjection{}, err
	}
	for field, value := range more {
		values[field] = value
	}
	if !item && values["status"] != "pending" && values["status"] != "issued" && values["status"] != "rejected" {
		return commerceProjection{}, fmt.Errorf("unknown invoice status")
	}
	if values["trade_no"] == "" {
		return commerceProjection{}, fmt.Errorf("invoice order reference required")
	}
	return commerceProjection{table, values, []string{"user_id", "source_type", "trade_no"}}, nil
}
