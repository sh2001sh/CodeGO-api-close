package legacy

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (d *commerceData) validate(report *Report) {
	if report.Counts == nil {
		report.Counts = map[string]int64{}
	}
	if report.Amounts == nil {
		report.Amounts = map[string]string{}
	}
	openingTotal := new(big.Int)
	subscriptionIDs, invoiceIDs := map[int64]bool{}, map[int64]bool{}
	for _, row := range d.rows["user_subscriptions"] {
		id, _ := row.integer("id")
		subscriptionIDs[id] = true
	}
	for _, row := range d.rows["invoice_requests"] {
		id, _ := row.integer("id")
		invoiceIDs[id] = true
	}
	for _, name := range commerceSourceNames {
		report.Counts[name] = int64(len(d.rows[name]))
		ids := map[int64]bool{}
		for _, row := range d.rows[name] {
			id, _ := row.integer("id")
			if name == "wallet_transfer_securities" {
				id, _ = row.integer("user_id")
			}
			if ids[id] {
				report.Issues = append(report.Issues, Issue{name, id, "duplicate_commerce_id", "source contains duplicate IDs"})
			}
			ids[id] = true
			projected, err := d.project(name, row)
			if err != nil {
				report.Issues = append(report.Issues, Issue{name, id, "invalid_commerce_row", err.Error()})
				continue
			}
			if projected.table == "orders" && projected.values["state"] == "paid" && projected.values["provider"] == "epay" && projected.values["payment_event_id"] == nil {
				report.Counts["orders_missing_provider_transaction"]++
			}
			for _, field := range []string{"user_id", "creator_id", "claimed_by", "sender_user_id", "recipient_user_id", "handled_by"} {
				if user, ok := projected.values[field].(int64); ok && user > 0 && !d.users[user] {
					report.Issues = append(report.Issues, Issue{name, id, "commerce_user_missing", field + " references an absent source user"})
				}
			}
			for _, field := range []string{"user_id", "sender_user_id", "recipient_user_id"} {
				if user, exists := projected.values[field]; exists && user.(int64) <= 0 {
					report.Issues = append(report.Issues, Issue{name, id, "invalid_commerce_user", field + " must be positive"})
				}
			}
			for field, validIDs := range map[string]map[int64]bool{"subscription_id": subscriptionIDs, "target_subscription_id": subscriptionIDs, "invoice_id": invoiceIDs} {
				if ref, ok := projected.values[field].(int64); ok && ref > 0 && !validIDs[ref] {
					report.Issues = append(report.Issues, Issue{name, id, "commerce_reference_missing", field + " references an absent source record"})
				}
			}
			if name == "wallet_transfers" && projected.values["sender_user_id"] == projected.values["recipient_user_id"] {
				report.Issues = append(report.Issues, Issue{name, id, "invalid_wallet_transfer", "sender and recipient must differ"})
			}
			if name == "user_subscriptions" {
				amount, balanceErr := d.subscriptionOpening(row, projected)
				if balanceErr != nil {
					report.Issues = append(report.Issues, Issue{name, id, "invalid_subscription_balance", balanceErr.Error()})
				} else {
					openingTotal.Add(openingTotal, big.NewInt(amount))
				}
			}
		}
	}
	report.Amounts["subscription_opening_micro_credits"] = openingTotal.String()
	d.validateRefundOrigins(report)
	// Two order tables share one unique trade-number namespace in v3.
	trades := map[string]bool{}
	for _, name := range []string{"top_ups", "subscription_orders"} {
		for _, row := range d.rows[name] {
			trade, _ := row.text("trade_no")
			if trades[trade] {
				id, _ := row.integer("id")
				report.Issues = append(report.Issues, Issue{name, id, "duplicate_trade_no", "duplicate trade number across source commerce orders"})
			}
			trades[trade] = true
		}
	}
}

func (d *commerceData) subscriptionOpening(row commerceRow, projected commerceProjection) (int64, error) {
	id, _ := row.integer("id")
	total, used := projected.values["total_credits"].(int64), projected.values["used_credits"].(int64)
	period, periodUsed := projected.values["period_credits"].(int64), projected.values["period_used"].(int64)
	remaining := total - used
	if total == 0 {
		if period == 0 {
			return 0, fmt.Errorf("unlimited subscription has no finite ledger representation; target funding policy is required")
		}
		remaining = period - periodUsed
	}
	if snapshot, exists := d.snapshot[id]; exists {
		if snapshot[1] != 0 {
			return 0, fmt.Errorf("subscription has outstanding reserved v2 units")
		}
		converted, err := OpeningBalance(snapshot[0])
		if err != nil {
			return 0, err
		}
		if total > 0 && int64(converted) != remaining {
			return 0, fmt.Errorf("subscription canonical snapshot and lifetime projection differ")
		}
		remaining = int64(converted)
	}
	// v2 checks the period limit in addition to its canonical lifetime balance.
	// v3 accounts are admission buckets, so expose only the currently spendable
	// period remainder while retaining exact lifetime totals in subscriptions.
	if period > 0 {
		remaining = min(remaining, period-periodUsed)
	}
	if projected.values["state"] != "active" {
		// An ended bucket must not become consumable, even if old projection
		// retains nominal unused quota. A positive canonical ledger blocks it.
		if snapshot, exists := d.snapshot[id]; exists && snapshot[0] > 0 {
			return 0, fmt.Errorf("ended subscription has a nonzero canonical balance; reconcile source first")
		}
		remaining = 0
	}
	return remaining, nil
}

