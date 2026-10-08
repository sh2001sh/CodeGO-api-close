package identity

import (
	"context"
	"errors"
	"net/mail"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

type RegisterInput struct {
	Username               string `json:"username"`
	Password               string `json:"password"`
	DisplayName            string `json:"display_name"`
	Email                  string `json:"email"`
	VerificationCode       string `json:"verification_code,omitempty"`
	AffiliateCode          string `json:"aff_code,omitempty"`
	AcceptedTermsVersion   string `json:"accepted_terms_version,omitempty"`
	AcceptedPrivacyVersion string `json:"accepted_privacy_version,omitempty"`
	AgreementLocale        string `json:"agreement_locale,omitempty"`
}

func validUsername(v string) bool { return regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,32}$`).MatchString(v) }

func validateRegistration(in RegisterInput) error {
	if !validUsername(in.Username) || len(in.Password) < 10 || len(in.Password) > 72 || len(in.DisplayName) > 100 || len(in.Email) > 254 {
		return ErrInvalidInput
	}
	if in.Email != "" {
		if _, err := mail.ParseAddress(in.Email); err != nil {
			return ErrInvalidInput
		}
	}
	return nil
}

func (c *Control) Register(ctx context.Context, in RegisterInput) (User, error) {
	if c.cfg.DisableRegistration {
		return User{}, ErrForbidden
	}
	in.Username = strings.TrimSpace(in.Username)
	if err := validateRegistration(in); err != nil {
		return User{}, err
	}
	if err := validateRegistrationPolicies(in, false); err != nil {
		return User{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	return c.registerWithEmailProof(ctx, in, string(hash))
}

const userColumns = `id,username,display_name,coalesce(email,''),role,status,group_name,
 coalesce((SELECT greatest(a.balance,0) FROM v3_billing.accounts a WHERE a.owner_type='user' AND a.owner_id=v3_identity.users.id AND a.kind='affiliate'),0)`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Role, &u.Status, &u.Group, &u.AffiliateMicroCredits)
	return u, controlDBError(err)
}

func (c *Control) User(ctx context.Context, id int64) (User, error) {
	return scanUser(c.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM v3_identity.users WHERE id=$1 AND deleted_at IS NULL`, id))
}

func (c *Control) Login(ctx context.Context, username, password string) (User, error) {
	var u User
	var hash string
	err := c.pool.QueryRow(ctx, `SELECT `+userColumns+`,coalesce(password_hash,'') FROM v3_identity.users WHERE username=$1 AND deleted_at IS NULL`, username).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Role, &u.Status, &u.Group, &u.AffiliateMicroCredits, &hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return User{}, err
	}
	// Always run bcrypt, even for unknown users, to avoid a timing oracle.
	if hash == "" {
		hash = "$2a$10$7EqJtq98hPqEX7fNZaFWoOHiVbWpHM.xCE2MoRZmhu/TDIiFmXDRK"
	}
	check := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil || check != nil || u.Status != "active" {
		return User{}, ErrCredentials
	}
	var enabled bool
	err = c.pool.QueryRow(ctx, `SELECT coalesce((SELECT enabled FROM v3_identity.two_factor WHERE user_id=$1),false)`, u.ID).Scan(&enabled)
	if err != nil {
		return User{}, err
	}
	if enabled {
		return u, ErrSecondFactorRequired
	}
	_, err = c.pool.Exec(ctx, `UPDATE v3_identity.users SET last_login_at=now() WHERE id=$1`, u.ID)
	return u, err
}

type UserUpdate struct {
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	Group       string `json:"group"`
}

func (c *Control) UpdateUser(ctx context.Context, actor User, id int64, in UserUpdate) (User, error) {
	if !actor.IsAdmin() {
		return User{}, ErrForbidden
	}
	if len(in.DisplayName) > 100 || len(in.Email) > 254 || in.Group == "" || len(in.Group) > 100 {
		return User{}, ErrInvalidInput
	}
	if in.Role != "user" && in.Role != "admin" && in.Role != "root" {
		return User{}, ErrInvalidInput
	}
	if in.Status != "active" && in.Status != "disabled" {
		return User{}, ErrInvalidInput
	}
	if in.Email != "" {
		if _, err := mail.ParseAddress(in.Email); err != nil {
			return User{}, ErrInvalidInput
		}
	}
	// Only root may grant or edit admin accounts. A root cannot lock itself out.
	if actor.Role != "root" && in.Role != "user" {
		return User{}, ErrForbidden
	}
	if actor.ID == id && (in.Role != actor.Role || in.Status != "active") {
		return User{}, ErrForbidden
	}
	return scanUser(c.pool.QueryRow(ctx, `UPDATE v3_identity.users SET display_name=$2,email=NULLIF($3,''),role=$4,status=$5,group_name=$6,
	 email_verified=CASE WHEN email IS DISTINCT FROM NULLIF($3,'') THEN false ELSE email_verified END
		WHERE id=$1 AND deleted_at IS NULL AND ($7='root' OR role='user') RETURNING `+userColumns, id, in.DisplayName, in.Email, in.Role, in.Status, in.Group, actor.Role))
}

func (c *Control) ListUsers(ctx context.Context, actor User, before int64, limit int) ([]User, error) {
	if !actor.IsAdmin() {
		return nil, ErrForbidden
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := c.pool.Query(ctx, `SELECT `+userColumns+` FROM v3_identity.users WHERE deleted_at IS NULL AND ($1::bigint=0 OR id<$1) ORDER BY id DESC LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func controlDBError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var e *pgconn.PgError
	if errors.As(err, &e) && e.Code == "23505" {
		return ErrDuplicate
	}
	return err
}
