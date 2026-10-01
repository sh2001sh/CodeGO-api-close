package identitydto

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

func set(fields map[string]json.RawMessage, key string, value any) {
	fields[key], _ = json.Marshal(value)
}

func keyInput(f map[string]json.RawMessage) error {
	if err := checkMixedKeyVersions(f); err != nil {
		return err
	}
	if _, modern := f["budget_limited"]; !modern {
		set(f, "budget_limited", true)
	}
	if err := normalizeKeyStatus(f); err != nil {
		return err
	}
	unlimited, err := migrateUnlimitedQuota(f)
	if err != nil {
		return err
	}
	if err := migrateRemainQuota(f, unlimited); err != nil {
		return err
	}
	if err := migrateModelLimits(f); err != nil {
		return err
	}
	if err := migrateAllowIPs(f); err != nil {
		return err
	}
	if err := migrateExpiry(f); err != nil {
		return err
	}
	if err := migrateMultiplierLimit(f); err != nil {
		return err
	}
	for _, readonly := range []string{"key", "user_id", "created_time", "accessed_time", "used_quota", "DeletedAt"} {
		delete(f, readonly)
	}
	return nil
}

func checkMixedKeyVersions(f map[string]json.RawMessage) error {
	legacyToModern := map[string]string{
		"unlimited_quota": "budget_limited", "remain_quota": "budget_micro_credits",
		"model_limits_enabled": "allowed_models", "allow_ips": "allowed_cidrs",
		"expired_time": "expires_at", "marketplace_multiplier_limit": "max_marketplace_multiplier_ppm",
	}
	for old, modern := range legacyToModern {
		if f[old] != nil && f[modern] != nil {
			return errors.New("mixed API key policy versions")
		}
	}
	return nil
}

func normalizeKeyStatus(f map[string]json.RawMessage) error {
	status := f["status"]
	if len(status) == 0 || status[0] == '"' {
		return nil
	}
	var code int
	if json.Unmarshal(status, &code) != nil || code < 1 || code > 4 {
		return errors.New("invalid key status")
	}
	name := "disabled"
	if code == 1 {
		name = "active"
	}
	set(f, "status", name)
	return nil
}

func migrateUnlimitedQuota(f map[string]json.RawMessage) (bool, error) {
	var unlimited bool
	if raw, ok := f["unlimited_quota"]; ok {
		if json.Unmarshal(raw, &unlimited) != nil {
			return false, errors.New("invalid unlimited flag")
		}
		set(f, "budget_limited", !unlimited)
		delete(f, "unlimited_quota")
	}
	return unlimited, nil
}

func migrateRemainQuota(f map[string]json.RawMessage, unlimited bool) error {
	raw, ok := f["remain_quota"]
	if !ok {
		return nil
	}
	var remaining int64
	if json.Unmarshal(raw, &remaining) != nil || remaining < 0 || remaining > math.MaxInt64/2 {
		return errors.New("invalid key budget")
	}
	if !unlimited {
		set(f, "budget_micro_credits", remaining*2)
	}
	delete(f, "remain_quota")
	return nil
}

func migrateModelLimits(f map[string]json.RawMessage) error {
	raw, ok := f["model_limits_enabled"]
	if !ok {
		return nil
	}
	var enabled bool
	if json.Unmarshal(raw, &enabled) != nil {
		return errors.New("invalid model flag")
	}
	var models string
	if value := f["model_limits"]; value != nil && json.Unmarshal(value, &models) != nil {
		return errors.New("invalid models")
	}
	var limits []string
	if enabled {
		limits = split(models)
		if limits == nil {
			limits = []string{}
		}
	}
	set(f, "allowed_models", limits)
	delete(f, "model_limits_enabled")
	delete(f, "model_limits")
	return nil
}

func migrateAllowIPs(f map[string]json.RawMessage) error {
	raw, ok := f["allow_ips"]
	if !ok {
		return nil
	}
	var text *string
	if json.Unmarshal(raw, &text) != nil {
		return errors.New("invalid IP restriction")
	}
	var cidrs []string
	if text != nil {
		for _, token := range split(*text) {
			prefix, err := netip.ParsePrefix(token)
			if err != nil {
				address, err := netip.ParseAddr(token)
				if err != nil {
					return err
				}
				prefix = netip.PrefixFrom(address, address.BitLen())
			}
			cidrs = append(cidrs, prefix.Masked().String())
		}
	}
	set(f, "allowed_cidrs", cidrs)
	delete(f, "allow_ips")
	return nil
}

func migrateExpiry(f map[string]json.RawMessage) error {
	raw, ok := f["expired_time"]
	if !ok {
		return nil
	}
	var expiry int64
	if json.Unmarshal(raw, &expiry) != nil || expiry < -1 || expiry > 253402300799 {
		return errors.New("invalid expiry")
	}
	var at *time.Time
	if expiry > 0 {
		value := time.Unix(expiry, 0).UTC()
		at = &value
	}
	set(f, "expires_at", at)
	delete(f, "expired_time")
	return nil
}

