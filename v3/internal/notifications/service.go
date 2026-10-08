// Package notifications owns the session-scoped transactional inbox.
package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalid     = errors.New("notifications: invalid input")
	ErrNotFound    = errors.New("notifications: not found")
	ErrUnavailable = errors.New("notifications: unavailable")
)

type Service struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	hub  *eventHub
}

func New(pool *pgxpool.Pool, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	s := &Service{pool: pool, log: log}
	s.hub = newEventHub(pool, log)
	return s
}

type Item struct {
	ID        string          `json:"id"`
	Category  string          `json:"category"`
	Kind      string          `json:"kind"`
	TitleKey  string          `json:"title_key"`
	BodyKey   string          `json:"body_key"`
	Data      json.RawMessage `json:"data"`
	ActionURL string          `json:"action_url"`
	CreatedAt time.Time       `json:"created_at"`
	ReadAt    *time.Time      `json:"read_at"`
}

type Filter struct {
	Category       string
	Unread         bool
	Page, PageSize int
}
type List struct {
	Items       []Item `json:"items"`
	UnreadCount int64  `json:"unread_count"`
	Page        int    `json:"page"`
	PageSize    int    `json:"page_size"`
	Total       int64  `json:"total"`
	LatestID    string `json:"latest_id"`
}

type Summary struct {
	UnreadCount int64  `json:"unread_count"`
	LatestID    string `json:"latest_id"`
}

func validCategory(c string) bool {
	return c == "" || c == "all" || c == "market" || c == "billing" || c == "review" || c == "rewards" || c == "system"
}
func normalizeCategory(c string) string {
	if c == "all" {
		return ""
	}
	return c
}

func positiveID(value string) (int64, error) {
	if value == "" || len(value) > 19 || value[0] < '1' || value[0] > '9' {
		return 0, ErrInvalid
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, ErrInvalid
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return 0, ErrInvalid
	}
	return n, nil
}

func (s *Service) Summary(ctx context.Context, user int64) (Summary, error) {
	v := Summary{LatestID: "0"}
	if user <= 0 {
		return v, ErrInvalid
	}
	if s.pool == nil {
		return v, ErrUnavailable
	}
	err := s.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE read_at IS NULL),coalesce(max(id),0)::text FROM v3_identity.notifications WHERE user_id=$1`, user).Scan(&v.UnreadCount, &v.LatestID)
	return v, err
}

func (s *Service) List(ctx context.Context, user int64, f Filter) (List, error) {
	v := List{Items: []Item{}, Page: f.Page, PageSize: f.PageSize, LatestID: "0"}
	if user <= 0 || !validCategory(f.Category) || f.Page < 1 || f.Page > 100000 || f.PageSize < 1 || f.PageSize > 100 {
		return v, ErrInvalid
	}
	if s.pool == nil {
		return v, ErrUnavailable
	}
	f.Category = normalizeCategory(f.Category)
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE read_at IS NULL),count(*) FILTER(WHERE ($2='' OR category=$2) AND (NOT $3 OR read_at IS NULL)),coalesce(max(id),0)::text FROM v3_identity.notifications WHERE user_id=$1`, user, f.Category, f.Unread).Scan(&v.UnreadCount, &v.Total, &v.LatestID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id::text,category,kind,title_key,body_key,data,action_url,created_at,read_at FROM v3_identity.notifications WHERE user_id=$1 AND ($2='' OR category=$2) AND (NOT $3 OR read_at IS NULL) ORDER BY created_at DESC,id DESC LIMIT $4 OFFSET $5`, user, f.Category, f.Unread, f.PageSize, (f.Page-1)*f.PageSize)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n Item
			if err = rows.Scan(&n.ID, &n.Category, &n.Kind, &n.TitleKey, &n.BodyKey, &n.Data, &n.ActionURL, &n.CreatedAt, &n.ReadAt); err != nil {
				return err
			}
			v.Items = append(v.Items, n)
		}
		return rows.Err()
	})
	return v, err
}

func (s *Service) Read(ctx context.Context, user int64, id string, markRead bool) error {
	n, err := positiveID(id)
	if err != nil || n <= 0 || user <= 0 {
		return ErrInvalid
	}
	if s.pool == nil {
		return ErrUnavailable
	}
	// Lock ownership even for already-read entries, but avoid emitting duplicate
	// change events when a client repeats the same acknowledgement.
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var read *time.Time
		err := tx.QueryRow(ctx, `SELECT read_at FROM v3_identity.notifications WHERE id=$1 AND user_id=$2 FOR UPDATE`, n, user).Scan(&read)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil || (read != nil) == markRead {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE v3_identity.notifications SET read_at=CASE WHEN $3 THEN now() ELSE NULL END WHERE id=$1 AND user_id=$2`, n, user, markRead)
		return err
	})
}
func (s *Service) ReadAll(ctx context.Context, user int64, category, throughID string) error {
	through, err := positiveID(throughID)
	if err != nil || through <= 0 || user <= 0 || !validCategory(category) {
		return ErrInvalid
	}
	if s.pool == nil {
		return ErrUnavailable
	}
	_, err = s.pool.Exec(ctx, `UPDATE v3_identity.notifications SET read_at=now() WHERE user_id=$1 AND read_at IS NULL AND ($2='' OR category=$2) AND id<=$3`, user, normalizeCategory(category), through)
	return err
}
