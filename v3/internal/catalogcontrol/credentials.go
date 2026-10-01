package catalogcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

var errEncryptionUnavailable = errors.New("catalogcontrol: credential encryption unavailable")

type Credential struct {
	ID             int64                         `json:"id"`
	Kind           string                        `json:"kind"`
	Status         string                        `json:"status"`
	ExpiresAt      *time.Time                    `json:"expires_at"`
	MaxConcurrency int                           `json:"max_concurrency"`
	Fingerprint    catalog.CredentialFingerprint `json:"fingerprint"`
}

func (s *Server) listCredentials(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	rows, err := s.pool.Query(r.Context(), `SELECT id,kind,status,expires_at,max_concurrency,fingerprint FROM v3_catalog.channel_credentials WHERE channel_id=$1 ORDER BY id`, id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer rows.Close()
	items := make([]Credential, 0)
	for rows.Next() {
		var c Credential
		if err = rows.Scan(&c.ID, &c.Kind, &c.Status, &c.ExpiresAt, &c.MaxConcurrency, &c.Fingerprint); err != nil {
			s.dbError(w, err)
			return
		}
		items = append(items, c)
	}
	if err = rows.Err(); err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, items)
}

// credentialInput is the decoded request body for creating or updating a
// channel credential.
type credentialInput struct {
	Kind           string                         `json:"kind"`
	Status         string                         `json:"status"`
	Secret         *string                        `json:"secret"`
	ExpiresAt      json.RawMessage                `json:"expires_at"`
	MaxConcurrency *int                           `json:"max_concurrency"`
	Fingerprint    *catalog.CredentialFingerprint `json:"fingerprint"`
}

// normalizeCredentialInput applies POST-only defaults and validates kind,
// status, secret length and fingerprint.
func normalizeCredentialInput(input *credentialInput, method string) (errCode, errMsg string) {
	if err := validateFingerprint(input.Fingerprint); err != nil {
		return "invalid_fingerprint", err.Error()
	}
	if input.Kind == "" && method == http.MethodPost {
		input.Kind = "api_key"
	}
	if input.Status == "" && method == http.MethodPost {
		input.Status = "enabled"
	}
	if (input.MaxConcurrency != nil && *input.MaxConcurrency < 0) || (input.Kind != "" && input.Kind != "api_key" && input.Kind != "oauth") || (input.Status != "" && input.Status != "enabled" && input.Status != "disabled") || (input.Secret != nil && (len(*input.Secret) == 0 || len(*input.Secret) > 131072)) {
		return "invalid_credential", "Invalid credential kind, status or secret"
	}
	return "", ""
}

// resolveCredentialExpiry parses the explicit expires_at field, or derives it
// from an oauth secret when the field was omitted.
func resolveCredentialExpiry(input credentialInput) (*time.Time, error) {
	var expires *time.Time
	if len(input.ExpiresAt) > 0 {
		if err := json.Unmarshal(input.ExpiresAt, &expires); err != nil {
			return nil, err
		}
	}
	if len(input.ExpiresAt) == 0 && input.Secret != nil && input.Kind == "oauth" {
		expires = oauthExpiry(*input.Secret)
	}
	return expires, nil
}

// encryptCredentialSecret encrypts input.Secret when present, returning nil
// ciphertext (and no error) when no secret was supplied.
func (s *Server) encryptCredentialSecret(input credentialInput) ([]byte, error) {
	if input.Secret == nil {
		return nil, nil
	}
	if s.enc == nil {
		return nil, errEncryptionUnavailable
	}
	return s.enc.Encrypt([]byte(*input.Secret))
}

// insertCredential creates a new channel credential row and returns its id.
func (s *Server) insertCredential(ctx context.Context, channelID int64, input credentialInput, ciphertext []byte, expires *time.Time) (int64, error) {
	var credentialID int64
	err := s.pool.QueryRow(ctx, `INSERT INTO v3_catalog.channel_credentials(channel_id,kind,status,secret,expires_at,max_concurrency,fingerprint) VALUES($1,$2,$3,$4,$5,coalesce($6,0),coalesce($7::jsonb,'{}')) RETURNING id`, channelID, input.Kind, input.Status, ciphertext, expires, input.MaxConcurrency, input.Fingerprint).Scan(&credentialID)
	return credentialID, err
}

// updateCredential updates an existing channel credential row identified by
// credentialID and channelID, leaving unset fields unchanged.
func (s *Server) updateCredential(ctx context.Context, channelID, credentialID int64, input credentialInput, ciphertext []byte, expires *time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE v3_catalog.channel_credentials SET kind=coalesce(nullif($1,''),kind),status=coalesce(nullif($2,''),status),secret=coalesce($3::bytea,secret),expires_at=CASE WHEN $8 THEN $4::timestamptz ELSE expires_at END,max_concurrency=coalesce($7,max_concurrency),fingerprint=coalesce($9::jsonb,fingerprint) WHERE id=$5 AND channel_id=$6`, input.Kind, input.Status, ciphertext, expires, credentialID, channelID, input.MaxConcurrency, len(input.ExpiresAt) > 0 || expires != nil, input.Fingerprint)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Server) saveCredential(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var input credentialInput
	if !decode(w, r, &input) {
		return
	}
	if errCode, errMsg := normalizeCredentialInput(&input, r.Method); errCode != "" {
		fail(w, 400, errCode, errMsg)
		return
	}
	expires, err := resolveCredentialExpiry(input)
	if err != nil {
		fail(w, 400, "invalid_expiry", "Expected an RFC3339 credential expiry or null")
		return
	}
	ciphertext, err := s.encryptCredentialSecret(input)
	if err != nil {
		if errors.Is(err, errEncryptionUnavailable) {
			fail(w, 503, "encryption_unavailable", "Credential encryption is unavailable")
		} else {
			s.dbError(w, err)
		}
		return
	}
	var credentialID int64
	if r.Method == http.MethodPost {
		if input.Secret == nil {
			fail(w, 400, "missing_secret", "A new credential requires a secret")
			return
		}
		credentialID, err = s.insertCredential(r.Context(), id, input, ciphertext, expires)
	} else {
		credentialID, ok = pathID(w, r, "credential")
		if !ok {
			return
		}
		err = s.updateCredential(r.Context(), id, credentialID, input, ciphertext, expires)
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, map[string]any{"id": credentialID})
}

func (s *Server) deleteCredential(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	cr, ok := pathID(w, r, "credential")
	if !ok {
		return
	}
	tag, err := s.pool.Exec(r.Context(), `DELETE FROM v3_catalog.channel_credentials WHERE id=$1 AND channel_id=$2`, cr, id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		s.dbError(w, pgx.ErrNoRows)
		return
	}
	respond(w, 200, nil)
}
