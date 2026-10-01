package legacy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (m *Importer) importMarketplace(ctx context.Context, target pgx.Tx, data *marketplaceData) error {
	if len(data.issues) > 0 {
		return errors.New("legacy: marketplace import blocked by validation issues")
	}
	records := append([]marketplaceRecord(nil), data.records...)
	priority := map[string]int{"group_buys": 1, "group_buy_members": 2, "blind_box_pools": 3, "blind_box_orders": 4, "blind_box_purchases": 5, "blind_box_items": 6, "blind_box_open_records": 7, "blind_box_props": 8, "blind_box_pity": 9, "blind_box_grants": 10, "blind_box_credits": 11, "blind_box_prop_gifts": 12, "blind_box_prop_discount_usages": 13, "blind_box_gifts": 14, "blind_box_gift_items": 15, "blind_box_zero_hour_states": 16}
	sort.SliceStable(records, func(i, j int) bool { return priority[records[i].Table] < priority[records[j].Table] })
	for _, record := range records {
		if record.Table == "group_buy_members" {
			if record.Fields["subscription_id"] == nil {
				record.Fields["account_id"] = nil
			} else {
				var account int64
				if err := target.QueryRow(ctx, `SELECT account_id FROM v3_commerce.subscriptions WHERE id=$1 AND user_id=$2`, record.Fields["subscription_id"], record.Fields["user_id"]).Scan(&account); err != nil {
					return fmt.Errorf("legacy: group member %d subscription account: %w", record.ID, err)
				}
				record.Fields["account_id"] = account
			}
		}
		if err := importMarketplaceRecord(ctx, target, record); err != nil {
			return err
		}
	}
	for _, item := range data.source["balance_blind_box_items"] {
		openID := data.integer(item, "open_record_id")
		if openID <= 0 {
			continue
		}
		result, err := target.Exec(ctx, `UPDATE v3_marketplace.blind_box_items SET open_record_id=$2 WHERE id=$1 AND (open_record_id IS NULL OR open_record_id=$2)`, item.id(), openID)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return fmt.Errorf("legacy: item %d open link changed", item.id())
		}
	}
	if err := importMarketplaceProfiles(ctx, target); err != nil {
		return err
	}
	tables := make(map[string]bool)
	for _, r := range data.records {
		if _, ok := r.Fields["id"]; ok {
			tables[r.Table] = true
		}
	}
	for table := range tables {
		qualified := pgx.Identifier{"v3_marketplace", table}.Sanitize()
		if _, err := target.Exec(ctx, `SELECT setval(pg_get_serial_sequence($1,'id'),GREATEST(COALESCE((SELECT max(id) FROM `+qualified+`),0),1),EXISTS(SELECT 1 FROM `+qualified+`))`, "v3_marketplace."+table); err != nil {
			return err
		}
	}
	return nil
}

// Equality on every mapped field makes replay idempotent while refusing a
// different legacy row that happens to collide with an existing target ID.
func importMarketplaceRecord(ctx context.Context, target pgx.Tx, r marketplaceRecord) error {
	columns := marketplaceColumns(r.Fields)
	quoted, placeholders, current, proposed := make([]string, len(columns)), make([]string, len(columns)), make([]string, len(columns)), make([]string, len(columns))
	args := make([]any, len(columns))
	for i, c := range columns {
		q := pgx.Identifier{c}.Sanitize()
		quoted[i] = q
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		current[i] = "t." + q
		proposed[i] = "EXCLUDED." + q
		args[i] = r.Fields[c]
	}
	key := "id"
	conflict := `"id"`
	if r.Table == "blind_box_pity" {
		key = "user_id"
		conflict = `"user_id","pool_id"`
	}
	query := `INSERT INTO ` + pgx.Identifier{"v3_marketplace", r.Table}.Sanitize() + ` AS t (` + strings.Join(quoted, ",") + `) VALUES (` + strings.Join(placeholders, ",") + `) ON CONFLICT (` + conflict + `) DO UPDATE SET ` + pgx.Identifier{key}.Sanitize() + `=EXCLUDED.` + pgx.Identifier{key}.Sanitize() + ` WHERE ROW(` + strings.Join(current, ",") + `) IS NOT DISTINCT FROM ROW(` + strings.Join(proposed, ",") + `) RETURNING ` + pgx.Identifier{key}.Sanitize()
	var id int64
	if err := target.QueryRow(ctx, query, args...).Scan(&id); err != nil {
		return fmt.Errorf("legacy: import %s %d changed or invalid: %w", r.Table, r.ID, err)
	}
	return nil
}
