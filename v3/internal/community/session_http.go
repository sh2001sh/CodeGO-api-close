package community

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// RegisterSessionRoutes exposes browser operations with session identity. The
// service-secret bridge remains separate and never receives browser credentials.
func (s *Service) RegisterSessionRoutes(mux *http.ServeMux, authenticate func(*http.Request) (int64, error), subject func(context.Context, int64) (string, error)) {
	protect := func(next func(http.ResponseWriter, *http.Request, int64)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			if authenticate == nil {
				bridgeError(w, ErrUnauthorized)
				return
			}
			uid, err := authenticate(r)
			if err != nil || uid <= 0 {
				bridgeError(w, ErrUnauthorized)
				return
			}
			next(w, r, uid)
		}
	}
	mux.HandleFunc("GET /api/community/sellers", protect(func(w http.ResponseWriter, r *http.Request, _ int64) {
		s.sellersHTTP(w, r)
	}))
	mux.HandleFunc("GET /api/marketplace/groups/{id}/rating", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		var uid int64
		if authenticate != nil {
			if user, err := authenticate(r); err == nil && user > 0 {
				uid = user
			}
		}
		result, err := s.GetMarketRating(r.Context(), r.PathValue("id"), uid)
		bridgeResult(w, result, err)
	})
	rate := protect(func(w http.ResponseWriter, r *http.Request, uid int64) {
		var body struct {
			Stars int `json:"stars"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(&body) != nil || !errors.Is(d.Decode(new(any)), io.EOF) || body.Stars < 1 || body.Stars > 5 {
			bridgeError(w, ErrInvalidRating)
			return
		}
		if subject == nil {
			bridgeError(w, ErrUnavailable)
			return
		}
		viewer, err := subject(r.Context(), uid)
		if err != nil {
			bridgeError(w, err)
			return
		}
		result, err := s.RateMarketGroup(r.Context(), r.PathValue("id"), uid, viewer, body.Stars)
		bridgeResult(w, result, err)
	})
	mux.HandleFunc("POST /api/marketplace/groups/{id}/rating", rate)
	// Existing main-site browser links retain session authentication.
	mux.HandleFunc("POST /api/community/channels/{id}/rating", rate)
}
