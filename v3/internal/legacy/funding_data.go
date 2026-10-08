package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var fundingSourceNames = []string{"funding_source_policies", "funding_lots", "funding_allocations", "request_economics", "wallet_reward_holds"}

type fundingData struct {
	source   pgx.Tx
	sources  map[string]string
	rows     map[string][]commerceRow
	accounts map[string]historyAccount
	users    map[int64]sourceUser
	drains   []Issue
	queues   map[string]int64
}

func fundingSource(sources map[string]string, name string) string {
	if table := sources["billing_"+name]; table != "" {
		return table
	}
	return sources[name]
}

func loadFunding(ctx context.Context, source pgx.Tx, sources map[string]string) (*fundingData, error) {
	// Keep the source snapshot alive, but never materialize funding tables.
	// Only the much smaller owner/account indexes are needed for projections.
	d := &fundingData{source: source, sources: sources, accounts: map[string]historyAccount{}, users: map[int64]sourceUser{}, queues: map[string]int64{}}
	err := walkHistory(ctx, source, sources["accounts"], func(raw json.RawMessage) error {
		var account historyAccount
		if err := json.Unmarshal(raw, &account); err != nil {
			return fmt.Errorf("legacy: invalid funding account: %w", err)
		}
		d.accounts[account.ID] = account
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = walkHistory(ctx, source, sources["users"], func(raw json.RawMessage) error {
		var user sourceUser
		if err := json.Unmarshal(raw, &user); err != nil {
			return fmt.Errorf("legacy: invalid funding owner: %w", err)
		}
		d.users[user.ID] = user
		return nil
	})
	if err != nil {
		return nil, err
	}
	return d, d.inspectDrains(ctx, source, sources)
}

func (d *fundingData) walk(ctx context.Context, name string, visit func(commerceRow) error) error {
	if d.source == nil {
		for _, row := range d.rows[name] {
			if err := visit(row); err != nil {
				return err
			}
		}
		return nil
	}
	return walkHistory(ctx, d.source, fundingSource(d.sources, name), func(raw json.RawMessage) error {
		var row commerceRow
		if err := json.Unmarshal(raw, &row); err != nil {
			return fmt.Errorf("legacy: invalid funding row in %s: %w", name, err)
		}
		return visit(row)
	})
}

func (d *fundingData) retiredAccountIDs() []string {
	ids := make([]string, 0)
	for id, account := range d.accounts {
		if retiredAccountKind(account.Kind) {
			ids = append(ids, id)
		}
	}
	return ids
}

func (d *fundingData) inspectDrains(ctx context.Context, source pgx.Tx, sources map[string]string) error {
	terminal := map[string]map[string]bool{
		"reservations":  {"settled": true, "released": true, "expired": true},
		"settlements":   {"completed": true, "rejected": true},
		"outbox_events": {"published": true},
	}
	for _, name := range []string{"reservations", "settlements", "outbox_events"} {
		table := fundingSource(sources, name)
		if table == "" {
			continue
		}
		rows, err := source.Query(ctx, "SELECT COALESCE(status,''),count(*) FROM "+table+" GROUP BY status")
		if err != nil {
			return err
		}
		for rows.Next() {
			var status string
			var count int64
			if err = rows.Scan(&status, &count); err != nil {
				rows.Close()
				return err
			}
			d.queues["billing_"+name] += count
			if count > 0 && !terminal[name][status] {
				d.drains = append(d.drains, Issue{"billing_" + name, 0, "source_not_drained", fmt.Sprintf("%d source records are nonterminal; stop writers and drain before import", count)})
			}
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}
	if table := sources["balance_snapshots"]; table != "" {
		var outstanding int64
		if err := source.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE reserved_balance<>0 AND account_id<>ALL($1::text[])", d.retiredAccountIDs()).Scan(&outstanding); err != nil {
			return err
		}
		if outstanding > 0 {
			d.drains = append(d.drains, Issue{"billing_balance_snapshots", 0, "source_not_drained", "source billing accounts still contain reserved balances; drain all accounts before import"})
		}
	}
	return nil
}

func fundingText(row commerceRow, name string, required bool) (string, error) {
	value, err := row.text(name)
	if err != nil {
		return "", err
	}
	if required && strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func fundingTime(row commerceRow, name string) (time.Time, error) {
	value, err := row.text(name)
	if err != nil {
		return time.Time{}, err
	}
	if value == "" {
		return time.Time{}, fmt.Errorf("%s timestamp is required", name)
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Year() < 1 || parsed.Year() > 9999 {
		return time.Time{}, fmt.Errorf("%s must be a valid timestamp", name)
	}
	return parsed.UTC(), nil
}

func fundingKind(value string) bool {
	switch value {
	case "topup", "blind_box", "subscription", "legacy_unattributed", "other":
		return true
	default:
		return false
	}
}

func fundingPPM(row commerceRow, name string) (int64, error) {
	value, err := row.decimal(name)
	if err != nil {
		return 0, err
	}
	// Exact decimal arithmetic follows the existing native PPM conversion:
	// half-up only at the final integer boundary, never intermediate float64.
	number, ok := new(big.Rat).SetString(value)
	if !ok || number.Sign() < 0 || strings.Contains(value, "/") {
		return 0, fmt.Errorf("%s must be a finite nonnegative decimal", name)
	}
	return decimalProduct(value, "1000000")
}
