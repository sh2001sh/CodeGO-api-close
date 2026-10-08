package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Histories stay in the source repeatable-read transaction. Validation and
// import stream it independently, so memory does not grow with log volume.
type historyData struct {
	source          pgx.Tx
	sources         map[string]string
	accounts        map[string]historyAccount
	retiredAccounts map[string]bool
	passkeys        []historyPasskey
	providers       []historyOAuthProvider
	bindings        []historyBinding
	counts          map[string]int64
	amounts         map[string]*big.Int
	issues          []Issue
}

func loadHistory(ctx context.Context, source pgx.Tx, sources map[string]string) (*historyData, error) {
	d := &historyData{source: source, sources: sources, accounts: map[string]historyAccount{}, retiredAccounts: map[string]bool{}, counts: map[string]int64{}, amounts: map[string]*big.Int{}}
	users := map[int64]bool{}
	if err := walkHistory(ctx, source, sources["users"], func(raw json.RawMessage) error {
		var u struct {
			ID int64 `json:"id"`
		}
		if json.Unmarshal(raw, &u) != nil {
			return fmt.Errorf("legacy: invalid history user reference")
		}
		users[u.ID] = true
		return nil
	}); err != nil {
		return nil, err
	}
	if sources["passkeys"] != "" && sources["passkey_credentials"] != "" {
		return nil, fmt.Errorf("legacy: ambiguous passkey source tables")
	}
	passkeyTable := sources["passkey_credentials"]
	if passkeyTable == "" {
		passkeyTable = sources["passkeys"]
	}
	loads := []struct {
		name, table string
		decode      func(json.RawMessage) error
	}{
		{"passkeys", passkeyTable, func(raw json.RawMessage) error {
			p, err := decodeHistoryPasskey(raw)
			if err != nil {
				return err
			}
			if !p.Deleted {
				if !users[p.UserID] {
					return fmt.Errorf("passkey references a missing user")
				}
				d.passkeys = append(d.passkeys, p)
				d.counts["passkeys_active"]++
			} else {
				d.counts["passkeys_deleted"]++
			}
			return nil
		}},
		{"custom_oauth_providers", sources["custom_oauth_providers"], func(raw json.RawMessage) error {
			var p historyOAuthProvider
			if err := json.Unmarshal(raw, &p); err != nil {
				return fmt.Errorf("invalid OAuth provider row")
			}
			if p.ID <= 0 || strings.TrimSpace(p.Slug) == "" {
				return fmt.Errorf("OAuth provider ID and slug are required")
			}
			if p.AuthStyle < 0 || p.AuthStyle > 2 {
				return fmt.Errorf("OAuth provider auth style must be 0, 1, or 2")
			}
			d.providers = append(d.providers, p)
			return nil
		}},
		{"user_oauth_bindings", sources["user_oauth_bindings"], func(raw json.RawMessage) error {
			var b historyBinding
			if err := json.Unmarshal(raw, &b); err != nil {
				return fmt.Errorf("invalid OAuth binding row")
			}
			if b.ID <= 0 || b.UserID <= 0 || b.ProviderID <= 0 || b.Subject == "" {
				return fmt.Errorf("OAuth binding identifiers are required")
			}
			if !users[b.UserID] {
				return fmt.Errorf("OAuth binding references a missing user")
			}
			d.bindings = append(d.bindings, b)
			return nil
		}},
		{"historical_accounts", sources["accounts"], func(raw json.RawMessage) error {
			if id, retired := retiredHistoryAccount(raw); retired {
				d.retiredAccounts[id] = true
				d.counts["historical_accounts"]--
				d.counts["retired:accounts"]++
				return nil
			}
			var a historyAccount
			if err := json.Unmarshal(raw, &a); err != nil {
				return fmt.Errorf("invalid historical account row")
			}
			if a.ID == "" || a.OwnerType == "" || a.Kind == "" {
				return fmt.Errorf("historical account identifiers are required")
			}
			if a.Unit != "quota" {
				return fmt.Errorf("historical account uses an unsupported money unit")
			}
			d.accounts[a.ID] = a
			return nil
		}},
		{"logs", sources["logs"], func(raw json.RawMessage) error {
			l, err := decodeHistoryLog(raw)
			if err != nil {
				return err
			}
			if l.Type == 2 && !users[l.UserID] {
				return fmt.Errorf("usage log references a missing user")
			}
			d.addAmount("logs", l.Amount)
			if l.Type == 2 {
				d.counts["usage_logs"]++
				d.addAmount("usage_logs", l.Amount)
			}
			return nil
		}},
		{"ledger_entries", sources["ledger_entries"], func(raw json.RawMessage) error {
			if d.retiredHistoryEntry(raw) {
				d.counts["ledger_entries"]--
				d.reportRetiredHistoryEntry(raw)
				return nil
			}
			e, err := decodeHistoryEntry(raw)
			if err != nil {
				return err
			}
			if _, exists := d.accounts[e.AccountID]; !exists {
				return fmt.Errorf("historical ledger references a missing account")
			}
			d.addAmount("ledger_entries", e.Amount)
			return nil
		}},
		{"request_audits", sources["request_audits"], func(raw json.RawMessage) error {
			a, err := decodeHistoryRequestAudit(raw)
			if err == nil {
				d.addAmount("request_audits", a.Amount)
			}
			return err
		}},
		{"request_attempt_audits", sources["request_attempt_audits"], func(raw json.RawMessage) error { _, err := decodeHistoryAttemptAudit(raw); return err }},
	}
	for _, load := range loads {
		visit := func(raw json.RawMessage) error {
			d.counts[load.name]++
			if err := load.decode(raw); err != nil {
				var row struct {
					ID int64 `json:"id"`
				}
				_ = json.Unmarshal(raw, &row)
				d.recordIssue(Issue{load.name, row.ID, "invalid_history", err.Error()})
			}
			return nil
		}
		var err error
		if load.name == "logs" {
			d.counts["usage_request_ids_disambiguated"] = 0
			err = walkHistoryLogs(ctx, source, load.table, func(raw json.RawMessage, duplicate bool) error {
				if duplicate {
					d.counts["usage_request_ids_disambiguated"]++
				}
				return visit(raw)
			})
		} else {
			err = walkHistory(ctx, source, load.table, visit)
		}
		if err != nil {
			return nil, err
		}
	}
	providerIDs := map[int64]bool{}
	for _, p := range d.providers {
		providerIDs[p.ID] = true
	}
	for _, b := range d.bindings {
		if !providerIDs[b.ProviderID] {
			d.recordIssue(Issue{"user_oauth_binding", b.ID, "missing_provider", "binding references a missing custom OAuth provider"})
		}
	}
	if d.counts["request_attempt_audits"] > 0 {
		if sources["request_audits"] == "" {
			d.recordIssue(Issue{"request_attempt_audit", 0, "missing_request_audits", "request attempt history requires its parent request audits"})
		} else {
			var orphans int64
			if err := source.QueryRow(ctx, `SELECT count(*) FROM `+sources["request_attempt_audits"]+` a LEFT JOIN `+sources["request_audits"]+` r ON r.request_id=a.request_id WHERE r.request_id IS NULL`).Scan(&orphans); err != nil {
				return nil, err
			}
			// Preserve every orphan's complete source record in a separate
			// archive. Native attempts retain their live parent constraint.
			d.counts["orphan_request_attempt_history"] = orphans
		}
	}
	d.counts["request_attempt_audits_linked"] = d.counts["request_attempt_audits"] - d.counts["orphan_request_attempt_history"]
	return d, nil
}

