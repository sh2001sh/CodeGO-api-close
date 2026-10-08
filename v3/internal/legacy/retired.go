package legacy

import (
	"context"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Explicitly retired features stay in the read-only source. Their original
// units are evidence of exclusion, not an opening monetary credit.
func reportRetiredSources(ctx context.Context, source pgx.Tx, sources map[string]string, report *Report) error {
	fields := map[string][]string{
		"point_accounts":         {"balance", "frozen_balance"},
		"point_ledgers":          {"delta"},
		"bonus_quota_credits":    {"original_amount", "remaining_amount"},
		"user_wechat_bindings":   nil,
		"miniprogram_bind_codes": nil,
	}
	for name := range sources {
		if strings.HasPrefix(name, "pet_") || strings.HasPrefix(name, "user_pet") || name == "pets" {
			fields[name] = nil
		}
	}
	seen := map[string]bool{}
	for name, amounts := range fields {
		table := sources[name]
		if table == "" || seen[table] {
			continue
		}
		seen[table] = true
		var count int64
		if err := source.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			return err
		}
		report.Counts["retired_features."+name] = count
		for _, field := range amounts {
			var total string
			var malformed int64
			if err := source.QueryRow(ctx, `SELECT coalesce(sum(CASE WHEN to_jsonb(t)->>$1 ~ '^-?[0-9]+$'
				THEN (to_jsonb(t)->>$1)::numeric END),0)::text,
				count(*) FILTER(WHERE to_jsonb(t)->>$1 IS NOT NULL AND NOT(to_jsonb(t)->>$1 ~ '^-?[0-9]+$')) FROM `+table+" t", field).Scan(&total, &malformed); err != nil {
				return err
			}
			report.Amounts["retired_features."+name+"."+field+"_v2_units"] = total
			if malformed > 0 {
				report.Counts["retired_features."+name+"."+field+"_unparseable"] = malformed
			}
		}
	}
	return nil
}

func reportRetiredUserPoints(users []sourceUser, report *Report) {
	var total big.Int
	for _, user := range users {
		total.Add(&total, big.NewInt(user.GPTUnits))
		if user.GPTUnits != 0 {
			report.Counts["retired_features.users.gpt_wallet"]++
		}
	}
	report.Amounts["retired_features.users.gpt_wallet_v2_units"] = total.String()
}

func retiredAccountKind(kind string) bool {
	switch kind {
	case "wallet", "gpt_wallet", "points", "point_wallet", "bonus_quota", "blind_box_credits":
		return true
	default:
		return false
	}
}

func addRetiredAmount(report *Report, name string, value *int64) {
	if value == nil {
		return
	}
	key := "retired_features." + name
	total := new(big.Int)
	if current := report.Amounts[key]; current != "" {
		total.SetString(current, 10)
	}
	total.Add(total, big.NewInt(*value))
	report.Amounts[key] = total.String()
}
