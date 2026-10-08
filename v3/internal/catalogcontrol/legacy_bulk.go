package catalogcontrol

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

type legacyChannelBatch struct {
	IDs []int64 `json:"ids"`
	Tag *string `json:"tag"`
}

func validBulkIDs(ids []int64) bool {
	if len(ids) == 0 || len(ids) > 10000 {
		return false
	}
	for _, id := range ids {
		if id <= 0 {
			return false
		}
	}
	return true
}

func validLegacyTag(tag string) bool {
	return strings.TrimSpace(tag) != "" && len(tag) <= 255
}

func (s *Server) legacyDeleteChannelBatch(w http.ResponseWriter, r *http.Request) {
	var req legacyChannelBatch
	if !decode(w, r, &req) {
		return
	}
	if !validBulkIDs(req.IDs) {
		fail(w, 400, "invalid_ids", "Expected 1 to 10000 positive channel identifiers")
		return
	}
	tag, err := s.pool.Exec(r.Context(), `DELETE FROM v3_catalog.channels WHERE id=ANY($1::bigint[])`, req.IDs)
	if err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, tag.RowsAffected())
}

func (s *Server) legacyDeleteDisabledChannels(w http.ResponseWriter, r *http.Request) {
	tag, err := s.pool.Exec(r.Context(), `DELETE FROM v3_catalog.channels WHERE status IN ('disabled','auto_disabled')`)
	if err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, tag.RowsAffected())
}

func (s *Server) legacyBatchSetChannelTag(w http.ResponseWriter, r *http.Request) {
	var req legacyChannelBatch
	if !decode(w, r, &req) {
		return
	}
	if !validBulkIDs(req.IDs) || (req.Tag != nil && len(*req.Tag) > 255) {
		fail(w, 400, "invalid_batch", "Invalid channel identifiers or tag")
		return
	}
	tag, err := s.pool.Exec(r.Context(), `UPDATE v3_catalog.channels SET tag=$2 WHERE id=ANY($1::bigint[])`, req.IDs, req.Tag)
	if err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, tag.RowsAffected())
}

func (s *Server) legacyDisableTagChannels(w http.ResponseWriter, r *http.Request) {
	s.legacySetTagStatus(w, r, "disabled")
}

func (s *Server) legacyEnableTagChannels(w http.ResponseWriter, r *http.Request) {
	s.legacySetTagStatus(w, r, "enabled")
}

func (s *Server) legacySetTagStatus(w http.ResponseWriter, r *http.Request, status string) {
	var req struct {
		Tag string `json:"tag"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !validLegacyTag(req.Tag) {
		fail(w, 400, "invalid_tag", "A nonempty channel tag is required")
		return
	}
	err := pgx.BeginFunc(r.Context(), s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT id FROM v3_catalog.channels WHERE tag=$1 ORDER BY id`, req.Tag)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		// All groups are locked in the same order as single-channel transitions.
		rows, err = tx.Query(r.Context(), `SELECT channel_id FROM v3_channelmarket.groups
			WHERE channel_id=ANY($1::bigint[]) ORDER BY id FOR UPDATE`, ids)
		if err != nil {
			return err
		}
		channels, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		for _, id := range channels {
			if err = prepareMarketStatusTx(r.Context(), tx, id, status); err != nil {
				return err
			}
		}
		// Newly tagged channels must not slip past the verification checks above.
		if _, err = tx.Exec(r.Context(), `UPDATE v3_catalog.channels SET status=$2 WHERE id=ANY($1::bigint[])`, ids, status); err != nil {
			return err
		}
		for _, id := range channels {
			if err = syncMarketStatusTx(r.Context(), tx, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, nil)
}

func (s *Server) legacyGetTagModels(w http.ResponseWriter, r *http.Request) {
	tag := r.URL.Query().Get("tag")
	if !validLegacyTag(tag) {
		fail(w, 400, "invalid_tag", "A nonempty channel tag is required")
		return
	}
	var models string
	err := s.pool.QueryRow(r.Context(), `SELECT coalesce((SELECT coalesce(string_agg(cm.model,',' ORDER BY cm.model),'')
	 FROM v3_catalog.channels c LEFT JOIN v3_catalog.channel_models cm ON cm.channel_id=c.id
	 WHERE c.tag=$1 GROUP BY c.id ORDER BY count(cm.model) DESC,c.id LIMIT 1),'')`, tag).Scan(&models)
	if err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, models)
}

func validateLegacyMemberships(raw *string) ([]string, error) {
	// v2 tag forms use empty strings to leave memberships unchanged.
	if raw == nil || *raw == "" {
		return nil, nil
	}
	items := splitList(*raw)
	if len(items) > 10000 {
		return nil, fmt.Errorf("too many group or model memberships")
	}
	for _, item := range items {
		if len(item) > 255 {
			return nil, fmt.Errorf("invalid group or model name")
		}
	}
	return items, nil
}
