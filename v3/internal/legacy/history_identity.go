package legacy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
)

type historyPasskey struct {
	ID                   int64
	UserID               int64
	Credential           webauthn.Credential
	CreatedAt, UpdatedAt historyTime
	LastUsedAt           *historyTime
	Deleted              bool
}

func decodeHistoryPasskey(raw json.RawMessage) (historyPasskey, error) {
	var old struct {
		ID             int64           `json:"id"`
		UserID         int64           `json:"user_id"`
		CredentialID   string          `json:"credential_id"`
		PublicKey      string          `json:"public_key"`
		Attestation    string          `json:"attestation_type"`
		AAGUID         string          `json:"aaguid"`
		SignCount      uint32          `json:"sign_count"`
		CloneWarning   bool            `json:"clone_warning"`
		UserPresent    bool            `json:"user_present"`
		UserVerified   bool            `json:"user_verified"`
		BackupEligible bool            `json:"backup_eligible"`
		BackupState    bool            `json:"backup_state"`
		Transports     string          `json:"transports"`
		Attachment     string          `json:"attachment"`
		CreatedAt      historyTime     `json:"created_at"`
		UpdatedAt      historyTime     `json:"updated_at"`
		LastUsedAt     *historyTime    `json:"last_used_at"`
		DeletedAt      json.RawMessage `json:"deleted_at"`
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		return historyPasskey{}, fmt.Errorf("invalid passkey fields")
	}
	p := historyPasskey{ID: old.ID, UserID: old.UserID, CreatedAt: old.CreatedAt, UpdatedAt: old.UpdatedAt, LastUsedAt: old.LastUsedAt,
		Deleted: len(old.DeletedAt) > 0 && string(old.DeletedAt) != "null"}
	if p.Deleted {
		return p, nil
	}
	if p.ID <= 0 || p.UserID <= 0 {
		return p, fmt.Errorf("passkey ID and user ID are required")
	}
	id, err := base64.StdEncoding.DecodeString(old.CredentialID)
	if err != nil || len(id) == 0 {
		return p, fmt.Errorf("invalid passkey credential ID encoding")
	}
	public, err := base64.StdEncoding.DecodeString(old.PublicKey)
	if err != nil || len(public) == 0 {
		return p, fmt.Errorf("invalid passkey public key encoding")
	}
	if _, err = webauthncose.ParsePublicKey(public); err != nil {
		return p, fmt.Errorf("passkey public key is not a valid COSE key")
	}
	aaguid, err := base64.StdEncoding.DecodeString(old.AAGUID)
	if err != nil || (len(aaguid) != 0 && len(aaguid) != 16) {
		return p, fmt.Errorf("invalid passkey AAGUID")
	}
	var transports []protocol.AuthenticatorTransport
	if old.Transports != "" && json.Unmarshal([]byte(old.Transports), &transports) != nil {
		return p, fmt.Errorf("invalid passkey transports")
	}
	if old.BackupState && !old.BackupEligible {
		return p, fmt.Errorf("passkey backup state requires backup eligibility")
	}
	p.Credential = webauthn.Credential{ID: id, PublicKey: public, AttestationType: old.Attestation, Transport: transports,
		Flags:         webauthn.CredentialFlags{UserPresent: old.UserPresent, UserVerified: old.UserVerified, BackupEligible: old.BackupEligible, BackupState: old.BackupState},
		Authenticator: webauthn.Authenticator{AAGUID: aaguid, SignCount: old.SignCount, CloneWarning: old.CloneWarning, Attachment: protocol.AuthenticatorAttachment(old.Attachment)}}
	return p, nil
}

type historyBinding struct {
	ID         int64       `json:"id"`
	UserID     int64       `json:"user_id"`
	ProviderID int64       `json:"provider_id"`
	Subject    string      `json:"provider_user_id"`
	CreatedAt  historyTime `json:"created_at"`
}

type historyOAuthProvider struct {
	ID                    int64       `json:"id"`
	Name                  string      `json:"name"`
	Slug                  string      `json:"slug"`
	Icon                  string      `json:"icon"`
	Enabled               bool        `json:"enabled"`
	ClientID              string      `json:"client_id"`
	ClientSecret          string      `json:"client_secret"`
	AuthorizationEndpoint string      `json:"authorization_endpoint"`
	TokenEndpoint         string      `json:"token_endpoint"`
	UserInfoEndpoint      string      `json:"user_info_endpoint"`
	Scopes                string      `json:"scopes"`
	UserIDField           string      `json:"user_id_field"`
	UsernameField         string      `json:"username_field"`
	DisplayNameField      string      `json:"display_name_field"`
	EmailField            string      `json:"email_field"`
	WellKnown             string      `json:"well_known"`
	AuthStyle             int         `json:"auth_style"`
	AccessPolicy          string      `json:"access_policy"`
	AccessDeniedMessage   string      `json:"access_denied_message"`
	CreatedAt             historyTime `json:"created_at"`
	UpdatedAt             historyTime `json:"updated_at"`
}

