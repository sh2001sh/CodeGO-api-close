package identity

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

func (c *Control) registerWithEmailProof(ctx context.Context, in RegisterInput, passwordHash string) (User, error) {
	required, err := c.registrationEmailRequired(ctx)
	if err != nil {
		return User{}, err
	}
	verified := required || in.VerificationCode != ""
	if verified && (in.Email == "" || in.VerificationCode == "") {
		return User{}, ErrInvalidInput
	}
	if in.Email != "" {
		var err error
		in.Email, err = normalizedEmail(in.Email)
		if err != nil {
			return User{}, err
		}
		if err = c.validateEmailPolicy(ctx, in.Email, true); err != nil {
			return User{}, err
		}
	}
	if len(in.AffiliateCode) > 100 {
		return User{}, ErrInvalidInput
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if verified {
		if err = c.consumeEmailProofTx(ctx, tx, in.Email, "registration", in.VerificationCode, nil); err != nil {
			return User{}, commitInvalidEmailProof(ctx, tx, err)
		}
	}
	var u User
	err = tx.QueryRow(ctx, `INSERT INTO v3_identity.users(username,display_name,email,password_hash,email_verified,inviter_id)
	 VALUES($1,$2,NULLIF($3,''),$4,$5,(SELECT id FROM v3_identity.users WHERE aff_code=$6 AND status='active' AND deleted_at IS NULL))
	 RETURNING id,username,display_name,coalesce(email,''),role,status,group_name,0::bigint`,
		in.Username, in.DisplayName, in.Email, passwordHash, verified, in.AffiliateCode).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Role, &u.Status, &u.Group, &u.AffiliateMicroCredits)
	if err != nil {
		return User{}, controlDBError(err)
	}
	if err = recordRegistrationPolicies(ctx, tx, u.ID, in, c.cfg.Now()); err != nil {
		return User{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return u, nil
}

func (c *Control) VerifyEmail(ctx context.Context, uid int64, email, code string) (User, error) {
	email, err := normalizedEmail(email)
	if err != nil {
		return User{}, err
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = c.consumeEmailProofTx(ctx, tx, email, "binding", code, &uid); err != nil {
		return User{}, commitInvalidEmailProof(ctx, tx, err)
	}
	u, err := scanUser(tx.QueryRow(ctx, `UPDATE v3_identity.users SET email=$2,email_verified=true WHERE id=$1 AND status='active' AND deleted_at IS NULL RETURNING `+userColumns, uid, email))
	if err != nil {
		return User{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return u, nil
}

// ResetPassword retains enabled TOTP state and revokes all first-factor sessions.
// The empty-password form preserves v2's temporary-password response.
func (c *Control) ResetPassword(ctx context.Context, email, token, password string) (string, error) {
	email, err := normalizedEmail(email)
	if err != nil {
		return "", err
	}
	generated := ""
	if password == "" {
		password, err = randomToken()
		if err != nil {
			return "", err
		}
		password = password[:12]
		generated = password
	}
	if len(password) < 10 || len(password) > 72 {
		return "", ErrInvalidInput
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var uid int64
	err = tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE lower(btrim(email))=$1 AND status='active' AND deleted_at IS NULL FOR UPDATE`, email).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrCredentials
	}
	if err != nil {
		return "", err
	}
	if err = c.consumeEmailProofTx(ctx, tx, email, "reset", token, &uid); err != nil {
		return "", commitInvalidEmailProof(ctx, tx, err)
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_identity.users SET password_hash=$2 WHERE id=$1`, uid, string(hash)); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_identity.sessions SET revoked_at=$2 WHERE user_id=$1 AND revoked_at IS NULL`, uid, c.cfg.Now()); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM v3_identity.login_challenges WHERE user_id=$1`, uid); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return generated, nil
}
