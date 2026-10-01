package identity

import (
	"context"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
)

type controlPage[T any] struct {
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
	Items    []T   `json:"items"`
}

func legacyPagination(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("p"))
	if page < 1 {
		page = 1
	}
	if page > 10000000 {
		page = 10000000
	}
	limit := 0
	for _, name := range []string{"page_size", "ps", "size"} {
		value, _ := strconv.Atoi(r.URL.Query().Get(name))
		if value > 0 {
			limit = value
			break
		}
	}
	if limit == 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}

func (c *Control) keyPage(ctx context.Context, uid int64, page, limit int) (controlPage[KeyRecord], error) {
	out := controlPage[KeyRecord]{Page: page, PageSize: limit, Items: []KeyRecord{}}
	err := pgx.BeginTxFunc(ctx, c.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM v3_identity.api_keys WHERE user_id=$1 AND deleted_at IS NULL`, uid).Scan(&out.Total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+keyColumns+` FROM `+keyFrom+` WHERE k.user_id=$1 AND k.deleted_at IS NULL ORDER BY k.id DESC LIMIT $2 OFFSET $3`, uid, limit, (page-1)*limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			k, err := scanKey(rows)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, k)
		}
		return rows.Err()
	})
	return out, err
}

func (c *Control) userPage(ctx context.Context, actor User, page, limit int) (controlPage[User], error) {
	out := controlPage[User]{Page: page, PageSize: limit, Items: []User{}}
	if !actor.IsAdmin() {
		return out, ErrForbidden
	}
	err := pgx.BeginTxFunc(ctx, c.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users WHERE deleted_at IS NULL`).Scan(&out.Total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+userColumns+` FROM v3_identity.users WHERE deleted_at IS NULL ORDER BY id DESC LIMIT $1 OFFSET $2`, limit, (page-1)*limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			u, err := scanUser(rows)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, u)
		}
		return rows.Err()
	})
	return out, err
}
