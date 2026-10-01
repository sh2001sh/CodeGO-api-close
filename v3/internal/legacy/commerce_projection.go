package legacy

import (
	"fmt"
	"math"
	"strings"
)

type commerceProjection struct {
	table    string
	values   map[string]any
	identity []string
}

// Types: s text; i integer; b boolean; n exact numeric; c v2 credit units;
// j per-model credit map; t required epoch; T nullable epoch.
func commerceFields(r commerceRow, specs string) (map[string]any, error) {
	out := map[string]any{}
	for _, spec := range strings.Fields(specs) {
		parts := strings.Split(spec, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("legacy: invalid commerce field specification")
		}
		target, source, kind := parts[0], parts[1], parts[2]
		var value any
		var err error
		switch kind {
		case "s":
			value, err = r.text(source)
		case "i":
			value, err = r.integer(source)
		case "b":
			value, err = r.boolean(source)
		case "n":
			value, err = r.decimal(source)
		case "c":
			value, err = commerceUnits(r, source)
		case "j":
			value, err = commerceModelMap(r, source)
		case "t", "T":
			value, err = r.epoch(source, kind == "T")
		default:
			err = fmt.Errorf("legacy: invalid commerce field type")
		}
		if err != nil {
			return nil, err
		}
		out[target] = value
	}
	return out, nil
}

func (d *commerceData) project(name string, r commerceRow) (commerceProjection, error) {
	id, err := r.integer("id")
	if err != nil || (id <= 0 && name != "wallet_transfer_securities") {
		return commerceProjection{}, fmt.Errorf("positive ID required")
	}
	switch name {
	case "subscription_plans":
		return d.projectPlan(r)
	case "user_subscriptions":
		return d.projectSubscription(r)
	case "subscription_orders", "top_ups":
		return d.projectOrder(name, r)
	case "redemptions":
		return d.projectRedemption(r)
	case "wallet_transfers":
		values, err := commerceFields(r, `id:id:i request_id:request_id:s sender_user_id:sender_user_id:i recipient_user_id:recipient_user_id:i
			sender_external_id:sender_external_id:s recipient_external_id:recipient_external_id:s sender_display_name_masked:sender_display_name_masked:s
			recipient_display_name_masked:recipient_display_name_masked:s amount:amount_quota:c fee:fee_quota:c total_debit:total_debit_quota:c
			sender_balance_after:sender_balance_after:c recipient_balance_after:recipient_balance_after:c status:status:s created_at:created_at:t`)
		if err == nil && (values["status"] != "completed" || values["amount"].(int64) <= 0 ||
			values["amount"].(int64) > math.MaxInt64-values["fee"].(int64) || values["amount"].(int64)+values["fee"].(int64) != values["total_debit"].(int64)) {
			err = fmt.Errorf("completed transfer and amount+fee=total_debit required")
		}
		return commerceProjection{"wallet_transfers", values, []string{"request_id", "sender_user_id", "recipient_user_id"}}, err
	case "wallet_transfer_securities":
		values, err := commerceFields(r, `user_id:user_id:i password_hash:password_hash:s failed_attempts:failed_attempts:i locked_until:locked_until:T created_at:created_at:t updated_at:updated_at:t`)
		if err == nil && (values["user_id"].(int64) <= 0 || values["password_hash"] == "" || values["failed_attempts"].(int64) < 0) {
			err = fmt.Errorf("valid transfer security user, hash and failure count required")
		}
		return commerceProjection{"wallet_transfer_security", values, []string{"user_id"}}, err
	case "invoice_requests":
		return d.projectInvoice(r, false)
	case "invoice_request_items":
		return d.projectInvoice(r, true)
	case "subscription_pre_consume_records":
		values, err := commerceFields(r, `id:id:i request_id:request_id:s user_id:user_id:i subscription_id:user_subscription_id:i model_name:model_name:s amount:pre_consumed:c status:status:s created_at:created_at:t updated_at:updated_at:t`)
		if err == nil {
			// Successful v2 settlement leaves the record "consumed". Its durable
			// reservation, rather than this projection, determines completion.
			// A reservation cannot be
			// imported while in flight, even if the wallet snapshot is zero-reserved.
			switch values["status"] {
			case "refunded", "settled", "completed":
			case "consumed":
				states := d.reservationStates[values["request_id"].(string)]
				if len(states) == 0 && d.completedRequests[values["request_id"].(string)] != values["user_id"].(int64) {
					err = fmt.Errorf("consumed subscription record has no durable settlement evidence")
				}
				for _, state := range states {
					if state != "settled" {
						err = fmt.Errorf("nonterminal subscription reservation; drain v2 requests before import")
					}
				}
			default:
				err = fmt.Errorf("nonterminal subscription preconsume; drain v2 requests before import")
			}
		}
		return commerceProjection{"subscription_preconsumes", values, []string{"request_id", "subscription_id"}}, err
	}
	return commerceProjection{}, fmt.Errorf("unsupported commerce table")
}
