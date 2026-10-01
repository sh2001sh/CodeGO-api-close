package commerce

import (
	"net/http"
	"strconv"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func (h *handler) registerSubscriptionValues(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/subscription/fuel/quote", h.authorized(false, h.quoteFuel))
	mux.HandleFunc("POST /api/subscription/fuel/purchase", h.authorized(false, h.purchaseFuel))
	mux.HandleFunc("GET /api/subscription/self/claude-conversions", h.authorized(false, h.listConversions))
	mux.HandleFunc("POST /api/subscription/self/claude-conversions", h.authorized(false, h.convertSubscription))
}

type fuelBody struct {
	SubscriptionID int64         `json:"subscription_id"`
	Credits        credits.Micro `json:"credits"`
	Provider       string        `json:"provider"`
	PaymentMethod  string        `json:"payment_method"`
	SuccessURL     string        `json:"success_url"`
	CancelURL      string        `json:"cancel_url"`
}

func (h *handler) quoteFuel(w http.ResponseWriter, r *http.Request, a Actor) {
	var body fuelBody
	if err := readJSON(w, r, &body); err != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	result, err := h.s.QuoteSubscriptionFuel(r.Context(), a.UserID, body.SubscriptionID, body.Credits)
	respond(w, result, err)
}
func (h *handler) purchaseFuel(w http.ResponseWriter, r *http.Request, a Actor) {
	var body fuelBody
	if err := readJSON(w, r, &body); err != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	if body.Provider == "" {
		body.Provider = body.PaymentMethod
	}
	result, err := h.s.CreateSubscriptionFuel(r.Context(), CreateOrder{UserID: a.UserID, TargetSubscriptionID: body.SubscriptionID, FuelCredits: body.Credits, Provider: body.Provider, SuccessURL: body.SuccessURL, CancelURL: body.CancelURL})
	respond(w, result, err)
}
func (h *handler) convertSubscription(w http.ResponseWriter, r *http.Request, a Actor) {
	var body struct {
		SubscriptionID    int64  `json:"subscription_id"`
		ConversionPercent int    `json:"conversion_percent"`
		RequestID         string `json:"request_id"`
	}
	if err := readJSON(w, r, &body); err != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	if body.RequestID == "" {
		body.RequestID = r.Header.Get("Idempotency-Key")
	}
	result, err := h.s.ConvertSubscription(r.Context(), a.UserID, body.SubscriptionID, body.ConversionPercent, body.RequestID)
	respond(w, result, err)
}
func (h *handler) listConversions(w http.ResponseWriter, r *http.Request, a Actor) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	result, err := h.s.ListSubscriptionConversions(r.Context(), a.UserID, limit)
	respond(w, result, err)
}