func migrateMultiplierLimit(f map[string]json.RawMessage) error {
	raw, ok := f["marketplace_multiplier_limit"]
	if !ok {
		return nil
	}
	ratio, valid := new(big.Rat).SetString(string(raw))
	if !valid || ratio.Sign() < 0 {
		return errors.New("invalid multiplier")
	}
	ratio.Mul(ratio, big.NewRat(1000000, 1))
	if !ratio.IsInt() || !ratio.Num().IsInt64() {
		return errors.New("multiplier cannot be represented exactly")
	}
	set(f, "max_marketplace_multiplier_ppm", ratio.Num().Int64())
	delete(f, "marketplace_multiplier_limit")
	return nil
}

func split(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t' })
}

func keyOutput(raw json.RawMessage) (json.RawMessage, error) {
	var f map[string]json.RawMessage
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	var name string
	if json.Unmarshal(f["status"], &name) != nil {
		return raw, nil
	}
	code := 2
	if name == "active" {
		code = 1
	}
	limited, units, err := outputBudgetFields(f)
	if err != nil {
		return nil, err
	}
	if code == 1 && limited && units <= 0 {
		code = 4
	}
	if err := outputTimestampFields(f, &code); err != nil {
		return nil, err
	}
	set(f, "status", code)
	if err := outputModelAndIPFields(f); err != nil {
		return nil, err
	}
	outputMultiplierAndKeyFields(f)
	legacyKeys := []string{"allowed_models", "allowed_cidrs", "expires_at", "key_prefix", "created_at", "last_used_at",
		"budget_limited", "budget_micro_credits", "max_marketplace_multiplier_ppm", "budget_account_id", "spent_micro_credits"}
	for _, modern := range legacyKeys {
		delete(f, modern)
	}
	return json.Marshal(f)
}

// outputBudgetFields fills in the legacy unlimited_quota/remain_quota/
// used_quota fields and returns whether the budget is limited and how many
// legacy units remain, so the caller can derive the exhausted-budget status.
func outputBudgetFields(f map[string]json.RawMessage) (limited bool, units int64, err error) {
	_ = json.Unmarshal(f["budget_limited"], &limited)
	var remaining *int64
	if value := f["budget_micro_credits"]; value != nil {
		if err := json.Unmarshal(value, &remaining); err != nil {
			return false, 0, err
		}
	}
	set(f, "unlimited_quota", !limited)
	if remaining != nil {
		units = *remaining / 2
	}
	set(f, "remain_quota", units)
	var spent int64
	if value := f["spent_micro_credits"]; value != nil {
		if err := json.Unmarshal(value, &spent); err != nil {
			return false, 0, err
		}
	}
	set(f, "used_quota", spent/2)
	return limited, units, nil
}

// outputTimestampFields migrates created_at/last_used_at/expires_at into
// their legacy Unix-seconds fields. *code is downgraded to 3 (expired) if
// the key has passed its expiry and no earlier rule already changed it.
func outputTimestampFields(f map[string]json.RawMessage, code *int) error {
	modernByLegacy := map[string]string{"created_time": "created_at", "accessed_time": "last_used_at", "expired_time": "expires_at"}
	for old, modern := range modernByLegacy {
		var at *time.Time
		if value := f[modern]; value != nil {
			if err := json.Unmarshal(value, &at); err != nil {
				return err
			}
		}
		stamp := int64(0)
		if old == "expired_time" {
			stamp = -1
		}
		if at != nil {
			stamp = at.Unix()
		}
		set(f, old, stamp)
		if old == "expired_time" && stamp > 0 && stamp <= time.Now().Unix() && *code == 1 {
			*code = 3
		}
	}
	return nil
}

func outputModelAndIPFields(f map[string]json.RawMessage) error {
	var models, cidrs []string
	_ = json.Unmarshal(f["allowed_models"], &models)
	_ = json.Unmarshal(f["allowed_cidrs"], &cidrs)
	if cidrs != nil && len(cidrs) == 0 {
		return errors.New("legacy CIDR DTO cannot represent deny-all; request API version 3")
	}
	set(f, "model_limits_enabled", models != nil)
	set(f, "model_limits", strings.Join(models, ","))
	set(f, "allow_ips", strings.Join(cidrs, "\n"))
	return nil
}

func outputMultiplierAndKeyFields(f map[string]json.RawMessage) {
	var ppm int64
	_ = json.Unmarshal(f["max_marketplace_multiplier_ppm"], &ppm)
	f["marketplace_multiplier_limit"] = json.RawMessage(strconv.FormatInt(ppm/1000000, 10) + "." + padPPM(ppm%1000000))
	var prefix string
	if err := json.Unmarshal(f["key_prefix"], &prefix); err != nil {
		prefix = ""
	}
	set(f, "key", prefix+"**********") // Raw key disclosure remains confined to /{id}/key.
	if string(f["group"]) == "null" {
		set(f, "group", "")
	}
}

func padPPM(value int64) string {
	text := strconv.FormatInt(value, 10)
	return strings.Repeat("0", 6-len(text)) + text
}
