package identity

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
)

func (c *Control) passkeyVerifyBegin(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	wa, err := c.webauthn()
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	user, err := loadPasskeyUser(r.Context(), c.pool, u.ID)
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	options, session, err := wa.BeginLogin(user)
	if err != nil {
		c.reply(w, nil, ErrCredentials)
		return
	}
	state, err := c.saveCeremony(r.Context(), "verification", &u.ID, session)
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	c.passkeyCookie(w, state)
	c.reply(w, options, nil)
}

func (c *Control) passkeyVerifyFinish(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	claims, err := c.parseToken(requestToken(r))
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	state, err := r.Cookie("codego_passkey_state")
	if err != nil {
		c.reply(w, nil, ErrCredentials)
		return
	}
	session, err := c.consumeCeremony(r.Context(), state.Value, "verification", &u.ID)
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	wa, err := c.webauthn()
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	tx, err := c.pool.Begin(r.Context())
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	credential, err := c.finishPasskeyLogin(w, r, tx, wa, u.ID, session)
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	proof, err := c.persistPasskeyVerification(r.Context(), tx, credential, u.ID, claims.SessionID)
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	origin, _ := url.Parse(c.cfg.PublicURL)
	http.SetCookie(w, &http.Cookie{Name: "codego_passkey_verified", Value: proof, Path: "/api/user/passkey", HttpOnly: true, Secure: origin.Scheme == "https", SameSite: http.SameSiteStrictMode, Expires: c.cfg.Now().Add(2 * time.Minute)})
	c.reply(w, struct {
		Verified bool `json:"verified"`
	}{true}, nil)
}

// finishPasskeyLogin locks the user's passkey rows, loads them, and verifies
// the WebAuthn assertion in r against session. It returns ErrCredentials for
// a failed assertion or a cloned authenticator, matching the prior inline
// behavior exactly.
func (c *Control) finishPasskeyLogin(w http.ResponseWriter, r *http.Request, tx pgx.Tx, wa *webauthn.WebAuthn, uid int64, session webauthn.SessionData) (webauthn.Credential, error) {
	rows, err := tx.Query(r.Context(), `SELECT credential_id FROM v3_identity.passkeys WHERE user_id=$1 ORDER BY credential_id FOR UPDATE`, uid)
	if err != nil {
		return webauthn.Credential{}, err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return webauthn.Credential{}, err
	}
	user, err := loadPasskeyUser(r.Context(), tx, uid)
	if err != nil {
		return webauthn.Credential{}, err
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	credential, err := wa.FinishLogin(user, session, r)
	if err != nil || credential.Authenticator.CloneWarning {
		return webauthn.Credential{}, ErrCredentials
	}
	return *credential, nil
}

// persistPasskeyVerification records the refreshed credential, stores a
// short-lived verification proof, and commits tx. The returned proof is the
// raw token to set in the verified cookie.
func (c *Control) persistPasskeyVerification(ctx context.Context, tx pgx.Tx, credential webauthn.Credential, uid int64, sessionID string) (string, error) {
	b, err := json.Marshal(credential)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_identity.passkeys SET credential=$2,last_used_at=now() WHERE credential_id=$1`, credential.ID, b)
	if err != nil {
		return "", err
	}
	proof, err := randomToken()
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(proof))
	_, err = tx.Exec(ctx, `INSERT INTO v3_identity.passkey_verifications(proof_hash,user_id,session_id,expires_at) VALUES($1,$2,$3,$4)`, hash[:], uid, sessionID, c.cfg.Now().Add(2*time.Minute))
	if err != nil {
		return "", err
	}
	return proof, tx.Commit(ctx)
}

func (c *Control) passkeyDeleteHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	claims, err := c.parseToken(requestToken(r))
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	proof, err := r.Cookie("codego_passkey_verified")
	if err != nil {
		c.reply(w, nil, ErrForbidden)
		return
	}
	hash := sha256.Sum256([]byte(proof.Value))
	tx, err := c.pool.Begin(r.Context())
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	tag, err := tx.Exec(r.Context(), `DELETE FROM v3_identity.passkey_verifications WHERE proof_hash=$1 AND user_id=$2 AND session_id=$3 AND expires_at>$4`, hash[:], u.ID, claims.SessionID, c.cfg.Now())
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	if tag.RowsAffected() == 0 {
		c.reply(w, nil, ErrForbidden)
		return
	}
	var alternative bool
	err = tx.QueryRow(r.Context(), `SELECT password_hash IS NOT NULL OR EXISTS(SELECT 1 FROM v3_identity.user_identities WHERE user_id=$1) FROM v3_identity.users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&alternative)
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	if !alternative {
		c.reply(w, nil, ErrForbidden)
		return
	}
	_, err = tx.Exec(r.Context(), `DELETE FROM v3_identity.passkeys WHERE user_id=$1`, u.ID)
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	c.reply(w, nil, tx.Commit(r.Context()))
}
