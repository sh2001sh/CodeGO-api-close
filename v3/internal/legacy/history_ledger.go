package legacy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type historyAccount struct {
	ID        string          `json:"account_id"`
	OwnerType string          `json:"owner_type"`
	OwnerID   int64           `json:"owner_id"`
	Kind      string          `json:"account_type"`
	Unit      string          `json:"quota_unit"`
	Status    string          `json:"status"`
	Version   int64           `json:"version"`
	Metadata  json.RawMessage `json:"meta_json"`
	CreatedAt historyTime     `json:"created_at"`
	UpdatedAt historyTime     `json:"updated_at"`
}

type historyEntry struct {
	ID             string          `json:"entry_id"`
	AccountID      string          `json:"account_id"`
	ReferenceType  string          `json:"reference_type"`
	ReferenceID    string          `json:"reference_id"`
	EntryType      string          `json:"entry_type"`
	Direction      string          `json:"direction"`
	Amount         int64           `json:"amount"`
	BalanceAfter   *int64          `json:"balance_after"`
	IdempotencyKey string          `json:"idempotency_key"`
	ReasonCode     string          `json:"reason_code"`
	ReasonDetail   string          `json:"reason_detail"`
	OperatorType   string          `json:"operator_type"`
	OperatorID     string          `json:"operator_id"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedAt      historyTime     `json:"created_at"`
}

func decodeHistoryEntry(raw json.RawMessage) (historyEntry, error) {
	var e historyEntry
	if json.Unmarshal(raw, &e) != nil {
		return e, fmt.Errorf("invalid historical ledger fields")
	}
	if e.ID == "" || e.AccountID == "" || e.EntryType == "" || e.IdempotencyKey == "" {
		return e, fmt.Errorf("historical ledger identifiers and type are required")
	}
	if e.Amount < 0 {
		return e, fmt.Errorf("source historical ledger amount must be nonnegative")
	}
	amount, err := FromV2Units(e.Amount)
	if err != nil {
		return e, fmt.Errorf("historical ledger amount overflows micro-credits")
	}
	switch e.Direction {
	case "debit":
		e.Amount = -int64(amount)
	case "credit":
		e.Amount = int64(amount)
	default:
		return e, fmt.Errorf("historical ledger direction must be debit or credit")
	}
	if e.BalanceAfter != nil {
		balance, err := FromV2Units(*e.BalanceAfter)
		if err != nil {
			return e, fmt.Errorf("historical ledger balance overflows micro-credits")
		}
		converted := int64(balance)
		e.BalanceAfter = &converted
	}
	e.Metadata = historyMetadata(e.Metadata)
	return e, nil
}

// Original source account IDs/FKs are retained separately from current accounts.
// Current monetary subscriptions may be closed, but their financial history
// stays queryable without posting money again. Retired points remain in v2.
func (m *Importer) importHistoryLedger(ctx context.Context, target pgx.Tx, d *historyData) error {
	for _, a := range d.accounts {
		var mapped *int64
		owner, kind := historicalAccountMapping(a)
		if kind != "" {
			var id int64
			err := target.QueryRow(ctx, `SELECT id FROM v3_billing.accounts WHERE owner_type=$1 AND owner_id=$2 AND kind=$3`, owner, a.OwnerID, kind).Scan(&id)
			if err == nil {
				mapped = &id
			} else if err != pgx.ErrNoRows {
				return err
			}
		}
		columns := []string{"source_account_id", "account_id", "owner_type", "owner_id", "account_type", "unit", "status", "version", "metadata", "created_at", "updated_at"}
		values := []any{a.ID, mapped, a.OwnerType, a.OwnerID, a.Kind, a.Unit, a.Status, a.Version, historyMetadata(a.Metadata), historyDate(a.CreatedAt), historyDate(a.UpdatedAt)}
		if err := insertHistoryExact(ctx, target, "v3_billing", "historical_accounts", "source_account_id", columns, values); err != nil {
			return fmt.Errorf("legacy: historical account import: %w", err)
		}
	}
	return walkHistory(ctx, d.source, d.sources["ledger_entries"], func(raw json.RawMessage) error {
		if d.retiredHistoryEntry(raw) {
			return nil
		}
		e, err := decodeHistoryEntry(raw)
		if err != nil {
			return err
		}
		columns := []string{"entry_id", "source_account_id", "reference_type", "reference_id", "entry_type", "direction", "amount", "balance_after", "idempotency_key", "reason_code", "reason_detail", "operator_type", "operator_id", "metadata", "created_at"}
		values := []any{e.ID, e.AccountID, e.ReferenceType, e.ReferenceID, e.EntryType, e.Direction, e.Amount, e.BalanceAfter, e.IdempotencyKey, e.ReasonCode, e.ReasonDetail, e.OperatorType, e.OperatorID, e.Metadata, historyDate(e.CreatedAt)}
		return insertHistoryExact(ctx, target, "v3_billing", "historical_entries", "entry_id", columns, values)
	})
}

func historicalAccountMapping(a historyAccount) (string, string) {
	switch {
	case a.OwnerType == "user" && a.Kind == "claude_wallet":
		return "user", "wallet"
	case a.OwnerType == "token" && a.Kind == "token":
		return "api_key", "key_budget"
	case a.OwnerType == "user_subscription" && a.Kind == "subscription":
		return "subscription", "subscription"
	case a.OwnerType == "user" && a.Kind == "marketplace_owner_pending":
		return "user", "marketplace_pending"
	case a.OwnerType == "user" && a.Kind == "marketplace_owner_available":
		return "user", "marketplace_earned"
	case a.OwnerType == "system" && a.Kind == "marketplace_platform_revenue":
		return "platform", "platform_revenue"
	default:
		return "", ""
	}
}
