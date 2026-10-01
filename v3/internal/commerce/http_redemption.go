package commerce

import (
	"net/http"
	"strconv"
)

func (h *handler) registerRedemptions(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/user/topup", h.authorized(false, h.redeem))
	mux.HandleFunc("POST /api/commerce/redemptions/redeem", h.authorized(false, h.redeem))
	mux.HandleFunc("GET /api/redemption/", h.authorized(true, h.redemptions))
	mux.HandleFunc("POST /api/redemption/", h.authorized(true, h.issueRedemption))
	mux.HandleFunc("DELETE /api/redemption/{id}", h.authorized(true, h.revokeRedemption))
}

func (h *handler) redeem(w http.ResponseWriter, r *http.Request, a Actor) {
	var input struct {
		Key string `json:"key"`
	}
	if err := readJSON(w, r, &input); err != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	result, err := h.s.RedeemTyped(r.Context(), a.UserID, input.Key)
	respond(w, result, err)
}

func (h *handler) redemptions(w http.ResponseWriter, r *http.Request, _ Actor) {
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	result, err := h.s.ListRedemptions(r.Context(), before, limit)
	respond(w, result, err)
}

func (h *handler) issueRedemption(w http.ResponseWriter, r *http.Request, _ Actor) {
	var input IssueRedemptionInput
	if err := readJSON(w, r, &input); err != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	result, err := h.s.IssueTypedRedemption(r.Context(), input)
	respond(w, result, err)
}

func (h *handler) revokeRedemption(w http.ResponseWriter, r *http.Request, _ Actor) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		respond(w, nil, ErrInvalid)
		return
	}
	respond(w, nil, h.s.RevokeRedemption(r.Context(), id))
}
