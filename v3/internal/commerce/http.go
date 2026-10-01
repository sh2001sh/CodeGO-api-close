package commerce

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type Actor struct {
	UserID int64
	Role   string
}
type Authenticate func(*http.Request) (Actor, error)

// Register preserves the subscription routes and adds an explicit order API.
// Authentication is injected by the control composition root, never by headers
// claiming a user ID. Webhooks are anonymous but independently signed.
func (s *Service) Register(mux *http.ServeMux, authenticate Authenticate) {
	h := &handler{s: s, authenticate: authenticate}
	h.registerCompatibility(mux)
	h.registerRedemptions(mux)
	h.registerWallet(mux)
	h.registerInvoices(mux)
	h.registerSubscriptionLifecycle(mux)
	h.registerCashBoxes(mux)
	mux.HandleFunc("GET /api/commerce/orders", h.authorized(false, h.orders))
	mux.HandleFunc("POST /api/commerce/orders", h.authorized(false, h.create))
	mux.HandleFunc("GET /api/commerce/orders/{trade_no}", h.authorized(false, h.order))
	mux.HandleFunc("GET /api/commerce/orders/{trade_no}/discount", h.authorized(false, h.orderDiscount))
	mux.HandleFunc("POST /api/commerce/orders/{trade_no}/cancel", h.authorized(false, h.cancel))
	mux.HandleFunc("GET /api/commerce/admin/orders", h.authorized(true, h.orders))
	mux.HandleFunc("GET /api/subscription/plans", h.authorized(false, h.plans))
	mux.HandleFunc("GET /api/subscription/self", h.authorized(false, h.subscriptions))
	mux.HandleFunc("GET /api/packages/public", h.authorized(false, h.plans))
	mux.HandleFunc("GET /api/packages/my-subscription", h.authorized(false, h.subscriptions))
	mux.HandleFunc("GET /api/subscription/orders/{trade_no}", h.authorized(false, h.order))
	mux.HandleFunc("POST /api/subscription/orders/{trade_no}/cancel", h.authorized(false, h.cancel))
	mux.HandleFunc("GET /api/subscription/admin/plans", h.authorized(true, h.plans))
	mux.HandleFunc("POST /api/subscription/admin/plans", h.authorized(true, h.savePlan))
	mux.HandleFunc("PUT /api/subscription/admin/plans/{id}", h.authorized(true, h.savePlan))
	mux.HandleFunc("POST /api/commerce/webhooks/{provider}", h.webhook)
	mux.HandleFunc("POST /api/stripe/webhook", func(w http.ResponseWriter, r *http.Request) { r.SetPathValue("provider", "stripe"); h.webhook(w, r) })
}

type handler struct {
	s            *Service
	authenticate Authenticate
}
type actorHandler func(http.ResponseWriter, *http.Request, Actor)