func (m *Importer) importHistoryIdentity(ctx context.Context, target pgx.Tx, d *historyData) error {
	for _, p := range d.passkeys {
		credential, err := json.Marshal(p.Credential)
		if err != nil {
			return err
		}
		var lastUsed any
		if p.LastUsedAt != nil {
			lastUsed = historyDate(*p.LastUsedAt)
		}
		var identical bool
		err = target.QueryRow(ctx, `INSERT INTO v3_identity.passkeys AS p
			(credential_id,user_id,credential,created_at,last_used_at,legacy_id,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(credential_id) DO UPDATE SET credential_id=EXCLUDED.credential_id
			RETURNING p.user_id=$2 AND p.credential=$3::jsonb AND p.created_at=$4 AND p.last_used_at IS NOT DISTINCT FROM $5 AND p.legacy_id=$6 AND p.updated_at=$7`, p.Credential.ID, p.UserID, credential,
			historyDate(p.CreatedAt), lastUsed, p.ID, historyDate(p.UpdatedAt)).Scan(&identical)
		if err != nil {
			return fmt.Errorf("legacy: import passkey %d: %w", p.ID, err)
		}
		if !identical {
			return fmt.Errorf("legacy: passkey %d target conflict", p.ID)
		}
	}
	providers := map[int64]string{}
	for _, p := range d.providers {
		providers[p.ID] = p.Slug
		ciphertext, err := m.crypto.Encrypt([]byte(p.ClientSecret))
		if err != nil {
			return err
		}
		var existing []byte
		err = target.QueryRow(ctx, `SELECT secret_ciphertext FROM v3_identity.oauth_providers WHERE id=$1`, p.ID).Scan(&existing)
		if err == nil {
			decrypter, ok := m.crypto.(interface{ Decrypt([]byte) ([]byte, error) })
			if !ok {
				return fmt.Errorf("legacy: decrypting existing OAuth provider secrets requires a decrypter")
			}
			plaintext, decryptErr := decrypter.Decrypt(existing)
			if decryptErr != nil || string(plaintext) != p.ClientSecret {
				return fmt.Errorf("legacy: OAuth provider %d secret conflict", p.ID)
			}
			ciphertext = existing
		} else if err != pgx.ErrNoRows {
			return err
		}
		columns := []string{"id", "name", "slug", "icon", "enabled", "client_id", "secret_ciphertext", "authorization_endpoint", "token_endpoint", "user_info_endpoint", "scopes", "user_id_field", "username_field", "display_name_field", "email_field", "well_known", "auth_style", "access_policy", "access_denied_message", "created_at", "updated_at"}
		values := []any{p.ID, p.Name, p.Slug, p.Icon, p.Enabled, p.ClientID, ciphertext, p.AuthorizationEndpoint, p.TokenEndpoint, p.UserInfoEndpoint, p.Scopes, p.UserIDField, p.UsernameField, p.DisplayNameField, p.EmailField, p.WellKnown, p.AuthStyle, p.AccessPolicy, p.AccessDeniedMessage, historyDate(p.CreatedAt), historyDate(p.UpdatedAt)}
		if err = insertHistoryExact(ctx, target, "v3_identity", "oauth_providers", "id", columns, values); err != nil {
			return fmt.Errorf("legacy: import OAuth provider %d: %w", p.ID, err)
		}
	}
	for _, b := range d.bindings {
		var identical bool
		err := target.QueryRow(ctx, `INSERT INTO v3_identity.user_identities AS i(provider,subject,user_id,created_at,legacy_binding_id)
			VALUES($1,$2,$3,$4,$5) ON CONFLICT(provider,subject) DO UPDATE SET
			legacy_binding_id=COALESCE(i.legacy_binding_id,EXCLUDED.legacy_binding_id),created_at=LEAST(i.created_at,EXCLUDED.created_at)
			RETURNING i.user_id=$3 AND (i.legacy_binding_id IS NULL OR i.legacy_binding_id=$5)`,
			providers[b.ProviderID], b.Subject, b.UserID, historyDate(b.CreatedAt), b.ID).Scan(&identical)
		if err != nil {
			return fmt.Errorf("legacy: import OAuth binding %d: %w", b.ID, err)
		}
		if !identical {
			return fmt.Errorf("legacy: OAuth binding %d identity collision", b.ID)
		}
	}
	return nil
}

// Every column is compared on retry. A duplicate key with changed semantics is
// a conflict, not permission to silently replace an already-used target row.
func insertHistoryExact(ctx context.Context, target pgx.Tx, schema, table, key string, columns []string, values []any) error {
	quoted, placeholders, equal := make([]string, len(columns)), make([]string, len(columns)), make([]string, len(columns))
	for i, c := range columns {
		quoted[i] = pgx.Identifier{c}.Sanitize()
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		equal[i] = "h." + quoted[i] + " IS NOT DISTINCT FROM " + placeholders[i]
	}
	name := pgx.Identifier{schema, table}.Sanitize()
	qkey := pgx.Identifier{key}.Sanitize()
	query := "INSERT INTO " + name + " AS h (" + strings.Join(quoted, ",") + ") VALUES(" + strings.Join(placeholders, ",") + ") ON CONFLICT(" + qkey + ") DO UPDATE SET " + qkey + "=EXCLUDED." + qkey + " RETURNING " + strings.Join(equal, " AND ")
	var identical bool
	if err := target.QueryRow(ctx, query, values...).Scan(&identical); err != nil {
		return err
	}
	if !identical {
		return fmt.Errorf("historical %s row conflicts with target", table)
	}
	return nil
}
