package marketplace

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// Authenticate is injected by cmd/control; the domain does not own sessions.
type Authenticate func(*http.Request) (userID int64, admin bool, err error)

// registerFunc wires a pattern to fn behind the given auth/admin requirements.
type registerFunc func(pattern string, admin bool, fn func(http.ResponseWriter, *http.Request, int64))

func (s *Service) Handler(auth Authenticate) http.Handler {
	mux := http.NewServeMux()
	register := func(pattern string, admin bool, fn func(http.ResponseWriter, *http.Request, int64)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if auth == nil {
				reply(w, nil, ErrUnavailable)
				return
			}
			id, isAdmin, err := auth(r)
			if err != nil || id <= 0 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if admin && !isAdmin {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			fn(w, r, id)
		})
	}
	s.registerGroupBuyRoutes(register)
	s.registerBlindBoxRoutes(register)
	s.registerBlindBoxCompat(register)
	s.registerBatchRoutes(register)
	return mux
}

func (s *Service) registerGroupBuyRoutes(register registerFunc) {
	register("GET /api/group-buy/list", false, func(w http.ResponseWriter, r *http.Request, _ int64) {
		g, e := s.ListGroups(r.Context(), 0, queryID(r, "before"), 30)
		reply(w, g, e)
	})
	register("GET /api/group-buy/mine", false, func(w http.ResponseWriter, r *http.Request, id int64) {
		g, e := s.ListGroups(r.Context(), id, queryID(r, "before"), 30)
		reply(w, g, e)
	})
	register("GET /api/group-buy/{id}", false, func(w http.ResponseWriter, r *http.Request, _ int64) {
		g, e := s.GetGroup(r.Context(), pathID(r))
		reply(w, g, e)
	})
	register("POST /api/group-buy/create", false, func(w http.ResponseWriter, r *http.Request, id int64) {
		var in struct {
			OrderID int64 `json:"order_id"`
		}
		if !decode(w, r, &in) {
			return
		}
		g, e := s.CreateGroup(r.Context(), id, in.OrderID)
		reply(w, g, e)
	})
	register("POST /api/group-buy/join", false, func(w http.ResponseWriter, r *http.Request, id int64) {
		var in struct {
			GroupID int64 `json:"group_buy_id"`
			OrderID int64 `json:"order_id"`
		}
		if !decode(w, r, &in) {
			return
		}
		g, e := s.JoinGroup(r.Context(), id, in.GroupID, in.OrderID)
		reply(w, g, e)
	})
}

func (s *Service) registerBlindBoxRoutes(register registerFunc) {
	for _, path := range []string{"/api/blind-box/self", "/api/blind-box/inventory/overview"} {
		register("GET "+path, false, func(w http.ResponseWriter, r *http.Request, id int64) {
			o, e := s.Overview(r.Context(), id)
			reply(w, o, e)
		})
	}
	register("GET /api/blind-box/history", false, func(w http.ResponseWriter, r *http.Request, id int64) {
		o, e := s.History(r.Context(), id, queryID(r, "before"), 30)
		reply(w, o, e)
	})
	register("POST /api/blind-box/inventory/open", false, func(w http.ResponseWriter, r *http.Request, id int64) {
		var in struct {
			RequestID       string `json:"request_id"`
			Count           int    `json:"count"`
			PoolID          int64  `json:"pool_id"`
			DrawCurrentPool *bool  `json:"draw_current_pool"`
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Count == 0 {
			in.Count = 1
		}
		o, e := s.OpenBoxesFromInventory(r.Context(), id, in.RequestID, in.Count, in.PoolID, in.DrawCurrentPool)
		reply(w, o, e)
	})
	register("POST /api/blind-box/props/{id}/use", false, func(w http.ResponseWriter, r *http.Request, id int64) {
		o, e := s.UseProp(r.Context(), id, pathID(r))
		reply(w, o, e)
	})
	register("POST /api/blind-box/props/{id}/pause", false, func(w http.ResponseWriter, r *http.Request, id int64) {
		o, e := s.PauseProp(r.Context(), id, pathID(r))
		reply(w, o, e)
	})
	register("POST /api/blind-box/props/{id}/convert", false, func(w http.ResponseWriter, r *http.Request, id int64) {
		var in struct {
			Target string `json:"target_type"`
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Target != "topup_discount_90" {
			reply(w, nil, ErrInvalidInput)
			return
		}
		o, e := s.ConvertDiscountProp(r.Context(), id, pathID(r))
		reply(w, o, e)
	})
	register("GET /api/blind-box/admin/pools", true, func(w http.ResponseWriter, r *http.Request, _ int64) {
		o, e := s.AdminPools(r.Context())
		reply(w, o, e)
	})
	register("PUT /api/blind-box/admin/pools", true, func(w http.ResponseWriter, r *http.Request, _ int64) {
		var p Pool
		if !decode(w, r, &p) {
			return
		}
		o, e := s.SavePool(r.Context(), p)
		reply(w, o, e)
	})
	register("GET /api/blind-box/admin/users/{id}/overview", true, func(w http.ResponseWriter, r *http.Request, _ int64) {
		o, e := s.Overview(r.Context(), pathID(r))
		reply(w, o, e)
	})
}

func pathID(r *http.Request) int64 { id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64); return id }
func queryID(r *http.Request, key string) int64 {
	id, _ := strconv.ParseInt(r.URL.Query().Get(key), 10, 64)
	return id
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	if d.Decode(v) != nil || !errors.Is(d.Decode(new(any)), io.EOF) {
		reply(w, nil, ErrInvalidInput)
		return false
	}
	return true
}
func reply(w http.ResponseWriter, data any, err error) {
	status := http.StatusOK
	message := ""
	if err != nil {
		message = err.Error()
		switch {
		case errors.Is(err, ErrInvalidInput):
			status = http.StatusBadRequest
		case errors.Is(err, ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, ErrConflict), errors.Is(err, ErrInventory), errors.Is(err, billing.ErrAPICreditsPurchaseLocked), errors.Is(err, gateway.ErrInsufficientCredits):
			status = http.StatusConflict
		case errors.Is(err, ErrDailyLimit), errors.Is(err, ErrMonthlyLimit), errors.Is(err, ErrOpenLimit):
			status = http.StatusTooManyRequests
		default:
			status = http.StatusServiceUnavailable
			message = "marketplace: service unavailable"
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    any    `json:"data,omitempty"`
	}{err == nil, message, data})
}
