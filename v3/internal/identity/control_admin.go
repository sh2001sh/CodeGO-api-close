package identity

import (
	"context"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

type AdminCreateInput struct {
	RegisterInput
	Role   string `json:"role"`
	Status string `json:"status"`
	Group  string `json:"group"`
}

func (c *Control) CreateUser(ctx context.Context, actor User, in AdminCreateInput) (User, error) {
	if !actor.IsAdmin() {
		return User{}, ErrForbidden
	}
	if err := validateRegistration(in.RegisterInput); err != nil {
		return User{}, err
	}
	if in.Role == "" {
		in.Role = "user"
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if in.Group == "" {
		in.Group = "default"
	}
	if in.Role != "user" && in.Role != "admin" && in.Role != "root" {
		return User{}, ErrInvalidInput
	}
	if in.Status != "active" && in.Status != "disabled" || len(in.Group) > 100 {
		return User{}, ErrInvalidInput
	}
	if actor.Role != "root" && in.Role != "user" {
		return User{}, ErrForbidden
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	return scanUser(c.pool.QueryRow(ctx, `INSERT INTO v3_identity.users(username,password_hash,display_name,email,role,status,group_name)
		VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7) RETURNING `+userColumns, in.Username, string(hash), in.DisplayName, in.Email, in.Role, in.Status, in.Group))
}

func (c *Control) DeleteUser(ctx context.Context, actor User, id int64) error {
	if !actor.IsAdmin() || actor.ID == id {
		return ErrForbidden
	}
	tag, err := c.pool.Exec(ctx, `UPDATE v3_identity.users SET deleted_at=now(),status='disabled' WHERE id=$1 AND deleted_at IS NULL AND ($2='root' OR role='user')`, id, actor.Role)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (c *Control) getUserHTTP(w http.ResponseWriter, r *http.Request) {
	actor, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	if !actor.IsAdmin() {
		c.reply(w, nil, ErrForbidden)
		return
	}
	u, err := c.User(r.Context(), parseID(r.PathValue("id")))
	c.reply(w, u, err)
}
func (c *Control) createUserHTTP(w http.ResponseWriter, r *http.Request) {
	actor, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	var in AdminCreateInput
	if err := decodeControl(w, r, &in); err != nil {
		c.reply(w, nil, err)
		return
	}
	u, err := c.CreateUser(r.Context(), actor, in)
	c.reply(w, u, err)
}
func (c *Control) deleteUserHTTP(w http.ResponseWriter, r *http.Request) {
	actor, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	c.reply(w, nil, c.DeleteUser(r.Context(), actor, parseID(r.PathValue("id"))))
}
func (c *Control) updateUserBodyHTTP(w http.ResponseWriter, r *http.Request) {
	actor, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	var in struct {
		UserUpdate
		ID int64 `json:"id"`
	}
	if err := decodeControl(w, r, &in); err != nil {
		c.reply(w, nil, err)
		return
	}
	u, err := c.UpdateUser(r.Context(), actor, in.ID, in.UserUpdate)
	c.reply(w, u, err)
}
