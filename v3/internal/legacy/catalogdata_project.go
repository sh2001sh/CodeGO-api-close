package legacy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"
)

func (d *catalogData) project(name string, row commerceRow) (catalogDataRecord, error) {
	id, err := row.integer("id")
	if err != nil || id <= 0 {
		return catalogDataRecord{}, fmt.Errorf("positive catalog ID required")
	}
	specs := map[string]string{
		"vendors":            `id:id:i name:name:s description:description:s icon:icon:s status:status:i created_at:created_time:t updated_at:updated_time:t`,
		"models":             `id:id:i model_name:model_name:s description:description:s icon:icon:s tags:tags:s endpoints:endpoints:s vendor_id:vendor_id:i status:status:i sync_official:sync_official:i name_rule:name_rule:i created_at:created_time:t updated_at:updated_time:t`,
		"prefill_groups":     `id:id:i name:name:s type:type:s description:description:s created_at:created_time:t updated_at:updated_time:t`,
		"route_pools":        `id:id:i name:name:s group_name:group:s enabled:enabled:b model:model_scope:s auto_discover:auto_discover:b multiplier_weight:multiplier_weight:i ttft_weight:ttft_weight:i cache_weight:cache_weight:i success_weight:success_weight:i`,
		"route_pool_members": `legacy_id:id:i pool_id:route_pool_id:i channel_id:channel_id:i cost_multiplier:cost_multiplier:n fault_domain:fault_domain:s enabled:enabled:b`,
	}
	if specs[name] == "" {
		return catalogDataRecord{}, fmt.Errorf("unknown catalog source")
	}
	fields, err := commerceFields(row, specs[name])
	if err != nil {
		return catalogDataRecord{}, err
	}
	fields["deleted_at"], err = catalogDataDeleted(row["deleted_at"])
	if err != nil {
		return catalogDataRecord{}, err
	}
	if name == "models" {
		vendor := fields["vendor_id"].(int64)
		if vendor == 0 {
			fields["vendor_id"] = nil
		} else if vendor < 0 {
			return catalogDataRecord{}, fmt.Errorf("vendor ID must be nonnegative")
		}
		if strings.TrimSpace(fields["model_name"].(string)) == "" || fields["name_rule"].(int64) < 0 || fields["name_rule"].(int64) > 3 {
			return catalogDataRecord{}, fmt.Errorf("model name and supported name rule required")
		}
	} else if name != "route_pool_members" && strings.TrimSpace(fields["name"].(string)) == "" {
		return catalogDataRecord{}, fmt.Errorf("catalog name required")
	}
	for _, field := range []string{"status", "sync_official"} {
		if n, exists := fields[field].(int64); exists && (n < 0 || n > math.MaxInt32) {
			return catalogDataRecord{}, fmt.Errorf("catalog status is outside integer range")
		}
	}
	key := "id"
	switch name {
	case "prefill_groups":
		fields["items"], err = catalogDataItems(row, fields["type"].(string))
	case "route_pools":
		fields["strategy"] = "scored"
		fields["model_scope"] = fields["model"]
		if strings.TrimSpace(fields["group_name"].(string)) == "" {
			return catalogDataRecord{}, fmt.Errorf("route pool group required")
		}
		if fields["model"] == "" {
			fields["model"] = "*"
		}
		for _, weight := range []string{"multiplier_weight", "ttft_weight", "cache_weight", "success_weight"} {
			if fields[weight].(int64) < 0 || fields[weight].(int64) > 100 {
				return catalogDataRecord{}, fmt.Errorf("pool scoring weight must be in [0,100]")
			}
		}
	case "route_pool_members":
		key = "legacy_id"
		if fields["pool_id"].(int64) <= 0 || fields["channel_id"].(int64) <= 0 {
			return catalogDataRecord{}, fmt.Errorf("positive pool and channel references required")
		}
		number, ok := new(big.Rat).SetString(fields["cost_multiplier"].(string))
		if !ok || number.Sign() <= 0 {
			return catalogDataRecord{}, fmt.Errorf("positive exact member cost required")
		}
		fields["model_cost_overrides"], err = catalogDataCostMap(row)
		if err != nil {
			break
		}
		channel := d.channels[fields["channel_id"].(int64)]
		priority, priorityErr := channel.integer("priority")
		weight, weightErr := channel.integer("weight")
		if priorityErr != nil || weightErr != nil || priority < math.MinInt32 || priority > math.MaxInt32 || weight < 0 || weight > math.MaxInt32 {
			return catalogDataRecord{}, fmt.Errorf("channel priority or weight is invalid")
		}
		fields["priority"], fields["weight"] = priority, max(weight, int64(1))
	}
	return catalogDataRecord{name, key, id, fields}, err
}

func catalogDataDeleted(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return nil, fmt.Errorf("deleted_at must be a timestamp")
	}
	date, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || date.Year() < 1 || date.Year() > 9999 {
		return nil, fmt.Errorf("invalid catalog deletion timestamp")
	}
	return date.UTC(), nil
}

func catalogDataCostMap(row commerceRow) (json.RawMessage, error) {
	encoded, err := row.text("model_cost_overrides")
	if err != nil {
		return nil, err
	}
	if encoded == "" {
		encoded = "{}"
	}
	var values map[string]json.RawMessage
	if json.Unmarshal([]byte(encoded), &values) != nil || values == nil {
		return nil, fmt.Errorf("model cost overrides must be a numeric object")
	}
	for model, value := range values {
		number, ok := new(big.Rat).SetString(string(value))
		if strings.TrimSpace(model) == "" || !ok || number.Sign() <= 0 {
			return nil, fmt.Errorf("model cost override requires model and positive exact number")
		}
	}
	return json.RawMessage(encoded), nil
}

func catalogDataItems(row commerceRow, kind string) (json.RawMessage, error) {
	raw := bytes.TrimSpace(row["items"])
	if len(raw) == 0 || string(raw) == "null" {
		if kind == "endpoint" {
			return json.RawMessage(`{}`), nil
		}
		raw = json.RawMessage(`[]`)
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		raw = bytes.TrimSpace(json.RawMessage(text))
		if len(raw) == 0 {
			if kind == "endpoint" {
				raw = json.RawMessage(`{}`)
			} else {
				raw = json.RawMessage(`[]`)
			}
		}
		if kind != "endpoint" && !json.Valid(raw) {
			items := strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == '\n' })
			for i := range items {
				items[i] = strings.TrimSpace(items[i])
			}
			raw, _ = json.Marshal(items)
		}
	}
	switch kind {
	case "model", "tag":
		var items []string
		if json.Unmarshal(raw, &items) != nil || items == nil {
			return nil, fmt.Errorf("model/tag prefill items must be a text list")
		}
		for _, item := range items {
			if strings.TrimSpace(item) == "" {
				return nil, fmt.Errorf("prefill items must not contain empty names")
			}
		}
	case "endpoint":
		if !json.Valid(raw) || len(raw) == 0 || (raw[0] != '[' && raw[0] != '{') {
			return nil, fmt.Errorf("endpoint prefill items must be a JSON object or list")
		}
	default:
		return nil, fmt.Errorf("unsupported prefill type")
	}
	return raw, nil
}
