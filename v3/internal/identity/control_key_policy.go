package identity

import (
	"context"
	"errors"
	"math"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// KeyBudgetPoster keeps cap adjustments in the same transaction as key edits.
type KeyBudgetPoster interface {
	PostTx(context.Context, pgx.Tx, billing.Entry) (billing.PostResult, error)
}

func (c *Control) validateKeyGroup(ctx context.Context, tx pgx.Tx, uid int64, in *KeyInput) error {
	if in.Group == nil {
		return nil
	}
	// Virtual card aliases are resolved against current entitlement and route
	// configuration by the planner; they are not global official-group grants.
	if *in.Group == "zero-hour" || *in.Group == "monthly-pass" {
		return nil
	}
	var authorized bool
	query := `SELECT EXISTS(SELECT 1 FROM v3_identity.allowed_groups($1) g WHERE g=$2)`
	args := []any{uid, *in.Group}
	if *in.Group == "auto" {
		query = `SELECT EXISTS(SELECT 1 FROM v3_identity.auto_groups($1))`
		args = []any{uid}
		in.CrossGroupRetry = true
	}
	if err := tx.QueryRow(ctx, query, args...).Scan(&authorized); err != nil {
		return err
	}
	if !authorized {
		return ErrForbidden
	}
	return nil
}

func (c *Control) setKeyBudget(ctx context.Context, tx pgx.Tx, keyID int64, remaining *int64) (int64, error) {
	if c.cfg.BudgetPoster == nil {
		return 0, errors.New("identity: key budget ledger writer unavailable")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind)
	 VALUES ('api_key',$1,'key_budget') ON CONFLICT(owner_type,owner_id,kind) DO NOTHING`, keyID); err != nil {
		return 0, err
	}
	var id, current int64
	if err := tx.QueryRow(ctx, `SELECT id,balance FROM v3_billing.accounts
	 WHERE owner_type='api_key' AND owner_id=$1 AND kind='key_budget' FOR UPDATE`, keyID).Scan(&id, &current); err != nil {
		return 0, err
	}
	if remaining == nil || *remaining == current {
		return id, nil
	}
	// requested is nonnegative, so only a negative current can overflow subtraction.
	if current < 0 && *remaining > math.MaxInt64+current {
		return 0, credits.ErrOverflow
	}
	operation, err := randomToken()
	if err != nil {
		return 0, err
	}
	_, err = c.cfg.BudgetPoster.PostTx(ctx, tx, billing.Entry{
		AccountID: id, Amount: credits.Micro(*remaining - current), Kind: "adjustment",
		OperationID: "key-budget:" + operation, Reason: "API key spending cap",
		Metadata: map[string]any{"key_id": keyID, "remaining_micro_credits": *remaining},
	})
	return id, err
}

func (c *Control) UpdateKeyStatus(ctx context.Context, uid, id int64, status string) error {
	if id <= 0 || status != "active" && status != "disabled" {
		return ErrInvalidInput
	}
	tag, err := c.pool.Exec(ctx, `UPDATE v3_identity.api_keys SET status=$3
	 WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL`, id, uid, status)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (c *Control) groupsHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	var groups []string
	err := c.pool.QueryRow(r.Context(), `SELECT ARRAY(SELECT v3_identity.allowed_groups($1))`, u.ID).Scan(&groups)
	c.reply(w, groups, err)
}