func (h *handler) authorized(admin bool, next actorHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.authenticate == nil {
			writeFailure(w, http.StatusUnauthorized, "authentication required")
			return
		}
		actor, err := h.authenticate(r)
		if err != nil || actor.UserID <= 0 {
			writeFailure(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if admin && actor.Role != "admin" && actor.Role != "root" {
			writeFailure(w, http.StatusForbidden, "administrator required")
			return
		}
		next(w, r, actor)
	}
}

func (h *handler) orders(w http.ResponseWriter, r *http.Request, a Actor) {
	userID := a.UserID
	if r.URL.Path == "/api/commerce/admin/orders" {
		userID = 0
	}
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	result, err := h.s.ListOrders(r.Context(), userID, before, limit)
	respond(w, result, err)
}

func (h *handler) order(w http.ResponseWriter, r *http.Request, a Actor) {
	result, err := h.s.GetOrder(r.Context(), a.UserID, r.PathValue("trade_no"))
	respond(w, result, err)
}

func (h *handler) orderDiscount(w http.ResponseWriter, r *http.Request, a Actor) {
	result, err := h.s.GetCheckoutDiscount(r.Context(), a.UserID, r.PathValue("trade_no"))
	respond(w, result, err)
}

func (h *handler) create(w http.ResponseWriter, r *http.Request, a Actor) {
	var body struct {
		AmountMinor          int64             `json:"amount_minor"`
		PlanID               int64             `json:"plan_id"`
		Provider             string            `json:"provider"`
		SuccessURL           string            `json:"success_url"`
		CancelURL            string            `json:"cancel_url"`
		ProductID            string            `json:"product_id,omitempty"`
		PurchaseAction       string            `json:"purchase_action,omitempty"`
		TargetSubscriptionID int64             `json:"target_subscription_id,omitempty"`
		RequestID            string            `json:"request_id,omitempty"`
		PurchaseType         string            `json:"purchase_type,omitempty"`
		GroupBuyID           int64             `json:"group_buy_id,omitempty"`
		FuelCredits          credits.Micro     `json:"fuel_credits,omitempty"`
		Selection            CheckoutSelection `json:"checkout_selection,omitempty"`
	}
	if err := readJSON(w, r, &body); err != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	result, err := h.s.Create(r.Context(), CreateOrder{UserID: a.UserID, AmountMinor: body.AmountMinor, PlanID: body.PlanID,
		Provider: body.Provider, SuccessURL: body.SuccessURL, CancelURL: body.CancelURL, ProductID: body.ProductID,
		PurchaseAction: body.PurchaseAction, TargetSubscriptionID: body.TargetSubscriptionID, RequestID: body.RequestID,
		PurchaseType: body.PurchaseType, GroupBuyID: body.GroupBuyID, FuelCredits: body.FuelCredits, Selection: body.Selection})
	respond(w, result, err)
}

func (h *handler) cancel(w http.ResponseWriter, r *http.Request, a Actor) {
	respond(w, nil, h.s.Cancel(r.Context(), a.UserID, r.PathValue("trade_no")))
}

func (h *handler) plans(w http.ResponseWriter, r *http.Request, _ Actor) {
	result, err := h.s.ListPlans(r.Context(), r.URL.Path == "/api/subscription/admin/plans")
	respond(w, result, err)
}

func (h *handler) savePlan(w http.ResponseWriter, r *http.Request, _ Actor) {
	var p Plan
	if err := readJSON(w, r, &p); err != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	if value := r.PathValue("id"); value != "" {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			respond(w, nil, ErrInvalid)
			return
		}
		p.ID = id
	} else {
		p.ID = 0
	}
	result, err := h.s.SavePlan(r.Context(), p)
	respond(w, result, err)
}

func (h *handler) subscriptions(w http.ResponseWriter, r *http.Request, a Actor) {
	result, err := h.s.ListSubscriptions(r.Context(), a.UserID)
	respond(w, result, err)
}

func (h *handler) webhook(w http.ResponseWriter, r *http.Request) {
	defer func() { _ = r.Body.Close() }()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid callback")
		return
	}
	if r.Method == http.MethodGet && (r.PathValue("provider") == "epay" || r.PathValue("provider") == "xunhu") {
		body = []byte(r.URL.RawQuery)
	}
	err = h.s.HandleWebhook(r.Context(), r.PathValue("provider"), r.Header, body)
	if responder, ok := h.s.providers[r.PathValue("provider")].(interface {
		WebhookResponse(bool) (http.Header, []byte, error)
	}); ok && err == nil {
		header, reply, replyErr := responder.WebhookResponse(true)
		if replyErr != nil {
			respond(w, nil, replyErr)
			return
		}
		for key, values := range header {
			w.Header()[key] = values
		}
		_, _ = w.Write(reply)
		return
	}
	if err == nil && (r.PathValue("provider") == "epay" || r.PathValue("provider") == "xunhu") {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("success"))
		return
	}
	respond(w, nil, err)
}

func readJSON(w http.ResponseWriter, r *http.Request, target any) error {
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return ErrInvalid
	}
	return nil
}

func respond(w http.ResponseWriter, value any, err error) {
	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Success bool `json:"success"`
			Data    any  `json:"data"`
		}{true, value})
		return
	}
	code, message := http.StatusInternalServerError, "commerce operation failed"
	switch {
	case errors.Is(err, ErrNotFound):
		code, message = http.StatusNotFound, "not found"
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrPaymentMismatch):
		code, message = http.StatusBadRequest, "invalid request or payment"
	case errors.Is(err, ErrStateConflict):
		code, message = http.StatusConflict, "order state conflict"
	case errors.Is(err, ErrProviderUnavailable):
		code, message = http.StatusServiceUnavailable, "payment provider unavailable"
	case errors.Is(err, ErrFundingPending):
		code, message = http.StatusServiceUnavailable, "subscription settlement pending; retry later"
	}
	writeFailure(w, code, message)
}

func writeFailure(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{false, message})
}
