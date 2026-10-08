package adminops

import (
	"net/http"
)

func (s *Server) registerFavorites(mux *http.ServeMux, auth Authenticate) {
	for _, p := range []string{"/api/models/favorites", "/api/models/favorites/{$}"} {
		mux.HandleFunc("GET "+p, s.protected(auth, "user", s.getFavorites))
		mux.HandleFunc("PUT "+p, s.protected(auth, "user", s.saveFavorite))
	}
}
func (s *Server) getFavorites(w http.ResponseWriter, r *http.Request, a Actor) {
	rows, err := s.pool.Query(r.Context(), `SELECT f.model_id,m.model_name FROM v3_adminops.model_favorites f JOIN v3_catalog.models m ON m.id=f.model_id WHERE f.user_id=$1 AND m.deleted_at IS NULL ORDER BY f.model_id`, a.UserID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer rows.Close()
	ids := make([]int64, 0)
	models := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var name string
		if err = rows.Scan(&id, &name); err != nil {
			s.dbError(w, err)
			return
		}
		ids = append(ids, id)
		models = append(models, map[string]any{"id": id, "model_name": name})
	}
	if err = rows.Err(); err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, map[string]any{"model_ids": ids, "models": models})
}
func (s *Server) saveFavorite(w http.ResponseWriter, r *http.Request, a Actor) {
	var in struct {
		ModelID  int64 `json:"model_id"`
		Favorite bool  `json:"favorite"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.ModelID <= 0 {
		fail(w, 400, "invalid_model", "Expected a positive model ID")
		return
	}
	if in.Favorite {
		tag, err := s.pool.Exec(r.Context(), `INSERT INTO v3_adminops.model_favorites(user_id,model_id) SELECT $1,id FROM v3_catalog.models WHERE id=$2 AND deleted_at IS NULL ON CONFLICT DO NOTHING`, a.UserID, in.ModelID)
		if err != nil {
			s.dbError(w, err)
			return
		}
		if tag.RowsAffected() == 0 {
			var exists bool
			err = s.pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM v3_catalog.models WHERE id=$1 AND deleted_at IS NULL)`, in.ModelID).Scan(&exists)
			if err != nil {
				s.dbError(w, err)
				return
			}
			if !exists {
				fail(w, 404, "not_found", "Model does not exist")
				return
			}
		}
	} else {
		if _, err := s.pool.Exec(r.Context(), `DELETE FROM v3_adminops.model_favorites WHERE user_id=$1 AND model_id=$2`, a.UserID, in.ModelID); err != nil {
			s.dbError(w, err)
			return
		}
	}
	s.getFavorites(w, r, a)
}
