package oidc

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrInactive means the requested identity is absent, disabled or deleted.
var ErrInactive = errors.New("oidc: active identity required")

// EnsureSubject resolves the stable six-character community subject. It needs
// only the migrated database, so community routes work with OIDC disabled.
func EnsureSubject(ctx context.Context, pool *pgxpool.Pool, userID int64) (string, error) {
	if userID <= 0 {
		return "", ErrInactive
	}
	if pool == nil {
		return "", errors.New("oidc: database pool required")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	u, err := loadUser(ctx, tx, userID)
	if errors.Is(err, errGrant) {
		return "", ErrInactive
	}
	if err != nil {
		return "", err
	}
	if u.Subject == "" {
		u.Subject, err = assignSubject(ctx, tx, userID)
		if err != nil {
			return "", err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return u.Subject, nil
}
