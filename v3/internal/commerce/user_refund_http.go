package commerce

import (
	"errors"
	"net/http"
)

// RegisterUserRefunds uses the control session authenticator. Order ownership
// is checked again by every service operation, including remote synchronization.
func (s *UserRefunds) RegisterUserRefunds(mux *http.ServeMux, authenticate Authenticate) {
	h := &handler{authenticate: authenticate}
	mux.HandleFunc("GET /api/wallet/refunds/eligible", h.authorized(false, func(w http.ResponseWriter, r *http.Request, a Actor) {
		items, err := s.Eligible(r.Context(), a.UserID)
		respondUserRefund(w, map[string]any{"items": items}, err)
	}))
	mux.HandleFunc("POST /api/wallet/refunds", h.authorized(false, func(w http.ResponseWriter, r *http.Request, a Actor) {
		var in UserRefundRequest
		if err := readJSON(w, r, &in); err != nil {
			respondUserRefund(w, nil, ErrInvalid)
			return
		}
		result, err := s.Create(r.Context(), a.UserID, in)
		respondUserRefund(w, result, err)
	}))
	mux.HandleFunc("POST /api/wallet/refunds/{refund_no}/sync", h.authorized(false, func(w http.ResponseWriter, r *http.Request, a Actor) {
		result, err := s.Sync(r.Context(), a.UserID, r.PathValue("refund_no"))
		respondUserRefund(w, result, err)
	}))
}

func respondUserRefund(w http.ResponseWriter, value any, err error) {
	if errors.Is(err, ErrRefundUnavailable) {
		writeFailure(w, http.StatusConflict, "订单没有可退款的未使用额度")
		return
	}
	respond(w, value, err)
}
