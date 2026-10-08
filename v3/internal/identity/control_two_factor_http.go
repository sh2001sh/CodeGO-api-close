package identity

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

func (c *Control) registerSecurityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/user/login/2fa", c.finishTwoFactorLoginHTTP)
	mux.HandleFunc("GET /api/user/2fa/status", c.twoFactorStatusHTTP)
	mux.HandleFunc("POST /api/user/2fa/setup", c.setupTwoFactorHTTP)
	mux.HandleFunc("POST /api/user/2fa/enable", c.twoFactorActionHTTP)
	mux.HandleFunc("POST /api/user/2fa/disable", c.twoFactorActionHTTP)
	mux.HandleFunc("POST /api/user/2fa/backup_codes", c.twoFactorActionHTTP)
	mux.HandleFunc("GET /api/user/2fa/stats", c.twoFactorStatsHTTP)
	mux.HandleFunc("DELETE /api/user/{id}/2fa", c.adminDisableTwoFactorHTTP)
	mux.HandleFunc("GET /api/verification", c.emailVerificationHTTP)
	mux.HandleFunc("POST /api/user/email/verify", c.verifyEmailHTTP)
	mux.HandleFunc("POST /api/oauth/email/bind", c.verifyEmailHTTP)
	mux.HandleFunc("GET /api/reset_password", c.passwordResetEmailHTTP)
	mux.HandleFunc("POST /api/user/reset", c.passwordResetHTTP)
}

func (c *Control) beginTwoFactorLoginHTTP(w http.ResponseWriter, r *http.Request, u User) {
	token, err := randomToken()
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	hash := sha256.Sum256([]byte(token))
	_, err = c.pool.Exec(r.Context(), `INSERT INTO v3_identity.login_challenges(token_hash,user_id,expires_at) VALUES($1,$2,$3)`, hash[:], u.ID, c.cfg.Now().Add(5*time.Minute))
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	if previous := requestToken(r); previous != "" {
		if err = c.Logout(r.Context(), previous); err != nil && !errors.Is(err, ErrCredentials) {
			c.reply(w, nil, err)
			return
		}
	}
	c.clearSessionCookies(w)
	c.pendingFactorCookie(w, token)
	c.reply(w, struct {
		RequireTwoFactor bool   `json:"require_2fa"`
		ChallengeToken   string `json:"challenge_token"`
	}{true, token}, nil)
}

func (c *Control) pendingFactorCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: "codego_pending_2fa", Value: token, Path: "/api/user/login/2fa", HttpOnly: true, Secure: len(c.cfg.PublicURL) >= 8 && c.cfg.PublicURL[:8] == "https://", SameSite: http.SameSiteStrictMode, Expires: c.cfg.Now().Add(5 * time.Minute)})
}

func (c *Control) FinishTwoFactorLogin(ctx context.Context, token, code string) (Session, error) {
	if len(token) != 43 || len(code) > 32 {
		return Session{}, ErrCredentials
	}
	hash := sha256.Sum256([]byte(token))
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var uid int64
	err = tx.QueryRow(ctx, `SELECT ch.user_id FROM v3_identity.login_challenges ch JOIN v3_identity.users u ON u.id=ch.user_id
	 WHERE ch.token_hash=$1 AND ch.expires_at>$2 AND u.status='active' AND u.deleted_at IS NULL`, hash[:], c.cfg.Now()).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrCredentials
	}
	if err != nil {
		return Session{}, err
	}
	err = c.verifyTwoFactorTx(ctx, tx, uid, code, true, true)
	if errors.Is(err, ErrCredentials) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return Session{}, commitErr
		}
		return Session{}, err
	}
	if err != nil {
		return Session{}, err
	}
	// Factor row lock serializes challenges; conditional deletion rejects challenge replay.
	tag, err := tx.Exec(ctx, `DELETE FROM v3_identity.login_challenges WHERE token_hash=$1 AND expires_at>$2`, hash[:], c.cfg.Now())
	if err != nil {
		return Session{}, err
	}
	if tag.RowsAffected() != 1 {
		return Session{}, ErrCredentials
	}
	u, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM v3_identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL`, uid))
	if err != nil {
		return Session{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	if _, err = c.pool.Exec(ctx, `UPDATE v3_identity.users SET last_login_at=$2 WHERE id=$1`, uid, c.cfg.Now()); err != nil {
		return Session{}, err
	}
	return c.NewSession(ctx, u)
}

func (c *Control) finishTwoFactorLoginHTTP(w http.ResponseWriter, r *http.Request) {
	if !c.allowLogin(w, r) {
		return
	}
	var in struct {
		Code           string `json:"code"`
		ChallengeToken string `json:"challenge_token,omitempty"`
	}
	if err := decodeControl(w, r, &in); err != nil {
		c.reply(w, nil, err)
		return
	}
	if in.ChallengeToken == "" {
		if cookie, err := r.Cookie("codego_pending_2fa"); err == nil {
			in.ChallengeToken = cookie.Value
		}
	}
	s, err := c.FinishTwoFactorLogin(r.Context(), in.ChallengeToken, in.Code)
	if err == nil {
		c.setSession(w, s)
		http.SetCookie(w, &http.Cookie{Name: "codego_pending_2fa", Value: "", Path: "/api/user/login/2fa", HttpOnly: true, MaxAge: -1})
	}
	c.reply(w, s, err)
}

func (c *Control) twoFactorStatusHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	data, err := c.TwoFactorStatus(r.Context(), u.ID)
	c.reply(w, data, err)
}
func (c *Control) setupTwoFactorHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	if !c.allowLogin(w, r) {
		return
	}
	data, err := c.SetupTwoFactor(r.Context(), u)
	c.reply(w, data, err)
}
func (c *Control) twoFactorActionHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	if !c.allowLogin(w, r) {
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if err := decodeControl(w, r, &in); err != nil {
		c.reply(w, nil, err)
		return
	}
	var err error
	var data any
	switch r.URL.Path {
	case "/api/user/2fa/enable":
		err = c.EnableTwoFactor(r.Context(), u.ID, in.Code)
	case "/api/user/2fa/disable":
		err = c.DisableTwoFactor(r.Context(), u.ID, in.Code)
	default:
		var codes []string
		codes, err = c.RegenerateTwoFactorBackupCodes(r.Context(), u.ID, in.Code)
		data = struct {
			BackupCodes []string `json:"backup_codes"`
		}{codes}
	}
	if err == nil && r.URL.Path != "/api/user/2fa/backup_codes" {
		c.clearSessionCookies(w)
	}
	c.reply(w, data, err)
}
func (c *Control) twoFactorStatsHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	if !u.IsAdmin() {
		c.reply(w, nil, ErrForbidden)
		return
	}
	var total, enabled int64
	err := c.pool.QueryRow(r.Context(), `SELECT count(*),count(*) FILTER(WHERE coalesce(t.enabled,false)) FROM v3_identity.users u LEFT JOIN v3_identity.two_factor t ON t.user_id=u.id WHERE u.deleted_at IS NULL`).Scan(&total, &enabled)
	rate := 0.0
	if total > 0 {
		rate = float64(enabled) / float64(total) * 100
	}
	c.reply(w, struct {
		Total   int64  `json:"total_users"`
		Enabled int64  `json:"enabled_users"`
		Rate    string `json:"enabled_rate"`
	}{total, enabled, fmt.Sprintf("%.1f%%", rate)}, err)
}
func (c *Control) adminDisableTwoFactorHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	c.reply(w, nil, c.AdminDisableTwoFactor(r.Context(), u, parseID(r.PathValue("id"))))
}
