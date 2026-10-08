package commerce

import (
	"errors"
	"net/http"
	"strconv"
)

type RedesignConfirmation struct {
	QuoteID       string `json:"quote_id"`
	RequestID     string `json:"request_id"`
	AcceptedTerms bool   `json:"accepted_terms"`
}
type WalletConversionQuoteRequest struct {
	SubscriptionID int64 `json:"subscription_id"`
}
type ResetCardQuoteRequest struct {
	RuleID   int64 `json:"rule_id"`
	Quantity int   `json:"quantity"`
}
type CardActivationRequest struct {
	RequestID string `json:"request_id"`
}

func (h *handler) registerRedesign(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/subscription/self/wallet-conversion/quote", h.authorized(false, h.walletConversionQuote))
	mux.HandleFunc("POST /api/subscription/self/wallet-conversion/confirm", h.authorized(false, h.walletConversionConfirm))
	mux.HandleFunc("GET /api/subscription/self/wallet-conversion/{request_id}", h.authorized(false, h.walletConversionResult))
	mux.HandleFunc("GET /api/subscription/self/reset-cards/rules", h.authorized(false, h.availableCardRules))
	mux.HandleFunc("POST /api/subscription/self/reset-cards/quote", h.authorized(false, h.resetCardQuote))
	mux.HandleFunc("POST /api/subscription/self/reset-cards/confirm", h.authorized(false, h.resetCardConfirm))
	mux.HandleFunc("GET /api/subscription/self/reset-cards", h.authorized(false, h.boundCards))
	mux.HandleFunc("POST /api/subscription/self/reset-cards/{id}/activate", h.authorized(false, h.activateCard))
	mux.HandleFunc("GET /api/subscription/admin/redesign-rules", h.authorized(true, h.redesignRules))
	mux.HandleFunc("PUT /api/subscription/admin/redesign-rules", h.authorized(true, h.saveRedesignRules))
	mux.HandleFunc("POST /api/subscription/admin/redesign-preview", h.authorized(true, h.redesignPreview))
	mux.HandleFunc("GET /api/subscription/admin/wallet-conversion-review/{id}", h.authorized(true, h.walletConversionReviewEvidence))
	mux.HandleFunc("PUT /api/subscription/admin/wallet-conversion-review/{id}", h.authorized(true, h.saveWalletConversionReview))
}
func (h *handler) walletConversionReviewEvidence(w http.ResponseWriter, r *http.Request, a Actor) {
	if !rootOnly(w, a) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	out, err := h.s.WalletConversionReviewEvidence(r.Context(), id)
	respond(w, out, err)
}
func (h *handler) saveWalletConversionReview(w http.ResponseWriter, r *http.Request, a Actor) {
	if !rootOnly(w, a) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var in WalletConversionReview
	if err != nil || readJSON(w, r, &in) != nil || (in.SubscriptionID != 0 && in.SubscriptionID != id) {
		respond(w, nil, ErrInvalid)
		return
	}
	in.SubscriptionID = id
	out, err := h.s.SaveWalletConversionReview(r.Context(), a.UserID, in)
	respond(w, out, err)
}
func (h *handler) walletConversionQuote(w http.ResponseWriter, r *http.Request, a Actor) {
	var in WalletConversionQuoteRequest
	if readJSON(w, r, &in) != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	out, err := h.s.QuoteWalletConversion(r.Context(), a.UserID, in.SubscriptionID)
	respond(w, out, err)
}
func (h *handler) walletConversionConfirm(w http.ResponseWriter, r *http.Request, a Actor) {
	var in RedesignConfirmation
	if readJSON(w, r, &in) != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	if in.RequestID == "" {
		in.RequestID = r.Header.Get("Idempotency-Key")
	}
	out, err := h.s.ConfirmWalletConversion(r.Context(), a.UserID, in.QuoteID, in.RequestID, in.AcceptedTerms)
	if errors.Is(err, ErrFundingPending) && out.State == "pending" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		respond(w, out, nil)
		return
	}
	respond(w, out, err)
}
func (h *handler) walletConversionResult(w http.ResponseWriter, r *http.Request, a Actor) {
	out, err := h.s.WalletConversion(r.Context(), a.UserID, r.PathValue("request_id"))
	respond(w, out, err)
}
func (h *handler) availableCardRules(w http.ResponseWriter, r *http.Request, a Actor) {
	out, err := h.s.AvailableResetCardRules(r.Context(), a.UserID)
	respond(w, out, err)
}
func (h *handler) resetCardQuote(w http.ResponseWriter, r *http.Request, a Actor) {
	var in ResetCardQuoteRequest
	if readJSON(w, r, &in) != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	out, err := h.s.QuoteResetCards(r.Context(), a.UserID, in.RuleID, in.Quantity)
	respond(w, out, err)
}
func (h *handler) resetCardConfirm(w http.ResponseWriter, r *http.Request, a Actor) {
	var in RedesignConfirmation
	if readJSON(w, r, &in) != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	if in.RequestID == "" {
		in.RequestID = r.Header.Get("Idempotency-Key")
	}
	out, err := h.s.ConfirmResetCards(r.Context(), a.UserID, in.QuoteID, in.RequestID, in.AcceptedTerms)
	respond(w, out, err)
}
func (h *handler) boundCards(w http.ResponseWriter, r *http.Request, a Actor) {
	out, err := h.s.BoundCards(r.Context(), a.UserID)
	respond(w, out, err)
}
func (h *handler) activateCard(w http.ResponseWriter, r *http.Request, a Actor) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	var in CardActivationRequest
	if readJSON(w, r, &in) != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	if in.RequestID == "" {
		in.RequestID = r.Header.Get("Idempotency-Key")
	}
	out, err := h.s.ActivateBoundCard(r.Context(), a.UserID, id, in.RequestID)
	respond(w, out, err)
}
func rootOnly(w http.ResponseWriter, a Actor) bool {
	if a.Role != "root" {
		writeFailure(w, http.StatusForbidden, "root administrator required")
		return false
	}
	return true
}
func (h *handler) redesignRules(w http.ResponseWriter, r *http.Request, a Actor) {
	if !rootOnly(w, a) {
		return
	}
	out, err := h.s.RedesignRules(r.Context())
	respond(w, out, err)
}
func (h *handler) saveRedesignRules(w http.ResponseWriter, r *http.Request, a Actor) {
	if !rootOnly(w, a) {
		return
	}
	var in RedesignRules
	if readJSON(w, r, &in) != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	out, err := h.s.SaveRedesignRules(r.Context(), in)
	respond(w, out, err)
}
func (h *handler) redesignPreview(w http.ResponseWriter, r *http.Request, a Actor) {
	if !rootOnly(w, a) {
		return
	}
	out, err := h.s.RedesignPreview(r.Context())
	respond(w, out, err)
}
