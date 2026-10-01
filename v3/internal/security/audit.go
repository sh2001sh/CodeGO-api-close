package security

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
)

var ErrNotFound = errors.New("security: audit event not found")
var ErrInvalid = errors.New("security: invalid audit query")
var ErrExportLimit = errors.New("security: export exceeds 20000 events; narrow the filters")

func auditWhere(a Actor, q Query) (string, []any, error) {
	if a.UserID <= 0 {
		return "", nil, ErrInvalid
	}
	parts := []string{"($1 OR owner_user_id=$2)"}
	args := []any{a.Admin, a.UserID}
	add := func(expression string, value any) {
		args = append(args, value)
		parts = append(parts, fmt.Sprintf(expression, len(args)))
	}
	if q.Source != "" {
		add("source=$%d", q.Source)
	}
	if q.ReviewStatus != "" {
		add("review_status=$%d", q.ReviewStatus)
	}
	if q.MarketplaceChannel != "" {
		add("marketplace_channel_id=$%d", q.MarketplaceChannel)
	}
	if q.Model != "" {
		add("model=$%d", q.Model)
	}
	if q.StartTimestamp > 0 {
		add("created_at>=to_timestamp($%d)", q.StartTimestamp)
	}
	if q.EndTimestamp > 0 {
		add("created_at<=to_timestamp($%d)", q.EndTimestamp)
	}
	if q.Search != "" {
		expression := "(request_id LIKE $%[1]d OR token_name LIKE $%[1]d OR user_id::text LIKE $%[1]d OR user_id IN (SELECT id FROM v3_identity.users WHERE external_id LIKE $%[1]d)"
		if a.Admin {
			expression += " OR upstream_error_message LIKE $%[1]d"
		}
		add(expression+")", "%"+q.Search+"%")
	}
	return " WHERE " + strings.Join(parts, " AND "), args, nil
}
func (g *Guard) List(ctx context.Context, a Actor, q Query) (EventList, error) {
	var result EventList
	if g.pool == nil {
		return result, errors.New("security: audit database unavailable")
	}
	where, args, err := auditWhere(a, q)
	if err != nil {
		return result, err
	}
	result.Page = max(q.Page, 1)
	result.PageSize = min(max(q.PageSize, 1), 100)
	if int64(result.Page-1) > math.MaxInt64/int64(result.PageSize) {
		return result, ErrInvalid
	}
	if err = g.pool.QueryRow(ctx, `SELECT count(*) FROM v3_security.security_audit_events`+where, args...).Scan(&result.Total); err != nil {
		return result, err
	}
	args = append(args, result.PageSize, (result.Page-1)*result.PageSize)
	sql := `SELECT ` + eventSelect + ` FROM v3_security.security_audit_events` + where + fmt.Sprintf(` ORDER BY created_at DESC NULLS LAST,id LIMIT $%d OFFSET $%d`, len(args)-1, len(args))
	rows, err := g.pool.Query(ctx, sql, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	result.Items = []Event{}
	for rows.Next() {
		e, err := scanEvent(rows, a.Admin)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, e)
	}
	return result, rows.Err()
}
func (g *Guard) Get(ctx context.Context, a Actor, id string) (Event, error) {
	if a.UserID <= 0 || g.pool == nil {
		return Event{}, ErrInvalid
	}
	e, err := scanEvent(g.pool.QueryRow(ctx, `SELECT `+eventSelect+` FROM v3_security.security_audit_events WHERE id=$1 AND ($2 OR owner_user_id=$3)`, id, a.Admin, a.UserID), a.Admin)
	if errors.Is(err, pgx.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	return e, err
}
func (g *Guard) Review(ctx context.Context, a Actor, id, status, note string) (Event, error) {
	if a.UserID <= 0 || g.pool == nil {
		return Event{}, ErrInvalid
	}
	switch status {
	case "unreviewed", "acknowledged", "resolved", "false_positive":
	default:
		return Event{}, ErrInvalid
	}
	runes := []rune(note)
	if len(runes) > 1000 {
		note = string(runes[:1000])
	}
	now := g.cfg.Now()
	reviewer := a.UserID
	var reviewed any = now
	if status == "unreviewed" {
		reviewer = 0
		reviewed = nil
	}
	e, err := scanEvent(g.pool.QueryRow(ctx, `UPDATE v3_security.security_audit_events SET review_status=$4,review_note=$5,reviewed_by=$6,reviewed_at=$7,updated_at=$8 WHERE id=$1 AND ($2 OR owner_user_id=$3) RETURNING `+eventSelect, id, a.Admin, a.UserID, status, note, reviewer, reviewed, now), a.Admin)
	if errors.Is(err, pgx.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	return e, err
}
