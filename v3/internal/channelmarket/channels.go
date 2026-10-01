package channelmarket

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
)

func (s *Service) Create(ctx context.Context, owner int64, r CreateRequest) (ChannelView, error) {
	var c ChannelView
	if owner <= 0 {
		return c, ErrInvalid
	}
	factor, err := r.validate()
	if err != nil {
		return c, err
	}
	if s.enc == nil {
		return c, ErrUnavailable
	}
	id, err := newID()
	if err != nil {
		return c, err
	}
	ciphertext, err := s.enc.Encrypt([]byte(r.APIKey))
	if err != nil {
		return c, err
	}
	err = s.transaction(ctx, func(tx pgx.Tx) error {
		var e error
		c, e = createChannelRowsTx(ctx, tx, owner, id, factor, ciphertext, r)
		return e
	})
	return c, err
}

// createChannelRowsTx creates the catalog group, channel, credentials,
// channel-group binding, declared models, and the market group row for a new
// marketplace channel, then returns its freshly scanned view.
func createChannelRowsTx(ctx context.Context, tx pgx.Tx, owner int64, id string, factor int64, ciphertext []byte, r CreateRequest) (ChannelView, error) {
	var c ChannelView
	name := r.Name
	if name == "" {
		name = r.SourceLabel
	}
	if name == "" {
		name = r.Provider
	}
	group := "market_" + id
	slug := "mg_" + id[:12]
	if _, e := tx.Exec(ctx, `INSERT INTO v3_catalog.groups(name,description,multiplier) VALUES($1,$2,$3::numeric/1000000)`, group, name, factor); e != nil {
		return c, e
	}
	market, e := safeSettings(r)
	if e != nil {
		return c, e
	}
	settings, e := json.Marshal(map[string]any{"community": map[string]any{"id": id, "slug": slug, "name": name, "visibility": r.Visibility, "lifecycle_status": "draft", "verification_status": "pending"}, "market": market})
	if e != nil {
		return c, e
	}
	var channel int64
	e = tx.QueryRow(ctx, `INSERT INTO v3_catalog.channels(name,provider,base_url,status,scope,owner_user_id,max_concurrency,max_user_concurrency,multiplier_card_supported,settings) VALUES($1,$2,$3,'disabled','marketplace',$4,$5,$6,$7,$8) RETURNING id`, name, nativeProvider(r.Provider), r.BaseURL, owner, r.MaxConcurrency, r.UserMaxConcurrency, r.MultiplierCardSupported, settings).Scan(&channel)
	if e != nil {
		return c, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO v3_catalog.channel_credentials(channel_id,secret) VALUES($1,$2)`, channel, ciphertext); e != nil {
		return c, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES($1,$2)`, channel, group); e != nil {
		return c, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO v3_catalog.channel_models(channel_id,model) SELECT $1,unnest($2::text[])`, channel, r.Models); e != nil {
		return c, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO v3_channelmarket.groups(id,public_channel_id,channel_id,owner_user_id,public_slug,internal_group_name,display_name,source_label,multiplier_ppm,visibility,model_prices) VALUES($1,$11,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, channel, owner, slug, group, name, r.SourceLabel, factor, r.Visibility, r.Prices, strconv.FormatInt(channel, 10)); e != nil {
		return c, e
	}
	if _, e = tx.Exec(ctx, `UPDATE v3_catalog.channels SET settings=jsonb_set(settings,'{community,id}',to_jsonb($2::text)) WHERE id=$1`, channel, strconv.FormatInt(channel, 10)); e != nil {
		return c, e
	}
	return scanChannel(tx.QueryRow(ctx, `SELECT `+channelColumns+channelFrom+` WHERE g.id=$1`, id))
}

func (s *Service) List(ctx context.Context, a Actor, mine bool) ([]ChannelView, error) {
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	rows, err := s.pool.Query(ctx, `SELECT `+channelColumns+channelFrom+` WHERE g.deleted_at IS NULL AND
	(($1 AND ($2 OR g.owner_user_id=$3)) OR (NOT $1 AND g.lifecycle_status='active' AND c.status='enabled' AND (g.visibility='public' OR g.owner_user_id=$3 OR EXISTS(SELECT 1 FROM v3_channelmarket.group_access a WHERE a.group_id=g.id AND a.user_id=$3))))
	AND ($1 OR NOT EXISTS(SELECT 1 FROM v3_channelmarket.channel_user_blocks b WHERE b.channel_id=c.id AND b.user_id=$3))
	ORDER BY g.created_at DESC,g.id LIMIT 1000`, mine, a.Admin, a.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChannelView{}
	for rows.Next() {
		c, e := scanChannel(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, a Actor, id string) (ChannelView, error) {
	var c ChannelView
	if s.pool == nil {
		return c, ErrUnavailable
	}
	c, err := scanChannel(s.pool.QueryRow(ctx, `SELECT `+channelColumns+channelFrom+` WHERE (g.id=$1 OR g.public_slug=$1 OR g.public_channel_id=$1)
	AND g.deleted_at IS NULL AND ($2 OR g.owner_user_id=$3 OR (g.lifecycle_status='active' AND c.status='enabled' AND
	(g.visibility='public' OR EXISTS(SELECT 1 FROM v3_channelmarket.group_access a WHERE a.group_id=g.id AND a.user_id=$3))
	AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.channel_user_blocks b WHERE b.channel_id=c.id AND b.user_id=$3)))`, id, a.Admin, a.UserID))
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return c, err
}

func (s *Service) ChannelID(ctx context.Context, a Actor, id string) (int64, error) {
	c, err := s.Get(ctx, a, id)
	return c.InternalChannelID, err
}

func (s *Service) Transition(ctx context.Context, a Actor, channel int64, action, reason string) error {
	return s.transaction(ctx, func(tx pgx.Tx) error {
		id, err := owned(ctx, tx, a, channel)
		if err != nil {
			return err
		}
		var from, status, verification string
		if err = tx.QueryRow(ctx, `SELECT lifecycle_status,verification_status FROM v3_channelmarket.groups WHERE id=$1`, id).Scan(&from, &verification); err != nil {
			return err
		}
		switch action {
		case "pause":
			if from != "active" {
				return ErrConflict
			}
			status = "paused"
		case "resume":
			if from != "paused" || verification != "passed" {
				return ErrConflict
			}
			status = "active"
		case "approve":
			if !a.Admin || verification != "passed" || from == "deleted" {
				return ErrConflict
			}
			status = "active"
		case "reject":
			if !a.Admin {
				return ErrNotFound
			}
			status = "rejected"
		case "delete":
			status = "deleted"
		default:
			return ErrInvalid
		}
		if _, err = tx.Exec(ctx, `UPDATE v3_channelmarket.groups SET lifecycle_status=$2,review_reason=$3,published_at=CASE WHEN $2='active' THEN coalesce(published_at,$4) ELSE published_at END,deleted_at=CASE WHEN $2='deleted' THEN $4 ELSE deleted_at END WHERE id=$1 AND lifecycle_status=$5`, id, status, reason, s.cfg.Now(), from); err != nil {
			return err
		}
		catalogStatus := "disabled"
		if status == "active" {
			catalogStatus = "enabled"
		}
		_, err = tx.Exec(ctx, `UPDATE v3_catalog.channels SET status=$2,settings=jsonb_set(settings,'{community,lifecycle_status}',to_jsonb($3::text)) WHERE id=$1`, channel, catalogStatus, status)
		if err != nil {
			return err
		}
		if action == "approve" || action == "reject" {
			labelStatus := "rejected"
			if action == "approve" {
				labelStatus = "approved"
			}
			if _, err = tx.Exec(ctx, `UPDATE v3_catalog.channels SET settings=jsonb_set(settings,'{market}',coalesce(settings->'market','{}')||jsonb_build_object('source_label_status',$2::text,'source_label_review_reason',$3::text)) WHERE id=$1`, channel, labelStatus, reason); err != nil {
				return err
			}
		}
		return securityTx(ctx, tx, a, channel, "channel_"+action, map[string]any{"from": from, "to": status, "reason": reason})
	})
}
