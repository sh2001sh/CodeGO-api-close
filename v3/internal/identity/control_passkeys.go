package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
)

type passkeyUser struct {
	User
	Credentials []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return []byte(strconv.FormatInt(u.ID, 10)) }
func (u passkeyUser) WebAuthnName() string                       { return u.Username }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.DisplayName }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }

func (c *Control) webauthn() (*webauthn.WebAuthn, error) {
	u, err := url.Parse(c.cfg.PublicURL)
	if err != nil || u.Hostname() == "" {
		return nil, ErrForbidden
	}
	return webauthn.New(&webauthn.Config{RPID: u.Hostname(), RPDisplayName: "CodeGo", RPOrigins: []string{u.Scheme + "://" + u.Host},
		AuthenticatorSelection: protocol.AuthenticatorSelection{UserVerification: protocol.VerificationRequired},
		Timeouts:               webauthn.TimeoutsConfig{Login: webauthn.TimeoutConfig{Enforce: true, Timeout: 5 * time.Minute}, Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: 5 * time.Minute}}})
}

type passkeyQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadPasskeyUser(ctx context.Context, q passkeyQuerier, uid int64) (passkeyUser, error) {
	u, err := scanUser(q.QueryRow(ctx, `SELECT `+userColumns+` FROM v3_identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL`, uid))
	if err != nil {
		return passkeyUser{}, err
	}
	rows, err := q.Query(ctx, `SELECT credential FROM v3_identity.passkeys WHERE user_id=$1`, uid)
	if err != nil {
		return passkeyUser{}, err
	}
	defer rows.Close()
	out := passkeyUser{User: u}
	for rows.Next() {
		var b []byte
		var credential webauthn.Credential
		if err := rows.Scan(&b); err != nil {
			return passkeyUser{}, err
		}
		if err = json.Unmarshal(b, &credential); err != nil {
			return passkeyUser{}, err
		}
		out.Credentials = append(out.Credentials, credential)
	}
	return out, rows.Err()
}

func (c *Control) saveCeremony(ctx context.Context, purpose string, uid *int64, session *webauthn.SessionData) (string, error) {
	state, err := randomToken()
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(state))
	b, err := json.Marshal(session)
	if err != nil {
		return "", err
	}
	_, err = c.pool.Exec(ctx, `INSERT INTO v3_identity.passkey_ceremonies(state_hash,purpose,user_id,session_data,expires_at) VALUES ($1,$2,$3,$4,$5)`, hash[:], purpose, uid, b, c.cfg.Now().Add(5*time.Minute))
	return state, err
}

func (c *Control) consumeCeremony(ctx context.Context, state, purpose string, uid *int64) (webauthn.SessionData, error) {
	if len(state) != 43 {
		return webauthn.SessionData{}, ErrCredentials
	}
	hash := sha256.Sum256([]byte(state))
	var b []byte
	err := c.pool.QueryRow(ctx, `DELETE FROM v3_identity.passkey_ceremonies WHERE state_hash=$1 AND purpose=$2 AND user_id IS NOT DISTINCT FROM $3::bigint AND expires_at>$4 RETURNING session_data`, hash[:], purpose, uid, c.cfg.Now()).Scan(&b)
	if err != nil {
		return webauthn.SessionData{}, controlDBError(err)
	}
	var session webauthn.SessionData
	err = json.Unmarshal(b, &session)
	return session, err
}

func (c *Control) passkeyRegistrationBegin(w http.ResponseWriter, r *http.Request) {
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
	options, session, err := wa.BeginRegistration(user, webauthn.WithExclusions(webauthn.Credentials(user.Credentials).CredentialDescriptors()))
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	state, err := c.saveCeremony(r.Context(), "registration", &u.ID, session)
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	c.passkeyCookie(w, state)
	c.reply(w, options, nil)
}

