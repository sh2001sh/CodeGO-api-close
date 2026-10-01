package oidc

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type user struct {
	Subject       string
	Username      string
	Name          string
	Email         string
	EmailVerified bool
}

func loadUser(ctx context.Context, tx pgx.Tx, uid int64) (user, error) {
	var u user
	err := tx.QueryRow(ctx, `SELECT COALESCE(external_id,''),username,display_name,COALESCE(email,''),
	 email_verified FROM v3_identity.users
	 WHERE id=$1 AND status='active' AND deleted_at IS NULL FOR UPDATE`, uid).
		Scan(&u.Subject, &u.Username, &u.Name, &u.Email, &u.EmailVerified)
	if errors.Is(err, pgx.ErrNoRows) {
		return user{}, errGrant
	}
	return u, err
}

// assignSubject runs while the user row is locked; six-character subjects retain
// the existing community API contract. A savepoint permits rare collision retries.
func assignSubject(ctx context.Context, tx pgx.Tx, uid int64) (string, error) {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	for attempt := 0; attempt < 8; attempt++ {
		var raw [6]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "", err
		}
		for i := range raw {
			raw[i] = alphabet[raw[i]&31]
		}
		if _, err := tx.Exec(ctx, "SAVEPOINT oidc_subject"); err != nil {
			return "", err
		}
		_, err := tx.Exec(ctx, `UPDATE v3_identity.users SET external_id=$2 WHERE id=$1`, uid, string(raw[:]))
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if _, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT oidc_subject"); err != nil {
				return "", err
			}
			if _, err = tx.Exec(ctx, "RELEASE SAVEPOINT oidc_subject"); err != nil {
				return "", err
			}
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, "RELEASE SAVEPOINT oidc_subject"); err != nil {
			return "", err
		}
		return string(raw[:]), nil
	}
	return "", errors.New("oidc: unable to allocate unique external identity")
}

func userClaims(u user, scope string) map[string]any {
	claims := map[string]any{"sub": u.Subject}
	if hasScope(scope, "profile") {
		claims["preferred_username"] = u.Username
		name := u.Name
		if name == "" {
			name = u.Username
		}
		claims["name"] = name
	}
	if hasScope(scope, "email") && strings.TrimSpace(u.Email) != "" {
		claims["email"] = u.Email
		claims["email_verified"] = u.EmailVerified
	}
	return claims
}

func (s *Server) userinfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !validChallenge(parts[1]) {
		invalidToken(w)
		return
	}
	u, scope, err := s.accessUser(r.Context(), parts[1])
	if errors.Is(err, errToken) {
		invalidToken(w)
		return
	}
	if err != nil {
		s.log.Error("oidc userinfo failed", "error", err)
		oauthError(w, http.StatusInternalServerError, "server_error")
		return
	}
	writeJSON(w, http.StatusOK, userClaims(u, scope))
}

func (s *Server) accessUser(ctx context.Context, token string) (user, string, error) {
	var u user
	var scope string
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(u.external_id,''),u.username,u.display_name,COALESCE(u.email,''),
	 u.email_verified,t.scope
	 FROM v3_identity.oidc_tokens t JOIN v3_identity.users u ON u.id=t.user_id
	 WHERE t.token_hash=$1 AND t.client_id=$2 AND t.expires_at>$3 AND t.revoked_at IS NULL
	 AND u.status='active' AND u.deleted_at IS NULL`, digest(token), s.cfg.ClientID, s.cfg.Now()).
		Scan(&u.Subject, &u.Username, &u.Name, &u.Email, &u.EmailVerified, &scope)
	if errors.Is(err, pgx.ErrNoRows) {
		return user{}, "", errToken
	}
	return u, scope, err
}

func invalidToken(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	oauthError(w, http.StatusUnauthorized, "invalid_token")
}
