// Package audit provides scoped, cursor-based usage reporting. Billing owns
// usage-log writes; this package only reads the ledger's usage projection.
package audit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

var (
	ErrInvalid     = errors.New("audit: invalid query")
	ErrForbidden   = errors.New("audit: forbidden")
	ErrUnavailable = errors.New("audit: unavailable")
	ErrNotFound    = errors.New("audit: request not found")
)

type Principal struct {
	UserID int64
	Admin  bool
	// KeyID scopes a read-only API-key caller to that key's usage.
	KeyID int64
}

type Config struct {
	Authenticate   func(*http.Request) (Principal, error)
	SampleRatePPM  int
	MaxSampleBytes int
}

type Service struct {
	pool *pgxpool.Pool
	cfg  Config
}

func New(pool *pgxpool.Pool, cfg Config) *Service {
	if cfg.MaxSampleBytes <= 0 {
		cfg.MaxSampleBytes = 64 << 10
	}
	if cfg.MaxSampleBytes > 1<<20 {
		cfg.MaxSampleBytes = 1 << 20
	}
	if cfg.SampleRatePPM < 0 {
		cfg.SampleRatePPM = 0
	}
	if cfg.SampleRatePPM > 1_000_000 {
		cfg.SampleRatePPM = 1_000_000
	}
	return &Service{pool: pool, cfg: cfg}
}

type Query struct {
	UserID, KeyID, ChannelID int64
	Model                    string
	From, To                 time.Time
	Limit                    int
	Cursor                   string
}

type Usage struct {
	ID               int64         `json:"id"`
	CreatedAt        time.Time     `json:"created_at"`
	AccountID        int64         `json:"account_id"`
	UserID           int64         `json:"user_id"`
	KeyID            int64         `json:"key_id"`
	ChannelID        int64         `json:"channel_id"`
	Amount           credits.Micro `json:"amount,string"`
	PromptTokens     int64         `json:"prompt_tokens"`
	CompletionTokens int64         `json:"completion_tokens"`
	CachedTokens     int64         `json:"cached_tokens"`
	Estimated        bool          `json:"estimated"`
	RequestID        string        `json:"request_id"`
	Model            string        `json:"model"`
	Terminal         string        `json:"terminal"`
}

type Page struct {
	Items      []Usage `json:"items"`
	NextCursor string  `json:"next_cursor,omitempty"`
	HasMore    bool    `json:"has_more"`
	PageSize   int     `json:"page_size"`
}

type Summary struct {
	Requests         int64         `json:"requests"`
	Amount           credits.Micro `json:"amount,string"`
	PromptTokens     int64         `json:"prompt_tokens"`
	CompletionTokens int64         `json:"completion_tokens"`
	CachedTokens     int64         `json:"cached_tokens"`
}

type cursor struct {
	At time.Time `json:"at"`
	ID int64     `json:"id"`
}

func encodeCursor(u Usage) string {
	b, _ := json.Marshal(cursor{At: u.CreatedAt, ID: u.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(value string) (cursor, error) {
	var c cursor
	if value == "" {
		return c, nil
	}
	if len(value) > 256 {
		return c, ErrInvalid
	}
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || json.Unmarshal(b, &c) != nil || c.At.IsZero() || c.ID <= 0 {
		return cursor{}, ErrInvalid
	}
	return c, nil
}

func scope(p Principal, q Query) (Query, error) {
	if p.UserID <= 0 {
		return q, ErrForbidden
	}
	if !p.Admin || p.KeyID > 0 {
		if q.UserID != 0 && q.UserID != p.UserID {
			return q, ErrForbidden
		}
		q.UserID = p.UserID
	}
	if p.KeyID > 0 {
		if q.KeyID != 0 && q.KeyID != p.KeyID {
			return q, ErrForbidden
		}
		q.KeyID = p.KeyID
	}
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 || q.Limit > 200 || q.UserID < 0 || q.KeyID < 0 || q.ChannelID < 0 ||
		len(q.Model) > 256 || (!q.From.IsZero() && !q.To.IsZero() && !q.From.Before(q.To)) {
		return q, ErrInvalid
	}
	_, err := decodeCursor(q.Cursor)
	return q, err
}

const usageColumns = `id, created_at, account_id, user_id, key_id, channel_id,
 amount, prompt_tokens, completion_tokens, cached_tokens, estimated, request_id, model, terminal`

const usageWhere = `WHERE ($1::bigint = 0 OR user_id = $1)
 AND ($2::bigint = 0 OR key_id = $2) AND ($3::bigint = 0 OR channel_id = $3)
 AND ($4::text = '' OR model = $4)
 AND ($5::timestamptz IS NULL OR created_at >= $5)
 AND ($6::timestamptz IS NULL OR created_at < $6)`

func timeArg(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func queryArgs(q Query) []any {
	return []any{q.UserID, q.KeyID, q.ChannelID, q.Model, timeArg(q.From), timeArg(q.To)}
}

func (s *Service) List(ctx context.Context, p Principal, q Query) (Page, error) {
	q, err := scope(p, q)
	if err != nil {
		return Page{}, err
	}
	if s.pool == nil {
		return Page{}, ErrUnavailable
	}
	c, _ := decodeCursor(q.Cursor)
	args := append(queryArgs(q), timeArg(c.At), c.ID, q.Limit+1)
	rows, err := s.pool.Query(ctx, `SELECT `+usageColumns+` FROM v3_billing.usage_logs `+usageWhere+`
 AND ($7::timestamptz IS NULL OR (created_at, id) < ($7, $8))
 ORDER BY created_at DESC, id DESC LIMIT $9`, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	page := Page{Items: make([]Usage, 0, q.Limit), PageSize: q.Limit}
	for rows.Next() {
		var u Usage
		if err := rows.Scan(&u.ID, &u.CreatedAt, &u.AccountID, &u.UserID, &u.KeyID, &u.ChannelID,
			&u.Amount, &u.PromptTokens, &u.CompletionTokens, &u.CachedTokens, &u.Estimated,
			&u.RequestID, &u.Model, &u.Terminal); err != nil {
			return Page{}, err
		}
		page.Items = append(page.Items, u)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	if len(page.Items) > q.Limit {
		page.Items = page.Items[:q.Limit]
		page.HasMore = true
		page.NextCursor = encodeCursor(page.Items[len(page.Items)-1])
	}
	return page, nil
}

func (s *Service) Summarize(ctx context.Context, p Principal, q Query) (Summary, error) {
	q, err := scope(p, q)
	if err != nil {
		return Summary{}, err
	}
	if s.pool == nil {
		return Summary{}, ErrUnavailable
	}
	var result Summary
	err = s.pool.QueryRow(ctx, `SELECT count(*), COALESCE(sum(amount),0),
 COALESCE(sum(prompt_tokens),0), COALESCE(sum(completion_tokens),0), COALESCE(sum(cached_tokens),0)
 FROM v3_billing.usage_logs `+usageWhere, queryArgs(q)...).Scan(&result.Requests, &result.Amount,
		&result.PromptTokens, &result.CompletionTokens, &result.CachedTokens)
	return result, err
}
