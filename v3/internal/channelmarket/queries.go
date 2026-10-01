package channelmarket

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

func formatFactor(f int64) string { return fmt.Sprintf("%d.%06d", f/1000000, f%1000000) }
func (s *Service) BatchMultipliers(ctx context.Context, a Actor, targets []MultiplierTarget, value *json.Number) (int, error) {
	if len(targets) == 0 || len(targets) > 100 {
		return 0, ErrInvalid
	}
	for i := range targets {
		if targets[i].PublicChannelID != "" {
			channel, e := s.ChannelID(ctx, a, targets[i].PublicChannelID)
			if e != nil {
				return 0, e
			}
			targets[i].ChannelID = channel
			targets[i].PublicChannelID = ""
		}
	}
	var factor int64
	var err error
	if value != nil {
		factor, err = multiplier(*value)
		if err != nil {
			return 0, err
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].ChannelID == targets[j].ChannelID {
			return targets[i].UserID < targets[j].UserID
		}
		return targets[i].ChannelID < targets[j].ChannelID
	})
	changed := 0
	err = s.transaction(ctx, func(tx pgx.Tx) error {
		seen := map[MultiplierTarget]bool{}
		for _, t := range targets {
			if seen[t] {
				continue
			}
			seen[t] = true
			if t.UserID <= 0 || t.ChannelID <= 0 {
				return ErrInvalid
			}
			if _, e := owned(ctx, tx, a, t.ChannelID); e != nil {
				return e
			}
			if e := setMultiplierTx(ctx, tx, t.ChannelID, t.UserID, factor, "batch"); e != nil {
				return e
			}
			changed++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return changed, nil
}

type Notice struct {
	ID            int64     `json:"id"`
	ChannelID     int64     `json:"channel_id"`
	PreviousPPM   int64     `json:"previous_multiplier_ppm"`
	MultiplierPPM int64     `json:"multiplier_ppm"`
	Cleared       bool      `json:"cleared"`
	Source        string    `json:"source"`
	CreatedAt     time.Time `json:"created_at"`
}

func (s *Service) Notices(ctx context.Context, user int64) ([]Notice, error) {
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	rows, err := s.pool.Query(ctx, `SELECT id,channel_id,previous_ppm,multiplier_ppm,cleared,source,created_at FROM v3_channelmarket.multiplier_notices WHERE user_id=$1 AND read_at IS NULL ORDER BY id LIMIT 100`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Notice{}
	for rows.Next() {
		var n Notice
		if err = rows.Scan(&n.ID, &n.ChannelID, &n.PreviousPPM, &n.MultiplierPPM, &n.Cleared, &n.Source, &n.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, n)
	}
	return items, rows.Err()
}
func (s *Service) ReadNotice(ctx context.Context, user, id int64) error {
	if s.pool == nil {
		return ErrUnavailable
	}
	tag, err := s.pool.Exec(ctx, `UPDATE v3_channelmarket.multiplier_notices SET read_at=coalesce(read_at,$3) WHERE id=$1 AND user_id=$2`, id, user, s.cfg.Now())
	if err == nil && tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return err
}
func (s *Service) TimeMultipliers(ctx context.Context, a Actor, channel int64) ([]TimeMultiplier, error) {
	items := []TimeMultiplier{}
	err := s.transaction(ctx, func(tx pgx.Tx) error {
		if _, e := owned(ctx, tx, a, channel); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT id,channel_id,starts_at,ends_at,multiplier_ppm,label FROM v3_channelmarket.time_range_multipliers WHERE channel_id=$1 ORDER BY starts_at,id`, channel)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var m TimeMultiplier
			var start, end time.Time
			if e = rows.Scan(&m.ID, &m.ChannelID, &start, &end, &m.MultiplierPPM, &m.Label); e != nil {
				return e
			}
			m.Start = start.Unix()
			m.End = end.Unix()
			m.Multiplier = json.Number(formatFactor(m.MultiplierPPM))
			items = append(items, m)
		}
		return rows.Err()
	})
	return items, err
}
func (s *Service) Bargains(ctx context.Context, a Actor) ([]Bargain, error) {
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	rows, err := s.pool.Query(ctx, `SELECT b.id,b.group_id,b.user_id,b.proposed_ppm,b.reason,b.status FROM v3_channelmarket.bargain_requests b JOIN v3_channelmarket.groups g ON g.id=b.group_id WHERE g.owner_user_id=$1 OR $2 ORDER BY b.created_at DESC LIMIT 1000`, a.UserID, a.Admin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Bargain{}
	for rows.Next() {
		var b Bargain
		if err = rows.Scan(&b.ID, &b.GroupID, &b.UserID, &b.ProposedPPM, &b.Reason, &b.Status); err != nil {
			return nil, err
		}
		b.Proposed = json.Number(formatFactor(b.ProposedPPM))
		items = append(items, b)
	}
	return items, rows.Err()
}

type Income struct {
	OwnerID   int64 `json:"owner_user_id"`
	Requests  int64 `json:"request_count"`
	Total     int64 `json:"total_income_micro"`
	Pending   int64 `json:"pending_income_micro"`
	Released  int64 `json:"released_income_micro"`
	Reclaimed int64 `json:"reclaimed_income_micro"`
}

func (s *Service) Income(ctx context.Context, a Actor) ([]Income, error) {
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	rows, err := s.pool.Query(ctx, `SELECT owner_user_id,count(*),sum(net_micro)::bigint,coalesce(sum(net_micro) FILTER(WHERE status='pending'),0)::bigint,coalesce(sum(net_micro-reclaimed_micro) FILTER(WHERE status='released'),0)::bigint,sum(reclaimed_micro)::bigint FROM v3_channelmarket.settlements WHERE owner_user_id=$1 OR $2 GROUP BY owner_user_id ORDER BY owner_user_id`, a.UserID, a.Admin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Income{}
	for rows.Next() {
		var i Income
		if err = rows.Scan(&i.OwnerID, &i.Requests, &i.Total, &i.Pending, &i.Released, &i.Reclaimed); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

type SecurityEvent struct {
	ID          int64           `json:"id"`
	ChannelID   *int64          `json:"channel_id"`
	ActorUserID int64           `json:"actor_user_id"`
	Kind        string          `json:"kind"`
	Status      string          `json:"status"`
	Details     json.RawMessage `json:"details"`
	CreatedAt   time.Time       `json:"created_at"`
}

func (s *Service) Security(ctx context.Context, a Actor) ([]SecurityEvent, error) {
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id,e.channel_id,e.actor_user_id,e.kind,e.status,e.details,e.created_at FROM v3_channelmarket.security_audit_events e LEFT JOIN v3_catalog.channels c ON c.id=e.channel_id WHERE c.owner_user_id=$1 OR $2 ORDER BY e.id DESC LIMIT 1000`, a.UserID, a.Admin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SecurityEvent{}
	for rows.Next() {
		var e SecurityEvent
		if err = rows.Scan(&e.ID, &e.ChannelID, &e.ActorUserID, &e.Kind, &e.Status, &e.Details, &e.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}
func (s *Service) ResolveSecurity(ctx context.Context, a Actor, id int64, status string) error {
	if status != "resolved" && status != "ignored" && status != "open" {
		return ErrInvalid
	}
	if s.pool == nil {
		return ErrUnavailable
	}
	tag, err := s.pool.Exec(ctx, `UPDATE v3_channelmarket.security_audit_events e SET status=$4,updated_at=$5 WHERE e.id=$1 AND ($2 OR EXISTS(SELECT 1 FROM v3_catalog.channels c WHERE c.id=e.channel_id AND c.owner_user_id=$3))`, id, a.Admin, a.UserID, status, s.cfg.Now())
	if err == nil && tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return err
}

func securityTx(ctx context.Context, tx pgx.Tx, a Actor, channel int64, kind string, details map[string]any) error {
	body, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_channelmarket.security_audit_events(channel_id,actor_user_id,kind,details) VALUES(NULLIF($1,0),$2,$3,$4)`, channel, a.UserID, kind, body)
	return err
}