func (m *Importer) importCommerce(ctx context.Context, target pgx.Tx, data *commerceData) error {
	populated := map[string]bool{}
	// Plans precede both order namespaces; subscriptions follow paid history.
	for _, name := range []string{"subscription_plans", "top_ups", "subscription_orders", "user_subscriptions", "redemptions", "wallet_transfers", "wallet_transfer_securities", "invoice_requests", "invoice_request_items", "subscription_pre_consume_records"} {
		for _, row := range data.rows[name] {
			projected, err := data.project(name, row)
			if err != nil {
				return fmt.Errorf("legacy: project %s: %w", name, err)
			}
			if name == "user_subscriptions" {
				id := projected.values["id"].(int64)
				amount, err := data.subscriptionOpening(row, projected)
				if err != nil {
					return err
				}
				if err = opening(ctx, target, "subscription", id, "subscription", amount); err != nil {
					return err
				}
				var accountID int64
				if err = target.QueryRow(ctx, `SELECT id FROM v3_billing.accounts WHERE owner_type='subscription' AND owner_id=$1 AND kind='subscription'`, id).Scan(&accountID); err != nil {
					return err
				}
				projected.values["account_id"] = accountID
			}
			if err = insertCommerce(ctx, target, projected); err != nil {
				return err
			}
			populated[projected.table] = true
			if name == "user_subscriptions" {
				if _, err = target.Exec(ctx, `INSERT INTO v3_commerce.subscription_buckets(account_id,subscription_id,starts_at,ended_at)
				 VALUES($1,$2,$3,$4) ON CONFLICT(account_id) DO NOTHING`, projected.values["account_id"], projected.values["id"], projected.values["starts_at"], projected.values["ended_at"]); err != nil {
					return err
				}
			}
		}
	}
	for _, table := range []string{"plans", "orders", "subscriptions", "redemption_codes", "wallet_transfers", "invoices", "invoice_items", "subscription_preconsumes"} {
		if !populated[table] {
			continue
		}
		qualified := "v3_commerce." + table
		if _, err := target.Exec(ctx, `SELECT setval(pg_get_serial_sequence($1,'id'),
			GREATEST(COALESCE((SELECT max(id) FROM `+qualified+`),0),1),EXISTS(SELECT 1 FROM `+qualified+`))`, qualified); err != nil {
			return fmt.Errorf("legacy: reset sequence %s: %w", table, err)
		}
	}
	return m.importRefundOrigins(ctx, target, data)
}

func insertCommerce(ctx context.Context, tx pgx.Tx, row commerceProjection) error {
	fields := make([]string, 0, len(row.values))
	for field := range row.values {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	args, placeholders, quoted := make([]any, len(fields)), make([]string, len(fields)), make([]string, len(fields))
	for i, field := range fields {
		args[i], placeholders[i], quoted[i] = row.values[field], "$"+strconv.Itoa(i+1), pgx.Identifier{field}.Sanitize()
	}
	primary := "id"
	if row.table == "wallet_transfer_security" {
		primary = "user_id"
	}
	table := pgx.Identifier{"v3_commerce", row.table}.Sanitize()
	tag, err := tx.Exec(ctx, "INSERT INTO "+table+"("+strings.Join(quoted, ",")+") VALUES("+strings.Join(placeholders, ",")+") ON CONFLICT("+primary+") DO NOTHING", args...)
	if err != nil {
		return fmt.Errorf("legacy: insert commerce %s row %v: %w", row.table, row.values[primary], err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	// Used balances/state must never be overwritten on reimport. Check identity
	// instead; a coincident native target ID must not swallow another source row.
	checks := []string{primary + "=$1"}
	identityArgs := []any{row.values[primary]}
	for _, field := range row.identity {
		identityArgs = append(identityArgs, row.values[field])
		checks = append(checks, pgx.Identifier{field}.Sanitize()+" IS NOT DISTINCT FROM $"+strconv.Itoa(len(identityArgs)))
	}
	var found bool
	err = tx.QueryRow(ctx, "SELECT true FROM "+table+" WHERE "+strings.Join(checks, " AND "), identityArgs...).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("legacy: target commerce %s ID %v belongs to a different record", row.table, row.values[primary])
	}
	return err
}
