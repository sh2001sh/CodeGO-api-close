package incentives

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

type Actor struct {
	UserID int64
	Role   string
}
type Authenticate func(*http.Request) (Actor, error)

func (s *Service) Register(mux *http.ServeMux, authenticate Authenticate) {
	bind := func(pattern string, admin bool, next func(http.ResponseWriter, *http.Request, Actor)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if authenticate == nil {
				reply(w, nil, errUnauthorized)
				return
			}
			a, err := authenticate(r)
			if err != nil || a.UserID <= 0 {
				reply(w, nil, errUnauthorized)
				return
			}
			if admin && a.Role != "admin" && a.Role != "root" {
				reply(w, nil, errForbidden)
				return
			}
			if r.Method != "GET" {
				origin := r.Header.Get("Origin")
				if origin != "" {
					u, err := url.Parse(origin)
					if err != nil || u.Host != r.Host {
						reply(w, nil, errForbidden)
						return
					}
				}
			}
			next(w, r, a)
		})
	}
	bind("GET /api/user/aff/rewards", false, func(w http.ResponseWriter, r *http.Request, a Actor) {
		v, e := s.AffiliateRewards(r.Context(), a.UserID)
		reply(w, v, e)
	})
	bind("GET /api/user/aff/overview", false, func(w http.ResponseWriter, r *http.Request, a Actor) {
		v, e := s.AffiliateRewards(r.Context(), a.UserID)
		reply(w, v, e)
	})
	bind("GET /api/user/aff", false, func(w http.ResponseWriter, r *http.Request, a Actor) {
		v, e := s.AffiliateCode(r.Context(), a.UserID)
		reply(w, v, e)
	})
	bind("GET /api/subscription/self/reset-opportunity", false, func(w http.ResponseWriter, r *http.Request, a Actor) {
		v, e := s.ResetOpportunities(r.Context(), a.UserID)
		reply(w, v, e)
	})
	bind("POST /api/subscription/self/reset-opportunity/use", false, func(w http.ResponseWriter, r *http.Request, a Actor) {
		v, e := s.UseReset(r.Context(), a.UserID)
		reply(w, v, e)
	})
	s.registerReferralHTTP(bind)
}

var errUnauthorized = errors.New("authentication required")
var errForbidden = errors.New("permission denied")

func reply(w http.ResponseWriter, data any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
		return
	}
	status, message := 500, "incentive operation failed"
	switch {
	case errors.Is(err, ErrInvalid):
		status, message = 400, "invalid request"
	case errors.Is(err, ErrNotFound):
		status, message = 404, "no eligible record found"
	case errors.Is(err, ErrMonthlyUsed):
		status, message = 409, "reset already used this month"
	case errors.Is(err, ErrUnavailable):
		status, message = 409, "reset opportunities unavailable"
	case errors.Is(err, ErrReferralConflict):
		status, message = 409, "referral terms or budget changed"
	case errors.Is(err, ErrRetired):
		status, message = http.StatusGone, "daily lucky number retired; historical rewards remain available"
	case errors.Is(err, commerce.ErrFundingPending):
		status, message = 503, "subscription settlement pending; retry later"
	case errors.Is(err, commerce.ErrStateConflict):
		status, message = 409, "subscription cannot be reset in its current state"
	case errors.Is(err, errUnauthorized):
		status, message = 401, err.Error()
	case errors.Is(err, errForbidden):
		status, message = 403, err.Error()
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": message})
}
func decodeBody(w http.ResponseWriter, r *http.Request, target any) error {
	defer func() { _ = r.Body.Close() }()
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return ErrInvalid
	}
	return nil
}
func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrInvalid
	}
	return id, nil
}
func paging(r *http.Request) (int, int, error) {
	p, n := 1, 20
	for key, target := range map[string]*int{"page": &p, "p": &p, "page_size": &n} {
		if v := r.URL.Query().Get(key); v != "" {
			parsed, err := strconv.Atoi(v)
			if err != nil || parsed < 1 || parsed > 1000000 {
				return 0, 0, ErrInvalid
			}
			*target = parsed
		}
	}
	return p, n, nil
}
