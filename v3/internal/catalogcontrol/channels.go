package catalogcontrol

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
)

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	p, n := page(r)
	filter := r.URL.Query().Get("keyword")
	var total int64
	if err := s.pool.QueryRow(r.Context(), `SELECT count(*) FROM v3_catalog.channels WHERE ($1='' OR name ILIKE '%'||$1||'%')`, filter).Scan(&total); err != nil {
		s.dbError(w, err)
		return
	}
	rows, err := s.pool.Query(r.Context(), channelSelect+` WHERE ($1='' OR c.name ILIKE '%'||$1||'%') ORDER BY c.id DESC LIMIT $2 OFFSET $3`, filter, n, (p-1)*n)
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer rows.Close()
	items := make([]Channel, 0, n)
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			s.dbError(w, err)
			return
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items, "total": total, "page": p, "page_size": n})
}

func (s *Server) getChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	c, err := scanChannel(s.pool.QueryRow(r.Context(), channelSelect+` WHERE c.id=$1`, id))
	if err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, c)
}

func (s *Server) saveChannel(w http.ResponseWriter, r *http.Request) {
	var c Channel
	if !decode(w, r, &c) {
		return
	}
	if err := c.validate(); err != nil {
		fail(w, 400, "invalid_channel", err.Error())
		return
	}
	if r.Method == http.MethodPut {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		if c.ID != 0 && c.ID != id {
			fail(w, 400, "invalid_id", "Body and path identifiers differ")
			return
		}
		c.ID = id
	} else if c.ID != 0 {
		fail(w, 400, "invalid_id", "Identifiers are allocated by the server")
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	if err = s.writeChannel(r.Context(), tx, &c); err != nil {
		s.dbError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, map[string]any{"id": c.ID})
}

func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	tag, err := s.pool.Exec(r.Context(), `DELETE FROM v3_catalog.channels WHERE id=$1`, id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		s.dbError(w, pgx.ErrNoRows)
		return
	}
	respond(w, 200, nil)
}

func (s *Server) writeChannel(ctx context.Context, tx pgx.Tx, c *Channel) error {
	var err error
	if c.ID != 0 {
		if err = prepareMarketStatusTx(ctx, tx, c.ID, c.Status); err != nil {
			return err
		}
	}
	args := []any{c.Name, c.Provider, c.BaseURL, c.ProxyURL, c.Status, c.Scope, c.OwnerUserID, c.Priority, c.Weight, c.MaxConcurrency, c.MaxUserConcurrency, c.AutoDisable, c.MultiplierCardSupported, c.ModelMapping, c.ParamOverride, c.HeaderOverride, c.StatusCodeMapping, c.Settings, c.Tag, c.Remark}
	if c.ID == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO v3_catalog.channels(name,provider,base_url,proxy_url,status,scope,owner_user_id,priority,weight,max_concurrency,max_user_concurrency,auto_disable,multiplier_card_supported,model_mapping,param_override,header_override,status_code_mapping,settings,tag,remark)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,coalesce($14::jsonb,'{}'),$15,coalesce($16::jsonb,'{}'),$17,$18,$19,$20) RETURNING id`, args...).Scan(&c.ID)
	} else {
		args = append(args, c.ID)
		tag, e := tx.Exec(ctx, `UPDATE v3_catalog.channels SET name=$1,provider=$2,base_url=$3,proxy_url=$4,status=$5,scope=$6,owner_user_id=$7,priority=$8,weight=$9,max_concurrency=$10,max_user_concurrency=$11,auto_disable=$12,multiplier_card_supported=$13,model_mapping=coalesce($14::jsonb,'{}'),param_override=$15,header_override=coalesce($16::jsonb,'{}'),status_code_mapping=$17,settings=$18,tag=$19,remark=$20 WHERE id=$21`, args...)
		err = e
		if err == nil && tag.RowsAffected() == 0 {
			err = pgx.ErrNoRows
		}
	}
	if err != nil {
		return err
	}
	if c.Groups != nil {
		if _, err = tx.Exec(ctx, `DELETE FROM v3_catalog.channel_groups WHERE channel_id=$1`, c.ID); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO v3_catalog.channel_groups(channel_id,group_name) SELECT $1,unnest($2::text[]) ON CONFLICT DO NOTHING`, c.ID, c.Groups)
		}
		if err != nil {
			return err
		}
	}
	if c.Models != nil {
		if _, err = tx.Exec(ctx, `DELETE FROM v3_catalog.channel_models WHERE channel_id=$1`, c.ID); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO v3_catalog.channel_models(channel_id,model) SELECT $1,unnest($2::text[]) ON CONFLICT DO NOTHING`, c.ID, c.Models)
		}
		if err != nil {
			return err
		}
	}
	if c.Credentials != nil {
		if s.enc == nil {
			return fmt.Errorf("credential encryption is unavailable")
		}
		if !c.AppendCredentials {
			if _, err = tx.Exec(ctx, `DELETE FROM v3_catalog.channel_credentials WHERE channel_id=$1`, c.ID); err != nil {
				return err
			}
		}
		for _, cr := range c.Credentials {
			ciphertext, e := s.enc.Encrypt([]byte(cr.Secret))
			if e != nil {
				return e
			}
			if _, err = tx.Exec(ctx, `INSERT INTO v3_catalog.channel_credentials(channel_id,kind,secret,expires_at,max_concurrency,fingerprint) VALUES($1,$2,$3,$4,$5,coalesce($6::jsonb,'{}'))`, c.ID, cr.Kind, ciphertext, cr.ExpiresAt, cr.MaxConcurrency, cr.Fingerprint); err != nil {
				return err
			}
		}
	}
	return syncMarketStatusTx(ctx, tx, c.ID)
}
