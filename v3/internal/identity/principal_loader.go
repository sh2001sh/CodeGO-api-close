package identity

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// LoadPrincipal resolves current authority for a persisted background job.
// Normal requests use the cached Authorizer; this function never reads secrets.
func LoadPrincipal(ctx context.Context, pool *pgxpool.Pool, userID, keyID int64) (gateway.Principal, error) {
	if userID <= 0 || keyID <= 0 {
		return gateway.Principal{}, gateway.ErrInvalidKey
	}
	if pool == nil {
		return gateway.Principal{}, fmt.Errorf("identity: principal database is unavailable")
	}
	p, err := scanProfile(pool.QueryRow(ctx, profileSelect+`WHERE k.user_id=$1 AND k.id=$2`, userID, keyID))
	if err != nil {
		return gateway.Principal{}, err
	}
	if p == nil {
		return gateway.Principal{}, gateway.ErrInvalidKey
	}
	if err = p.usable(time.Now()); err != nil {
		return gateway.Principal{}, err
	}
	if p.Group == "auto" {
		if len(p.AutoGroups) == 0 {
			return gateway.Principal{}, gateway.ErrInvalidKey
		}
	} else if p.Group != "zero-hour" && p.Group != "monthly-pass" && !slices.Contains(p.AllowedGroups, p.Group) {
		return gateway.Principal{}, gateway.ErrInvalidKey
	}
	if p.BudgetLimited && p.BudgetAccountID <= 0 {
		return gateway.Principal{}, gateway.ErrInvalidKey
	}
	return p.Principal(), nil
}
