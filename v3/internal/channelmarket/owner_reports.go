package channelmarket

import (
	"context"
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type UserUsage struct {
	UserID   int64 `json:"user_id"`
	Requests int64 `json:"request_count"`
	Amount   int64 `json:"amount_micro"`
}

// UserUsage aggregates the complete owned history in PostgreSQL. The numeric
// sum is cast only after aggregation, so overflow is an error, never wrapping.
func (s *Service) UserUsage(ctx context.Context, a Actor) (map[int64]UserUsage, error) {
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	if a.UserID <= 0 && !a.Admin {
		return nil, ErrInvalid
	}
	rows, err := s.pool.Query(ctx, `SELECT l.user_id,count(*),sum(l.amount)::bigint
FROM v3_billing.usage_logs l JOIN v3_catalog.channels c ON c.id=l.channel_id
WHERE c.scope='marketplace' AND (c.owner_user_id=$1 OR $2)
GROUP BY l.user_id ORDER BY l.user_id`, a.UserID, a.Admin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := map[int64]UserUsage{}
	for rows.Next() {
		var item UserUsage
		if err = rows.Scan(&item.UserID, &item.Requests, &item.Amount); err != nil {
			return nil, err
		}
		items[item.UserID] = item
	}
	return items, rows.Err()
}

func (s *Service) ExportLogs(ctx context.Context, a Actor, writer *csv.Writer) error {
	if err := writer.Write([]string{"request_id", "created_at", "channel_id", "user_id", "model", "amount_micro", "terminal"}); err != nil {
		return err
	}
	var before time.Time
	var beforeID int64
	for {
		items, err := s.LogsPage(ctx, a, before, beforeID, 1000)
		if err != nil {
			return err
		}
		for _, l := range items {
			if err = writer.Write([]string{csvText(l.RequestID), l.CreatedAt.Format(time.RFC3339Nano), strconv.FormatInt(l.ChannelID, 10), strconv.FormatInt(l.UserID, 10), csvText(l.Model), strconv.FormatInt(l.Amount, 10), csvText(l.Terminal)}); err != nil {
				return err
			}
		}
		if len(items) < 1000 {
			writer.Flush()
			return writer.Error()
		}
		last := items[len(items)-1]
		before, beforeID = last.CreatedAt, last.ID
	}
}

type UsagePoint struct {
	Timestamp int64 `json:"timestamp"`
	Requests  int64 `json:"request_count"`
	Successes int64 `json:"success_count"`
	Tokens    int64 `json:"token_count"`
	Amount    int64 `json:"amount_micro"`
}

type UsageSeries struct {
	UserID        string       `json:"user_id"`
	ChannelID     string       `json:"channel_id"`
	BucketSeconds int64        `json:"bucket_seconds"`
	Points        []UsagePoint `json:"points"`
}

func (s *Service) UserUsageSeries(ctx context.Context, a Actor, channel, user int64, rangeHours int) (UsageSeries, error) {
	result := UsageSeries{UserID: strconv.FormatInt(user, 10), ChannelID: strconv.FormatInt(channel, 10), BucketSeconds: 3600, Points: []UsagePoint{}}
	if s.pool == nil {
		return result, ErrUnavailable
	}
	if user <= 0 || channel <= 0 || rangeHours < 1 || rangeHours > 8760 || a.UserID <= 0 && !a.Admin {
		return result, ErrInvalid
	}
	var publicID string
	err := s.pool.QueryRow(ctx, `SELECT g.public_channel_id FROM v3_channelmarket.groups g
JOIN v3_catalog.channels c ON c.id=g.channel_id
WHERE c.id=$1 AND (c.owner_user_id=$2 OR $3)`, channel, a.UserID, a.Admin).Scan(&publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	result.ChannelID = publicID
	now := s.cfg.Now().UTC()
	rows, err := s.pool.Query(ctx, `SELECT extract(epoch FROM date_trunc('hour',created_at AT TIME ZONE 'UTC'))::bigint,
count(*), count(*) FILTER(WHERE terminal IN ('completed','completed_no_usage','Completed','CompletedNoUsage')),
coalesce(sum((prompt_tokens::numeric+completion_tokens::numeric)) FILTER(WHERE terminal IN ('completed','completed_no_usage','Completed','CompletedNoUsage')),0)::bigint,
sum(amount)::bigint FROM v3_billing.usage_logs
WHERE channel_id=$1 AND user_id=$2 AND created_at >= $3 AND created_at <= $4
GROUP BY 1 ORDER BY 1`, channel, user, now.Add(-time.Duration(rangeHours)*time.Hour), now)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var p UsagePoint
		if err = rows.Scan(&p.Timestamp, &p.Requests, &p.Successes, &p.Tokens, &p.Amount); err != nil {
			return result, err
		}
		result.Points = append(result.Points, p)
	}
	return result, rows.Err()
}

func (s *Service) httpUsageSeries(w http.ResponseWriter, r *http.Request, a Actor) {
	user, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil || user <= 0 {
		s.result(w, nil, ErrInvalid)
		return
	}
	hours := 24
	if value := r.URL.Query().Get("range_hours"); value != "" {
		hours, err = strconv.Atoi(value)
	}
	if err != nil || hours < 1 || hours > 8760 {
		s.result(w, nil, ErrInvalid)
		return
	}
	channel, ok := s.httpChannel(w, r, a)
	if !ok {
		return
	}
	items, err := s.UserUsageSeries(r.Context(), a, channel, user, hours)
	s.result(w, items, err)
}
