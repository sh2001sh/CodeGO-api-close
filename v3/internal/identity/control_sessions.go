package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Session struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	User         User      `json:"user"`
}

type sessionClaims struct {
	SessionID string `json:"sid"`
	UserID    int64  `json:"sub"`
	Expires   int64  `json:"exp"`
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (c *Control) accessToken(sid string, uid int64, expires time.Time) (string, error) {
	b, err := json.Marshal(sessionClaims{sid, uid, expires.Unix()})
	if err != nil {
		return "", err
	}
	unsigned := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." + base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, c.cfg.SessionSecret)
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (c *Control) parseToken(token string) (sessionClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9" {
		return sessionClaims{}, ErrCredentials
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return sessionClaims{}, ErrCredentials
	}
	mac := hmac.New(sha256.New, c.cfg.SessionSecret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return sessionClaims{}, ErrCredentials
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return sessionClaims{}, ErrCredentials
	}
	var claims sessionClaims
	if json.Unmarshal(b, &claims) != nil || claims.UserID <= 0 || claims.SessionID == "" || claims.Expires <= c.cfg.Now().Unix() {
		return sessionClaims{}, ErrCredentials
	}
	return claims, nil
}

func (c *Control) NewSession(ctx context.Context, u User) (Session, error) {
	sid, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	refresh, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	hash := sha256.Sum256([]byte(refresh))
	_, err = c.pool.Exec(ctx, `INSERT INTO v3_identity.sessions(id,user_id,refresh_hash,expires_at) VALUES ($1,$2,$3,$4)`, sid, u.ID, hash[:], c.cfg.Now().Add(c.cfg.RefreshTTL))
	if err != nil {
		return Session{}, err
	}
	expires := c.cfg.Now().Add(c.cfg.AccessTTL)
	access, err := c.accessToken(sid, u.ID, expires)
	return Session{access, refresh, expires, u}, err
}

func (c *Control) Authenticate(ctx context.Context, token string) (User, error) {
	claims, err := c.parseToken(token)
	if err != nil {
		return User{}, err
	}
	u, err := scanUser(c.pool.QueryRow(ctx, `SELECT u.id,u.username,u.display_name,coalesce(u.email,''),u.role,u.status,u.group_name,
		coalesce((SELECT greatest(a.balance,0) FROM v3_billing.accounts a WHERE a.owner_type='user' AND a.owner_id=u.id AND a.kind='affiliate'),0)
		FROM v3_identity.users u JOIN v3_identity.sessions s ON s.user_id=u.id
		WHERE s.id=$1 AND u.id=$2 AND s.revoked_at IS NULL AND s.expires_at>$3 AND u.status='active' AND u.deleted_at IS NULL`, claims.SessionID, claims.UserID, c.cfg.Now()))
	if errors.Is(err, ErrNotFound) {
		return User{}, ErrCredentials
	}
	return u, err
}

func (c *Control) Refresh(ctx context.Context, refresh string) (Session, error) {
	if len(refresh) != 43 {
		return Session{}, ErrCredentials
	}
	hash := sha256.Sum256([]byte(refresh))
	replacement, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	nextHash := sha256.Sum256([]byte(replacement))
	var sid string
	var uid int64
	// Conditional UPDATE is the refresh rotation: only one racing request wins.
	err = c.pool.QueryRow(ctx, `UPDATE v3_identity.sessions s SET refresh_hash=$2,expires_at=$3
		FROM v3_identity.users u WHERE s.refresh_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>$4
		AND u.id=s.user_id AND u.status='active' AND u.deleted_at IS NULL RETURNING s.id,s.user_id`, hash[:], nextHash[:], c.cfg.Now().Add(c.cfg.RefreshTTL), c.cfg.Now()).Scan(&sid, &uid)
	if errors.Is(controlDBError(err), ErrNotFound) {
		return Session{}, ErrCredentials
	}
	if err != nil {
		return Session{}, err
	}
	u, err := c.User(ctx, uid)
	if err != nil {
		return Session{}, err
	}
	expires := c.cfg.Now().Add(c.cfg.AccessTTL)
	access, err := c.accessToken(sid, uid, expires)
	return Session{access, replacement, expires, u}, err
}

func (c *Control) Logout(ctx context.Context, token string) error {
	claims, err := c.parseToken(token)
	if err != nil {
		return err
	}
	_, err = c.pool.Exec(ctx, `UPDATE v3_identity.sessions SET revoked_at=$3 WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, claims.SessionID, claims.UserID, c.cfg.Now())
	return err
}
