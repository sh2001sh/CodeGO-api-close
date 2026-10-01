package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/mail"

	"golang.org/x/crypto/bcrypt"
)

type ProfileInput struct {
	DisplayName      string `json:"display_name"`
	Email            string `json:"email"`
	Password         string `json:"password"`
	OriginalPassword string `json:"original_password"`
}

func (c *Control) UpdateProfile(ctx context.Context, uid int64, in ProfileInput) (User, error) {
	if len(in.DisplayName) > 100 || len(in.Email) > 254 {
		return User{}, ErrInvalidInput
	}
	if in.Email != "" {
		if _, err := mail.ParseAddress(in.Email); err != nil {
			return User{}, ErrInvalidInput
		}
	}
	if in.Password == "" {
		return scanUser(c.pool.QueryRow(ctx, `UPDATE v3_identity.users SET display_name=$2,email=NULLIF($3,''),
		 email_verified=CASE WHEN email IS DISTINCT FROM NULLIF($3,'') THEN false ELSE email_verified END
		 WHERE id=$1 AND status='active' AND deleted_at IS NULL RETURNING `+userColumns, uid, in.DisplayName, in.Email))
	}
	if len(in.Password) < 10 || len(in.Password) > 72 || len(in.OriginalPassword) > 72 {
		return User{}, ErrInvalidInput
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var old string
	err = tx.QueryRow(ctx, `SELECT coalesce(password_hash,'') FROM v3_identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL FOR UPDATE`, uid).Scan(&old)
	if err != nil {
		return User{}, controlDBError(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(old), []byte(in.OriginalPassword)) != nil {
		return User{}, ErrCredentials
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	u, err := scanUser(tx.QueryRow(ctx, `UPDATE v3_identity.users SET display_name=$2,email=NULLIF($3,''),password_hash=$4,
	 email_verified=CASE WHEN email IS DISTINCT FROM NULLIF($3,'') THEN false ELSE email_verified END
	 WHERE id=$1 RETURNING `+userColumns, uid, in.DisplayName, in.Email, string(hash)))
	if err != nil {
		return User{}, err
	}
	// A password change revokes every existing session in the same transaction.
	_, err = tx.Exec(ctx, `UPDATE v3_identity.sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, uid)
	if err != nil {
		return User{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return u, nil
}

func (c *Control) updateProfileHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	var in ProfileInput
	if err := decodeControl(w, r, &in); err != nil {
		c.reply(w, nil, err)
		return
	}
	updated, err := c.UpdateProfile(r.Context(), u.ID, in)
	c.reply(w, updated, err)
}

func (c *Control) settingsHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	var settings json.RawMessage
	if r.Method == http.MethodGet {
		err := c.pool.QueryRow(r.Context(), `SELECT settings FROM v3_identity.users WHERE id=$1`, u.ID).Scan(&settings)
		c.reply(w, settings, err)
		return
	}
	if err := decodeControl(w, r, &settings); err != nil {
		c.reply(w, nil, err)
		return
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(settings, &object) != nil || object == nil || len(settings) > 64*1024 {
		c.reply(w, nil, ErrInvalidInput)
		return
	}
	_, err := c.pool.Exec(r.Context(), `UPDATE v3_identity.users SET settings=$2 WHERE id=$1`, u.ID, settings)
	c.reply(w, settings, err)
}
