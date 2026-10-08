package channelmarket

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type GroupFavorite struct {
	GroupID    string       `json:"group_id"`
	ID         string       `json:"id"`
	PublicSlug string       `json:"public_slug,omitempty"`
	Name       string       `json:"system_display_name,omitempty"`
	Models     []string     `json:"declared_models,omitempty"`
	Multiplier *json.Number `json:"multiplier,omitempty"`
	Available  bool         `json:"available"`
	CreatedAt  time.Time    `json:"created_at"`
}

type GroupFavoritePage struct {
	Items      []GroupFavorite
	Pagination BrowsePagination
}

// ListGroupFavorites retains a removable bookmark when access is revoked, but
// never returns that group's name, models, price or private slug to the user.
func (s *Service) ListGroupFavorites(ctx context.Context, a Actor, page, size int, ids []string) (GroupFavoritePage, error) {
	result := GroupFavoritePage{Items: []GroupFavorite{}, Pagination: BrowsePagination{Page: page, PageSize: size}}
	if a.UserID <= 0 || page < 1 || page > 1_000_000 || size < 1 || size > 100 || len(ids) > 100 {
		return result, ErrInvalid
	}
	if s.pool == nil {
		return result, ErrUnavailable
	}
	if ids == nil {
		ids = []string{}
	}
	where := `f.user_id=$1 AND (cardinality($2::text[])=0 OR f.group_id=ANY($2::text[]))`
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM v3_channelmarket.group_favorites f WHERE `+where, a.UserID, ids).Scan(&result.Pagination.Total); err != nil {
		return result, err
	}
	rows, err := s.pool.Query(ctx, `SELECT f.group_id,g.public_channel_id,g.public_slug,g.display_name,
 ARRAY(SELECT model FROM v3_catalog.channel_models WHERE channel_id=c.id ORDER BY model),
 g.multiplier_ppm,(`+browseAccess+`),f.created_at
 FROM v3_channelmarket.group_favorites f JOIN v3_channelmarket.groups g ON g.id=f.group_id
 JOIN v3_catalog.channels c ON c.id=g.channel_id WHERE `+where+`
 ORDER BY f.created_at DESC,f.group_id LIMIT $3 OFFSET $4`, a.UserID, ids, size, (page-1)*size)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item GroupFavorite
		var slug, name string
		var models []string
		var ppm int64
		if err = rows.Scan(&item.GroupID, &item.ID, &slug, &name, &models, &ppm, &item.Available, &item.CreatedAt); err != nil {
			return result, err
		}
		if item.Available {
			item.PublicSlug = slug
			item.Name = name
			item.Models = models
			n := json.Number(formatFactor(ppm))
			item.Multiplier = &n
		}
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

func (s *Service) SaveGroupFavorite(ctx context.Context, a Actor, id string, favorite bool) error {
	if a.UserID <= 0 || id == "" || len(id) > 128 || strings.TrimSpace(id) != id {
		return ErrInvalid
	}
	if s.pool == nil {
		return ErrUnavailable
	}
	if !favorite {
		_, err := s.pool.Exec(ctx, `DELETE FROM v3_channelmarket.group_favorites WHERE user_id=$1 AND group_id=$2`, a.UserID, id)
		return err
	}
	// Visibility and blocks are checked in the same statement as the insert.
	tag, err := s.pool.Exec(ctx, `INSERT INTO v3_channelmarket.group_favorites(user_id,group_id)
 SELECT $1,g.id `+channelFrom+` WHERE g.id=$2 AND `+browseAccess+`
 ON CONFLICT(user_id,group_id) DO UPDATE SET group_id=excluded.group_id`, a.UserID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) httpGroupFavorites(w http.ResponseWriter, r *http.Request, a Actor) {
	options, err := parseBrowse(r)
	if err != nil {
		s.result(w, nil, err)
		return
	}
	var ids []string
	if value := r.URL.Query().Get("group_ids"); value != "" {
		if len(value) > 12900 {
			s.result(w, nil, ErrInvalid)
			return
		}
		ids = strings.Split(value, ",")
		if len(ids) > 100 {
			s.result(w, nil, ErrInvalid)
			return
		}
		for _, id := range ids {
			if id == "" || len(id) > 128 || strings.TrimSpace(id) != id {
				s.result(w, nil, ErrInvalid)
				return
			}
		}
	}
	result, err := s.ListGroupFavorites(r.Context(), a, options.Page, options.PageSize, ids)
	if err != nil {
		s.result(w, nil, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "", "data": result.Items, "pagination": result.Pagination})
}

func (s *Service) httpSaveGroupFavorite(w http.ResponseWriter, r *http.Request, a Actor) {
	var input struct {
		GroupID  string `json:"group_id"`
		Favorite *bool  `json:"favorite"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Favorite == nil {
		s.result(w, nil, ErrInvalid)
		return
	}
	err := s.SaveGroupFavorite(r.Context(), a, input.GroupID, *input.Favorite)
	s.result(w, map[string]any{"group_id": input.GroupID, "favorite": *input.Favorite}, err)
}
