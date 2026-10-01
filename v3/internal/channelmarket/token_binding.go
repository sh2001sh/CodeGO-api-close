package channelmarket

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type BoundToken struct {
	TokenID    int64  `json:"token_id"`
	TokenGroup string `json:"token_group"`
	APIKey     string `json:"api_key,omitempty"`
}

// IssueKey belongs to identity and must enforce its normal key policies. This
// port only selects the owner-authorized marketplace group.
func (s *Service) BindGroup(ctx context.Context, user int64, group string, key int64) (BoundToken, error) {
	var out BoundToken
	if key < 0 {
		return out, ErrInvalid
	}
	if key > 0 {
		err := s.BindToken(ctx, user, group, key)
		out.TokenID = key
		return out, err
	}
	if s.cfg.IssueKey == nil {
		return out, ErrUnavailable
	}
	var internal string
	err := s.transaction(ctx, func(tx pgx.Tx) error {
		if e := accessible(ctx, tx, user, group); e != nil {
			return e
		}
		return tx.QueryRow(ctx, `SELECT internal_group_name FROM v3_channelmarket.groups WHERE id=$1`, group).Scan(&internal)
	})
	if err != nil {
		return out, err
	}
	return s.cfg.IssueKey(ctx, user, internal)
}
func (s *Service) BindRoutePool(ctx context.Context, user int64, pool string, key int64) (BoundToken, error) {
	var out BoundToken
	if key < 0 {
		return out, ErrInvalid
	}
	if key > 0 {
		err := s.BindPoolToken(ctx, user, pool, key)
		out.TokenID = key
		return out, err
	}
	if s.cfg.IssueKey == nil || s.pool == nil {
		return out, ErrUnavailable
	}
	var internal string
	err := s.pool.QueryRow(ctx, `SELECT internal_group_name FROM v3_channelmarket.route_pools WHERE id=$1 AND owner_user_id=$2`, pool, user).Scan(&internal)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return out, err
	}
	return s.cfg.IssueKey(ctx, user, internal)
}
