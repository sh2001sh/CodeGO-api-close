package legacy

import (
	"fmt"
	"time"
)

type fundingProjection struct {
	table, key string
	values     map[string]any
	account    historyAccount
	remaining  int64
}

func (d *fundingData) projectFunding(name string, row commerceRow) (fundingProjection, error) {
	p := fundingProjection{table: name, values: map[string]any{}}
	var text, required, amounts, decimals, timestamps []string
	switch name {
	case "funding_source_policies":
		p.key, required = "source", []string{"source"}
		decimals, timestamps = []string{"revenue_multiplier"}, []string{"updated_at"}
	case "funding_lots":
		p.key = "lot_id"
		required = []string{"lot_id", "account_id", "source", "idempotency_key"}
		text, amounts = []string{"reference_type", "reference_id"}, []string{"original_amount", "remaining_amount"}
		decimals, timestamps = []string{"revenue_multiplier"}, []string{"created_at"}
	case "funding_allocations":
		p.key = "allocation_id"
		required = []string{"allocation_id", "request_id", "lot_id", "account_id", "source"}
		amounts, decimals, timestamps = []string{"amount"}, []string{"revenue_multiplier"}, []string{"created_at"}
	case "wallet_reward_holds":
		p.key = "hold_id"
		required = []string{"hold_id", "account_id", "idempotency_key"}
		text, amounts, timestamps = []string{"reference_type", "reference_id"}, []string{"original_amount", "consumed_amount"}, []string{"created_at"}
	case "request_economics":
		p.key, required = "request_id", []string{"request_id", "billing_source"}
		amounts, decimals = []string{"actual_amount"}, []string{"procurement_cost_multiplier", "revenue_multiplier"}
		timestamps = []string{"settled_at", "created_at"}
	default:
		return p, fmt.Errorf("unsupported funding source table")
	}
	for _, field := range append(required, text...) {
		value, err := fundingText(row, field, containsFunding(required, field))
		if err != nil {
			return p, err
		}
		column := field
		if field == "account_id" {
			column = "source_account_id"
			account, exists := d.accounts[value]
			if !exists {
				return p, fmt.Errorf("account_id references an absent source billing account")
			}
			if account.Unit != "quota" {
				return p, fmt.Errorf("funding account must use quota units")
			}
			p.account = account
		}
		p.values[column] = value
	}
	if source, exists := p.values["source"]; exists && !fundingKind(source.(string)) {
		return p, fmt.Errorf("unsupported funding source kind")
	}
	for _, field := range amounts {
		value, err := commerceUnits(row, field)
		if err != nil {
			return p, err
		}
		p.values[field] = value
	}
	for _, field := range decimals {
		value, err := fundingPPM(row, field)
		if err != nil {
			return p, err
		}
		p.values[field+"_ppm"] = value
	}
	for _, field := range timestamps {
		value, err := fundingTime(row, field)
		if err != nil {
			return p, err
		}
		p.values[field] = value
	}
	switch name {
	case "funding_lots":
		original, remaining := p.values["original_amount"].(int64), p.values["remaining_amount"].(int64)
		if original <= 0 || remaining > original {
			return p, fmt.Errorf("funding lot amounts require 0<=remaining<=original and original>0")
		}
		p.remaining = remaining
		_, kind := historicalAccountMapping(p.account)
		if remaining > 0 && kind == "" {
			return p, fmt.Errorf("nonempty funding lot has no native live account mapping")
		}
		if remaining > 0 && p.account.OwnerType == "user" {
			if _, exists := d.users[p.account.OwnerID]; !exists {
				return p, fmt.Errorf("nonempty funding lot references an absent wallet owner")
			}
		}
	case "funding_allocations":
		if p.values["amount"].(int64) <= 0 {
			return p, fmt.Errorf("allocation amount must be positive")
		}
	case "wallet_reward_holds":
		if err := d.projectFundingHold(row, &p); err != nil {
			return p, err
		}
	case "request_economics":
		if err := projectFundingEconomics(row, &p); err != nil {
			return p, err
		}
	}
	return p, nil
}

func containsFunding(values []string, key string) bool {
	for _, value := range values {
		if value == key {
			return true
		}
	}
	return false
}

func (d *fundingData) projectFundingHold(row commerceRow, p *fundingProjection) error {
	original, consumed := p.values["original_amount"].(int64), p.values["consumed_amount"].(int64)
	if original <= 0 || consumed > original {
		return fmt.Errorf("reward hold amounts require 0<=consumed<=original and original>0")
	}
	uid, err := row.integer("user_id")
	if err != nil {
		return err
	}
	user, exists := d.users[uid]
	if !exists || uid <= 0 {
		return fmt.Errorf("reward hold references an absent source user")
	}
	if p.account.OwnerType != "user" || p.account.OwnerID != uid || p.account.Kind != "claude_wallet" {
		return fmt.Errorf("reward hold account must be the owner's canonical wallet")
	}
	if user.CreatedAt > 253402300799 {
		return fmt.Errorf("reward owner age is outside supported timestamp range")
	}
	p.values["user_id"] = uid
	p.values["user_created_at"] = nil
	if user.CreatedAt > 0 {
		p.values["user_created_at"] = time.Unix(user.CreatedAt, 0).UTC()
	}
	p.remaining = original - consumed
	return nil
}

func projectFundingEconomics(row commerceRow, p *fundingProjection) error {
	for _, field := range []string{"channel_id", "route_pool_id", "subscription_id"} {
		value, err := row.integer(field)
		if err != nil || value < 0 {
			return fmt.Errorf("%s must be a nonnegative source ID", field)
		}
		p.values[field] = value
	}
	switch p.values["billing_source"].(string) {
	case "wallet", "subscription":
	default:
		return fmt.Errorf("unsupported billing source in request economics")
	}
	return nil
}
