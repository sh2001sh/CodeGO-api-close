package channelmarket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type MultiplierTarget struct {
	ChannelID       int64  `json:"channel_id"`
	UserID          int64  `json:"user_id"`
	PublicChannelID string `json:"-"`
}

func (t *MultiplierTarget) UnmarshalJSON(raw []byte) error {
	var fields struct {
		Channel json.RawMessage `json:"channel_id"`
		User    int64           `json:"user_id"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&fields) != nil {
		return ErrInvalid
	}
	t.UserID = fields.User
	t.ChannelID = 0
	t.PublicChannelID = ""
	if len(fields.Channel) == 0 {
		return ErrInvalid
	}
	if fields.Channel[0] == '"' {
		if json.Unmarshal(fields.Channel, &t.PublicChannelID) != nil || t.PublicChannelID == "" {
			return ErrInvalid
		}
		return nil
	}
	value, err := strconv.ParseInt(string(fields.Channel), 10, 64)
	if err != nil || value <= 0 {
		return ErrInvalid
	}
	t.ChannelID = value
	return nil
}

type UserMultiplier struct {
	MultiplierTarget
	MultiplierPPM int64     `json:"multiplier_ppm"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (s *Service) SetMultiplier(ctx context.Context, a Actor, channel, user int64, value *json.Number) error {
	var factor int64
	var err error
	if value != nil {
		factor, err = multiplier(*value)
		if err != nil {
			return err
		}
	}
	if user <= 0 {
		return ErrInvalid
	}
	return s.transaction(ctx, func(tx pgx.Tx) error {
		if _, e := owned(ctx, tx, a, channel); e != nil {
			return e
		}
		return setMultiplierTx(ctx, tx, channel, user, factor, "manual")
	})
}

func setMultiplierTx(ctx context.Context, tx pgx.Tx, channel, user, factor int64, source string) error {
	var previous, public int64
	err := tx.QueryRow(ctx, `SELECT multiplier_ppm FROM v3_channelmarket.groups WHERE channel_id=$1`, channel).Scan(&public)
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `SELECT multiplier_ppm FROM v3_channelmarket.user_multipliers WHERE channel_id=$1 AND user_id=$2`, channel, user).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		previous = public
	} else if err != nil {
		return err
	}
	next := factor
	if next == 0 {
		next = public
	}
	if next == previous {
		return nil
	}
	if factor == 0 {
		_, err = tx.Exec(ctx, `DELETE FROM v3_channelmarket.user_multipliers WHERE channel_id=$1 AND user_id=$2`, channel, user)
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO v3_channelmarket.user_multipliers(channel_id,user_id,multiplier_ppm) VALUES($1,$2,$3) ON CONFLICT(channel_id,user_id) DO UPDATE SET multiplier_ppm=EXCLUDED.multiplier_ppm,updated_at=now()`, channel, user, factor)
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_channelmarket.multiplier_notices(channel_id,user_id,previous_ppm,multiplier_ppm,cleared,source) VALUES($1,$2,$3,$4,$5,$6)`, channel, user, previous, next, factor == 0, source)
	return err
}

