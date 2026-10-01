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

// The keys are v2 table names. Values in sources are already SQL-quoted by
// discovery; no source names or row contents are interpolated into SQL here.
var commerceSourceNames = []string{
	"subscription_plans", "user_subscriptions", "subscription_orders", "top_ups",
	"redemptions", "wallet_transfers", "wallet_transfer_securities", "invoice_requests",
	"invoice_request_items", "subscription_pre_consume_records",
}

type commerceRow map[string]json.RawMessage

type commerceData struct {
	rows              map[string][]commerceRow
	plans             map[int64]commerceRow
	users             map[int64]bool
	options           map[string]string
	snapshot          map[int64][2]int64
	reservationStates map[string][]string
	refundOrigins     []commerceRefundOrigin
	completedRequests map[string]int64
	renewableBonuses  map[int64]*big.Int
	resetUsed         map[int64]bool
}

func loadCommerce(ctx context.Context, source pgx.Tx, sources map[string]string) (*commerceData, error) {
	d := &commerceData{rows: map[string][]commerceRow{}, plans: map[int64]commerceRow{}, users: map[int64]bool{}, snapshot: map[int64][2]int64{}, reservationStates: map[string][]string{}}
	for _, name := range append(append([]string{}, commerceSourceNames...), "users") {
		raw, err := loadRows(ctx, source, sources[name])
		if err != nil {
			return nil, fmt.Errorf("legacy: load commerce table %s: %w", name, err)
		}
		for _, encoded := range raw {
			row := commerceRow{}
			if err = json.Unmarshal(encoded, &row); err != nil {
				return nil, fmt.Errorf("legacy: decode commerce table %s: %w", name, err)
			}
			d.rows[name] = append(d.rows[name], row)
			if name == "subscription_plans" {
				id, _ := row.integer("id")
				d.plans[id] = row
			}
			if name == "users" {
				id, _ := row.integer("id")
				d.users[id] = true
			}
		}
	}
	var err error
	d.options, err = loadOptions(ctx, source, sources["options"])
	if err != nil {
		return nil, err
	}
	if err = d.loadResetUsage(ctx, source, sources); err != nil {
		return nil, err
	}
	if err = d.loadCompletedRequests(ctx, source, sources); err != nil {
		return nil, err
	}
	if err = d.loadRenewableBonuses(ctx, source, sources); err != nil {
		return nil, err
	}
	reservationTable := sources["billing_reservations"]
	if reservationTable == "" {
		reservationTable = sources["reservations"]
	}
	if reservationTable != "" {
		reservationRows, err := loadRows(ctx, source, reservationTable)
		if err != nil {
			return nil, err
		}
		for _, raw := range reservationRows {
			var row struct {
				RequestID string `json:"request_id"`
				Status    string `json:"status"`
			}
			if err = json.Unmarshal(raw, &row); err != nil {
				return nil, err
			}
			d.reservationStates[row.RequestID] = append(d.reservationStates[row.RequestID], row.Status)
		}
	}
	if sources["accounts"] == "" || sources["balance_snapshots"] == "" {
		return d, d.loadRefundOrigins(ctx, source, sources)
	}
	rows, err := source.Query(ctx, `SELECT a.owner_id,s.available_balance,s.reserved_balance FROM `+sources["accounts"]+` a
		JOIN `+sources["balance_snapshots"]+` s ON s.account_id=a.account_id
		WHERE a.owner_type='user_subscription' AND a.account_type='subscription' AND a.quota_unit='quota'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var value [2]int64
		if err = rows.Scan(&id, &value[0], &value[1]); err != nil {
			return nil, err
		}
		if _, exists := d.snapshot[id]; exists {
			return nil, fmt.Errorf("legacy: duplicate canonical subscription account %d", id)
		}
		d.snapshot[id] = value
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return d, d.loadRefundOrigins(ctx, source, sources)
}
func (r commerceRow) text(key string) (string, error) {
	value := r[key]
	if len(value) == 0 || string(value) == "null" {
		return "", nil
	}
	var out string
	if err := json.Unmarshal(value, &out); err != nil {
		return "", fmt.Errorf("%s must be text", key)
	}
	return out, nil
}
func (r commerceRow) integer(key string) (int64, error) {
	value := r[key]
	if len(value) == 0 || string(value) == "null" {
		return 0, nil
	}
	out, err := strconv.ParseInt(string(value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a bigint", key)
	}
	return out, nil
}
func (r commerceRow) decimal(key string) (string, error) {
	value := r[key]
	if len(value) == 0 || string(value) == "null" {
		return "0", nil
	}
	out := string(value)
	if value[0] == '"' {
		if err := json.Unmarshal(value, &out); err != nil {
			return "", fmt.Errorf("%s must be numeric", key)
		}
	}
	number, ok := new(big.Rat).SetString(out)
	if !ok || number.Sign() < 0 {
		return "", fmt.Errorf("%s must be a nonnegative decimal", key)
	}
	return out, nil
}

func (r commerceRow) boolean(key string) (bool, error) {
	value := r[key]
	if len(value) == 0 || string(value) == "null" {
		return false, nil
	}
	var out bool
	if err := json.Unmarshal(value, &out); err != nil {
		return false, fmt.Errorf("%s must be boolean", key)
	}
	return out, nil
}

func (r commerceRow) epoch(key string, nullable bool) (any, error) {
	n, err := r.integer(key)
	if err != nil {
		return nil, err
	}
	if n < 0 || n > 253402300799 {
		return nil, fmt.Errorf("%s is outside supported timestamp range", key)
	}
	if nullable && n == 0 {
		return nil, nil
	}
	return time.Unix(n, 0).UTC(), nil
}

func commerceUnits(r commerceRow, field string) (int64, error) {
	n, err := r.integer(field)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fmt.Errorf("%s must be nonnegative", field)
	}
	value, err := FromV2Units(n)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", field, err)
	}
	return int64(value), nil
}

// Per-model limits and usage are typed credit maps, not a raw source archive.
func commerceModelMap(r commerceRow, field string) (string, error) {
	text, err := r.text(field)
	if err != nil {
		return "", err
	}
	result := map[string]int64{}
	if text != "" {
		if err = json.Unmarshal([]byte(text), &result); err != nil {
			return "", fmt.Errorf("%s must be a bigint model map", field)
		}
		normalized := map[string]int64{}
		for model, amount := range result {
			model = strings.TrimSpace(model)
			if model == "" || amount < 0 {
				return "", fmt.Errorf("%s has invalid model or amount", field)
			}
			if _, duplicate := normalized[model]; duplicate {
				return "", fmt.Errorf("%s has ambiguous model names after trimming", field)
			}
			converted, err := FromV2Units(amount)
			if err != nil {
				return "", fmt.Errorf("%s: %w", field, err)
			}
			normalized[model] = int64(converted)
		}
		result = normalized
	}
	encoded, err := json.Marshal(result)
	return string(encoded), err
}

func commerceDuration(r commerceRow) (int64, error) {
	unit, err := r.text("duration_unit")
	if err != nil {
		return 0, err
	}
	n, err := r.integer("duration_value")
	if err != nil {
		return 0, err
	}
	if unit == "" {
		unit = "month"
	}
	if n == 0 {
		n = 1
	}
	scale := int64(0)
	switch unit {
	case "year":
		scale = 365 * 86400
	case "month":
		scale = 30 * 86400
	case "day":
		scale = 86400
	case "hour":
		scale = 3600
	case "custom":
		return r.integer("custom_seconds")
	default:
		return 0, fmt.Errorf("unsupported duration_unit")
	}
	if n < 0 || n > math.MaxInt64/scale {
		return 0, fmt.Errorf("duration overflows")
	}
	return n * scale, nil
}

func commerceOrderID(id int64, kind string) (int64, error) {
	// Both old tables start at 1. Parity preserves their IDs without collisions.
	if id <= 0 || id > (math.MaxInt64-1)/2 {
		return 0, fmt.Errorf("order ID cannot fit deterministic target mapping")
	}
	if kind == "subscription" {
		return 2*id + 1, nil
	}
	return 2 * id, nil
}

// Currency determines the payment minor unit, never the credit conversion.
func commerceMinor(r commerceRow, field, currency string) (int64, error) {
	value, err := r.decimal(field)
	if err != nil {
		return 0, err
	}
	scale := "100"
	switch strings.ToLower(currency) {
	case "bif", "clp", "djf", "gnf", "jpy", "kmf", "krw", "mga", "pyg", "rwf", "ugx", "vnd", "vuv", "xaf", "xof", "xpf":
		scale = "1"
	case "bhd", "jod", "kwd", "omr", "tnd":
		scale = "1000"
	case "usdt", "usdc":
		scale = "1000000"
	}
	return decimalProduct(value, scale)
}