func (c *Control) passkeyRegistrationFinish(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	state, err := r.Cookie("codego_passkey_state")
	if err != nil {
		c.reply(w, nil, ErrCredentials)
		return
	}
	session, err := c.consumeCeremony(r.Context(), state.Value, "registration", &u.ID)
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	user, err := loadPasskeyUser(r.Context(), c.pool, u.ID)
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	wa, err := c.webauthn()
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	credential, err := wa.FinishRegistration(user, session, r)
	if err != nil {
		c.reply(w, nil, ErrCredentials)
		return
	}
	b, err := json.Marshal(credential)
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	_, err = c.pool.Exec(r.Context(), `INSERT INTO v3_identity.passkeys(credential_id,user_id,credential) VALUES ($1,$2,$3)`, credential.ID, u.ID, b)
	c.reply(w, nil, controlDBError(err))
}

func (c *Control) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if !c.allowLogin(w, r) {
		return
	}
	wa, err := c.webauthn()
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	options, session, err := wa.BeginDiscoverableLogin()
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	state, err := c.saveCeremony(r.Context(), "login", nil, session)
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	c.passkeyCookie(w, state)
	c.reply(w, options, nil)
}

func (c *Control) passkeyCookie(w http.ResponseWriter, state string) {
	u, _ := url.Parse(c.cfg.PublicURL)
	http.SetCookie(w, &http.Cookie{Name: "codego_passkey_state", Value: state, Path: "/api", HttpOnly: true, Secure: u.Scheme == "https", SameSite: http.SameSiteLaxMode, Expires: c.cfg.Now().Add(5 * time.Minute)})
}

func (c *Control) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if !c.allowLogin(w, r) {
		return
	}
	state, err := r.Cookie("codego_passkey_state")
	if err != nil {
		c.reply(w, nil, ErrCredentials)
		return
	}
	session, err := c.consumeCeremony(r.Context(), state.Value, "login", nil)
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
	authenticated, err := c.finishDiscoverablePasskeyLogin(w, r, tx, wa, session)
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		c.reply(w, nil, err)
		return
	}
	s, err := c.NewSession(r.Context(), authenticated)
	if err == nil {
		c.setSession(w, s)
	}
	c.reply(w, s, err)
}

// finishDiscoverablePasskeyLogin resolves the discoverable WebAuthn
// assertion in r to a user (locking that user's passkey rows to serialize
// authenticator counter updates), verifies it against session, and persists
// the refreshed credential. It does not commit tx.
func (c *Control) finishDiscoverablePasskeyLogin(w http.ResponseWriter, r *http.Request, tx pgx.Tx, wa *webauthn.WebAuthn, session webauthn.SessionData) (User, error) {
	var authenticated User
	resolver := func(rawID, handle []byte) (webauthn.User, error) {
		var uid int64
		// Serialize authenticator counters so a racing assertion cannot write
		// an old counter over a newer one.
		err := tx.QueryRow(r.Context(), `SELECT user_id FROM v3_identity.passkeys WHERE credential_id=$1 FOR UPDATE`, rawID).Scan(&uid)
		if err != nil {
			return nil, err
		}
		user, err := loadPasskeyUser(r.Context(), tx, uid)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(handle, user.WebAuthnID()) {
			return nil, ErrCredentials
		}
		authenticated = user.User
		return user, nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	_, credential, err := wa.FinishPasskeyLogin(resolver, session, r)
	if err != nil || credential.Authenticator.CloneWarning {
		return User{}, ErrCredentials
	}
	b, err := json.Marshal(credential)
	if err != nil {
		return User{}, err
	}
	_, err = tx.Exec(r.Context(), `UPDATE v3_identity.passkeys SET credential=$2,last_used_at=now() WHERE credential_id=$1`, credential.ID, b)
	if err != nil {
		return User{}, err
	}
	return authenticated, nil
}

func (c *Control) passkeyStatus(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	var count int
	err := c.pool.QueryRow(r.Context(), `SELECT count(*) FROM v3_identity.passkeys WHERE user_id=$1`, u.ID).Scan(&count)
	c.reply(w, struct {
		Enabled bool `json:"enabled"`
		Count   int  `json:"count"`
	}{count > 0, count}, err)
}
