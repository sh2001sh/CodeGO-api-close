package ledger

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

var ErrHistoryQuery = errors.New("ledger: invalid history query")

type HistoricalEntry struct {
	ID            string         `json:"id"`
	SourceAccount string         `json:"source_account_id"`
	Amount        credits.Micro  `json:"amount_micro"`
	BalanceAfter  *credits.Micro `json:"balance_after_micro"`
	Kind          string         `json:"kind"`
	Direction     string         `json:"direction"`
	Reason        string         `json:"reason"`
	CreatedAt     time.Time      `json:"created_at"`
}

type HistoryPage struct {
	Items  []HistoricalEntry `json:"items"`
	Before string            `json:"next_before,omitempty"`
}

type historyCursor struct {
	Time time.Time `json:"time"`
	ID   string    `json:"id"`
}

// ReadHistory exposes only the authenticated user's retained financial
// evidence, including their mapped subscription and key accounts. Internal
// metadata, operator identities and credentials never enter the response.
func ReadHistory(ctx context.Context, pool *pgxpool.Pool, userID int64, source, before string, limit int) (HistoryPage, error) {
	page := HistoryPage{Items: []HistoricalEntry{}}
	if userID <= 0 || limit < 1 || limit > 200 || len(source) > 512 || len(before) > 1024 {
		return page, ErrHistoryQuery
	}
	cursor := historyCursor{}
	if before != "" {
		raw, err := base64.RawURLEncoding.DecodeString(before)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Time.IsZero() || cursor.ID == "" {
			return page, ErrHistoryQuery
		}
	}
	rows, err := pool.Query(ctx, `SELECT e.entry_id,e.source_account_id,e.amount,e.balance_after,e.entry_type,e.direction,e.reason_code,e.created_at
	 FROM v3_billing.historical_entries e
	 JOIN v3_billing.historical_accounts h USING(source_account_id)
	 LEFT JOIN v3_billing.accounts a ON a.id=h.account_id
	 WHERE ((h.owner_type='user' AND h.owner_id=$1) OR (a.owner_type='user' AND a.owner_id=$1)
	 OR (a.owner_type='api_key' AND EXISTS(SELECT 1 FROM v3_identity.api_keys k WHERE k.id=a.owner_id AND k.user_id=$1))
	 OR (a.owner_type='subscription' AND EXISTS(SELECT 1 FROM v3_commerce.subscriptions s WHERE s.account_id=a.id AND s.user_id=$1)))
	 AND ($2='' OR e.source_account_id=$2) AND ($3='' OR (e.created_at,e.entry_id)<($4,$3))
	 ORDER BY e.created_at DESC,e.entry_id DESC LIMIT $5`, userID, source, cursor.ID, cursor.Time, limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry HistoricalEntry
		if err := rows.Scan(&entry.ID, &entry.SourceAccount, &entry.Amount, &entry.BalanceAfter, &entry.Kind, &entry.Direction, &entry.Reason, &entry.CreatedAt); err != nil {
			return page, err
		}
		page.Items = append(page.Items, entry)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		raw, err := json.Marshal(historyCursor{Time: last.CreatedAt, ID: last.ID})
		if err != nil {
			return page, err
		}
		page.Before = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}
