package marketplace

import "net/http"

func (s *Service) registerBatchRoutes(register registerFunc) {
	register("GET /api/blind-box/batches", false, func(w http.ResponseWriter, r *http.Request, user int64) {
		out, err := s.BatchOverview(r.Context(), user)
		reply(w, out, err)
	})
	register("POST /api/blind-box/batches/{id}/draw", false, func(w http.ResponseWriter, r *http.Request, user int64) {
		var in struct {
			RequestID string `json:"request_id"`
			Count     int    `json:"count"`
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Count == 0 {
			in.Count = 1
		}
		out, err := s.DrawBatch(r.Context(), user, pathID(r), in.RequestID, in.Count)
		reply(w, out, err)
	})
	register("GET /api/blind-box/admin/batches", true, func(w http.ResponseWriter, r *http.Request, _ int64) {
		out, err := s.ListBatches(r.Context(), true)
		reply(w, out, err)
	})
	register("PUT /api/blind-box/admin/batches", true, func(w http.ResponseWriter, r *http.Request, user int64) {
		var in Batch
		if !decode(w, r, &in) {
			return
		}
		out, err := s.SaveBatch(r.Context(), user, in)
		reply(w, out, err)
	})
	for _, action := range []string{"publish", "pause"} {
		register("POST /api/blind-box/admin/batches/{id}/"+action, true, func(w http.ResponseWriter, r *http.Request, user int64) {
			var in struct {
				RequestID string `json:"request_id"`
				Revision  int64  `json:"revision"`
			}
			if !decode(w, r, &in) {
				return
			}
			out, err := s.ChangeBatchState(r.Context(), user, pathID(r), in.Revision, in.RequestID, action == "publish")
			reply(w, out, err)
		})
	}
	register("GET /api/blind-box/admin/batches/{id}/stats", true, func(w http.ResponseWriter, r *http.Request, _ int64) {
		out, err := s.BatchStats(r.Context(), pathID(r))
		reply(w, out, err)
	})
}
