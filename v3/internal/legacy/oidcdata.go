package legacy

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

type oidcData struct {
	records []oidcRecord
	issues  []Issue
}
type oidcRecord struct {
	table, key string
	fields     map[string]any
}

func loadOIDCData(ctx context.Context, source pgx.Tx, sources map[string]string) (*oidcData, error) {
	d := &oidcData{}
	users := map[int64]bool{}
	if err := walkHistory(ctx, source, sources["users"], func(raw json.RawMessage) error {
		var row struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		users[row.ID] = true
		return nil
	}); err != nil {
		return nil, err
	}
	for _, kind := range []string{"authorization_codes", "access_tokens"} {
		err := walkHistory(ctx, source, sources["codego_oidc_"+kind], func(raw json.RawMessage) error {
			var row struct {
				CodeHash    string     `json:"code_hash"`
				TokenHash   string     `json:"token_hash"`
				ClientID    string     `json:"client_id"`
				UserID      int64      `json:"user_id"`
				RedirectURI string     `json:"redirect_uri"`
				Scope       string     `json:"scope"`
				Nonce       string     `json:"nonce"`
				Challenge   string     `json:"code_challenge"`
				ExpiresAt   time.Time  `json:"expires_at"`
				UsedAt      *time.Time `json:"used_at"`
				RevokedAt   *time.Time `json:"revoked_at"`
			}
			if err := json.Unmarshal(raw, &row); err != nil {
				return fmt.Errorf("legacy: invalid OIDC row")
			}
			key, table, encoded := "token_hash", "oidc_tokens", row.TokenHash
			if kind == "authorization_codes" {
				key, table, encoded = "code_hash", "oidc_codes", row.CodeHash
			}
			digest, err := hex.DecodeString(encoded)
			if err != nil || len(digest) != 32 || row.ClientID == "" || !users[row.UserID] || row.ExpiresAt.IsZero() {
				d.issues = append(d.issues, Issue{"oidc", row.UserID, "invalid_oidc_grant", "grant digest, client, user or expiry is invalid"})
				return nil
			}
			fields := map[string]any{key: digest, "client_id": row.ClientID, "user_id": row.UserID, "scope": row.Scope, "expires_at": row.ExpiresAt}
			if kind == "authorization_codes" {
				if row.RedirectURI == "" || row.Challenge == "" {
					d.issues = append(d.issues, Issue{"oidc", row.UserID, "invalid_oidc_code", "redirect URI and PKCE challenge are required"})
					return nil
				}
				fields["redirect_uri"], fields["nonce"], fields["code_challenge"] = row.RedirectURI, row.Nonce, row.Challenge
				// Used codes remain unusable even during their original lifetime.
				if row.UsedAt != nil && row.UsedAt.Before(row.ExpiresAt) {
					fields["expires_at"] = *row.UsedAt
				}
			} else {
				fields["revoked_at"] = row.RevokedAt
			}
			d.records = append(d.records, oidcRecord{table, key, fields})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return d, nil
}

func (d *oidcData) validate(report *Report) {
	report.Issues = append(report.Issues, d.issues...)
	for _, r := range d.records {
		report.Counts["oidc:"+r.table]++
	}
}

func (m *Importer) importOIDCData(ctx context.Context, target pgx.Tx, d *oidcData) error {
	for _, r := range d.records {
		columns := make([]string, 0, len(r.fields))
		for key := range r.fields {
			columns = append(columns, key)
		}
		sort.Strings(columns)
		values := make([]any, len(columns))
		for i, key := range columns {
			values[i] = r.fields[key]
		}
		if err := insertHistoryExact(ctx, target, "v3_identity", r.table, r.key, columns, values); err != nil {
			return fmt.Errorf("legacy: import OIDC grant: %w", err)
		}
	}
	return nil
}

func (m *Importer) checkOIDCData(ctx context.Context, target pgx.Tx, d *oidcData, report *Report) error {
	for _, r := range d.records {
		match, err := checkProjection(ctx, target, "v3_identity."+r.table, r.fields)
		if err != nil {
			return err
		}
		if !match {
			checkIssue(report, "oidc", r.fields["user_id"].(int64), "OIDC grant digest, owner or expiry differs from source")
		}
		report.Counts["check:oidc"]++
	}
	return nil
}