func (s *Service) Multipliers(ctx context.Context, a Actor) ([]UserMultiplier, error) {
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	rows, err := s.pool.Query(ctx, `SELECT m.channel_id,m.user_id,m.multiplier_ppm,m.updated_at FROM v3_channelmarket.user_multipliers m JOIN v3_channelmarket.groups g ON g.channel_id=m.channel_id WHERE g.owner_user_id=$1 OR $2 ORDER BY m.updated_at DESC LIMIT 1000`, a.UserID, a.Admin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []UserMultiplier{}
	for rows.Next() {
		var m UserMultiplier
		if err = rows.Scan(&m.ChannelID, &m.UserID, &m.MultiplierPPM, &m.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

type TimeMultiplier struct {
	ID            string      `json:"id"`
	ChannelID     int64       `json:"channel_id"`
	Start         int64       `json:"start_timestamp"`
	End           int64       `json:"end_timestamp"`
	Multiplier    json.Number `json:"multiplier"`
	MultiplierPPM int64       `json:"multiplier_ppm"`
	Label         string      `json:"label"`
}

func (s *Service) SaveTimeMultiplier(ctx context.Context, a Actor, m TimeMultiplier) (TimeMultiplier, error) {
	f, err := multiplier(m.Multiplier)
	if err != nil || m.End <= m.Start || m.End-m.Start > 366*86400 || len(m.Label) > 255 {
		return m, ErrInvalid
	}
	m.MultiplierPPM = f
	m.ID, err = newID()
	if err != nil {
		return m, err
	}
	err = s.transaction(ctx, func(tx pgx.Tx) error {
		if _, e := owned(ctx, tx, a, m.ChannelID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO v3_channelmarket.time_range_multipliers(id,channel_id,starts_at,ends_at,multiplier_ppm,label) VALUES($1,$2,$3,$4,$5,$6)`, m.ID, m.ChannelID, time.Unix(m.Start, 0), time.Unix(m.End, 0), f, m.Label)
		return e
	})
	return m, err
}
func (s *Service) DeleteTimeMultiplier(ctx context.Context, a Actor, channel int64, id string) error {
	return s.transaction(ctx, func(tx pgx.Tx) error {
		if _, e := owned(ctx, tx, a, channel); e != nil {
			return e
		}
		tag, e := tx.Exec(ctx, `DELETE FROM v3_channelmarket.time_range_multipliers WHERE id=$1 AND channel_id=$2`, id, channel)
		if e == nil && tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		return e
	})
}

type Bargain struct {
	ID          string      `json:"id"`
	GroupID     string      `json:"group_id"`
	UserID      int64       `json:"user_id"`
	Proposed    json.Number `json:"proposed_multiplier"`
	ProposedPPM int64       `json:"proposed_ppm"`
	Reason      string      `json:"reason"`
	Status      string      `json:"status"`
}

func (s *Service) RequestBargain(ctx context.Context, user int64, b Bargain) (Bargain, error) {
	f, e := multiplier(b.Proposed)
	if e != nil || len(b.Reason) > 1000 {
		return b, ErrInvalid
	}
	b.ID, e = newID()
	if e != nil {
		return b, e
	}
	b.UserID = user
	b.ProposedPPM = f
	b.Status = "pending"
	e = s.transaction(ctx, func(tx pgx.Tx) error {
		if e := accessible(ctx, tx, user, b.GroupID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO v3_channelmarket.bargain_requests(id,group_id,user_id,proposed_ppm,reason) VALUES($1,$2,$3,$4,$5)`, b.ID, b.GroupID, user, f, b.Reason)
		return e
	})
	return b, e
}
func (s *Service) ResolveBargain(ctx context.Context, a Actor, id string, accept bool, note string) error {
	if len(note) > 1000 {
		return ErrInvalid
	}
	return s.transaction(ctx, func(tx pgx.Tx) error {
		var channel, user, f int64
		var status string
		e := tx.QueryRow(ctx, `SELECT g.channel_id,b.user_id,b.proposed_ppm,b.status FROM v3_channelmarket.bargain_requests b JOIN v3_channelmarket.groups g ON g.id=b.group_id WHERE b.id=$1 AND ($2 OR g.owner_user_id=$3) FOR UPDATE OF g,b`, id, a.Admin, a.UserID).Scan(&channel, &user, &f, &status)
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		if status != "pending" {
			return ErrConflict
		}
		status = "rejected"
		if accept {
			status = "accepted"
			if e = setMultiplierTx(ctx, tx, channel, user, f, "bargain"); e != nil {
				return e
			}
		}
		_, e = tx.Exec(ctx, `UPDATE v3_channelmarket.bargain_requests SET status=$2,resolution_note=$3,resolved_at=$4 WHERE id=$1 AND status='pending'`, id, status, note, s.cfg.Now())
		return e
	})
}