// Keep one representative issue per source/code, with exact occurrence counts.
// Bad rows still reject the entire import; corrupt large histories cannot fill
// memory with millions of equivalent diagnostic records.
func (d *historyData) recordIssue(issue Issue) {
	key := "invalid_rows." + issue.Entity + "." + issue.Code
	d.counts[key]++
	if d.counts[key] == 1 {
		d.issues = append(d.issues, issue)
	}
}

func (d *historyData) validate(report *Report) {
	report.Issues = append(report.Issues, d.issues...)
	if report.Counts == nil {
		report.Counts = map[string]int64{}
	}
	if report.Amounts == nil {
		report.Amounts = map[string]string{}
	}
	for name, count := range d.counts {
		if strings.HasPrefix(name, "retired:") {
			report.Counts["retired_features.billing_history."+strings.TrimPrefix(name, "retired:")] = count
			continue
		}
		report.Counts["history."+name] = count
	}
	for name, sum := range d.amounts {
		if strings.HasPrefix(name, "retired:") {
			report.Amounts["retired_features.billing_history."+strings.TrimPrefix(name, "retired:")] = sum.String()
			continue
		}
		report.Amounts["history."+name+".micro_credits"] = sum.String()
	}
}

func (d *historyData) addAmount(name string, amount int64) {
	if d.amounts[name] == nil {
		d.amounts[name] = new(big.Int)
	}
	d.amounts[name].Add(d.amounts[name], big.NewInt(amount))
}
