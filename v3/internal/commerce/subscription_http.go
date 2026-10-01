package commerce

import (
	"net/http"
	"strconv"
)

func (h *handler) registerSubscriptionLifecycle(mux *http.ServeMux) {
	h.registerSubscriptionValues(mux)
	mux.HandleFunc("GET /api/subscription/self/preference", h.authorized(false, h.getPreference))
	mux.HandleFunc("PUT /api/subscription/self/preference", h.authorized(false, h.setPreference))
	mux.HandleFunc("PATCH /api/subscription/admin/plans/{id}", h.authorized(true, h.planStatus))
	mux.HandleFunc("DELETE /api/subscription/admin/plans/{id}", h.authorized(true, h.deletePlan))
	mux.HandleFunc("POST /api/subscription/admin/bind", h.authorized(true, h.bindSubscription))
	mux.HandleFunc("GET /api/subscription/admin/users/{id}/subscriptions", h.authorized(true, h.adminSubscriptions))
	mux.HandleFunc("POST /api/subscription/admin/users/{id}/subscriptions", h.authorized(true, h.bindSubscription))
	mux.HandleFunc("POST /api/subscription/admin/user_subscriptions/{id}/invalidate", h.authorized(true, h.invalidateSubscription))
	mux.HandleFunc("POST /api/subscription/admin/user_subscriptions/{id}/reset", h.authorized(true, h.resetSubscription))
	mux.HandleFunc("PUT /api/subscription/admin/user_subscriptions/{id}", h.authorized(true, h.editSubscription))
	mux.HandleFunc("DELETE /api/subscription/admin/user_subscriptions/{id}", h.authorized(true, h.deleteSubscription))
	mux.HandleFunc("POST /api/packages/purchase", h.authorized(false, h.purchasePackage))
	mux.HandleFunc("POST /api/packages/renew", h.authorized(false, h.purchasePackage))
	mux.HandleFunc("POST /api/packages/upgrade", h.authorized(false, h.purchasePackage))
	mux.HandleFunc("GET /api/commerce/admin/package-payment-reviews", h.authorized(true, h.packagePaymentReviews))
	mux.HandleFunc("POST /api/commerce/admin/package-payment-reviews/{id}/resolve", h.authorized(true, h.resolvePackagePaymentReview))
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrInvalid
	}
	return id, nil
}

func (h *handler) getPreference(w http.ResponseWriter, r *http.Request, a Actor) {
	p, err := h.s.SubscriptionPreference(r.Context(), a.UserID)
	respond(w, p, err)
}
func (h *handler) setPreference(w http.ResponseWriter, r *http.Request, a Actor) {
	var p SubscriptionPreference
	if err := readJSON(w, r, &p); err != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	result, err := h.s.SetSubscriptionPreference(r.Context(), a.UserID, p)
	respond(w, result, err)
}
func (h *handler) planStatus(w http.ResponseWriter, r *http.Request, _ Actor) {
	id, err := pathID(r)
	if err != nil {
		respond(w, nil, err)
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err = readJSON(w, r, &body); err != nil || body.Enabled == nil {
		respond(w, nil, ErrInvalid)
		return
	}
	respond(w, nil, h.s.SetPlanEnabled(r.Context(), id, *body.Enabled))
}
func (h *handler) deletePlan(w http.ResponseWriter, r *http.Request, _ Actor) {
	id, err := pathID(r)
	if err == nil {
		err = h.s.DeletePlan(r.Context(), id)
	}
	respond(w, nil, err)
}
func (h *handler) adminSubscriptions(w http.ResponseWriter, r *http.Request, _ Actor) {
	id, err := pathID(r)
	if err != nil {
		respond(w, nil, err)
		return
	}
	result, err := h.s.ListSubscriptions(r.Context(), id)
	respond(w, result, err)
}
func (h *handler) bindSubscription(w http.ResponseWriter, r *http.Request, _ Actor) {
	var body struct {
		UserID    int64  `json:"user_id"`
		PlanID    int64  `json:"plan_id"`
		RequestID string `json:"request_id"`
	}
	if err := readJSON(w, r, &body); err != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	if r.PathValue("id") != "" {
		id, err := pathID(r)
		if err != nil {
			respond(w, nil, err)
			return
		}
		body.UserID = id
	}
	if body.RequestID == "" {
		body.RequestID = r.Header.Get("Idempotency-Key")
	}
	id, err := h.s.BindSubscription(r.Context(), body.UserID, body.PlanID, body.RequestID)
	respond(w, map[string]any{"id": id}, err)
}
func (h *handler) invalidateSubscription(w http.ResponseWriter, r *http.Request, a Actor) {
	id, err := pathID(r)
	if err == nil {
		err = h.s.EndSubscription(r.Context(), id, a.UserID, false)
	}
	respond(w, nil, err)
}
func (h *handler) resetSubscription(w http.ResponseWriter, r *http.Request, a Actor) {
	id, err := pathID(r)
	if err != nil {
		respond(w, nil, err)
		return
	}
	var body struct {
		RequestID string `json:"request_id"`
	}
	if err = readJSON(w, r, &body); err != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	if body.RequestID == "" {
		body.RequestID = r.Header.Get("Idempotency-Key")
	}
	respond(w, nil, h.s.ResetSubscription(r.Context(), id, a.UserID, body.RequestID))
}
func (h *handler) purchasePackage(w http.ResponseWriter, r *http.Request, a Actor) {
	var body struct {
		PlanID               int64  `json:"plan_id"`
		Provider             string `json:"provider"`
		PaymentMethod        string `json:"payment_method"`
		SuccessURL           string `json:"success_url"`
		CancelURL            string `json:"cancel_url"`
		TargetSubscriptionID int64  `json:"target_subscription_id"`
		SubscriptionID       int64  `json:"subscription_id"`
		RequestID            string `json:"request_id"`
	}
	if err := readJSON(w, r, &body); err != nil || body.PlanID <= 0 {
		respond(w, nil, ErrInvalid)
		return
	}
	if body.Provider == "" {
		body.Provider = body.PaymentMethod
	}
	action := "auto"
	if r.URL.Path == "/api/packages/renew" {
		action = "renew"
	}
	if r.URL.Path == "/api/packages/upgrade" {
		action = "upgrade"
	}
	if body.TargetSubscriptionID == 0 {
		body.TargetSubscriptionID = body.SubscriptionID
	}
	if body.RequestID == "" {
		body.RequestID = r.Header.Get("Idempotency-Key")
	}
	result, err := h.s.Create(r.Context(), CreateOrder{UserID: a.UserID, PlanID: body.PlanID, Provider: body.Provider, SuccessURL: body.SuccessURL, CancelURL: body.CancelURL, PurchaseAction: action, TargetSubscriptionID: body.TargetSubscriptionID, RequestID: body.RequestID})
	respond(w, result, err)
}

func (h *handler) editSubscription(w http.ResponseWriter, r *http.Request, a Actor) {
	id, err := pathID(r)
	if err != nil {
		respond(w, nil, err)
		return
	}
	var body EditSubscription
	if err = readJSON(w, r, &body); err != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	if body.RequestID == "" {
		body.RequestID = r.Header.Get("Idempotency-Key")
	}
	respond(w, nil, h.s.UpdateSubscription(r.Context(), id, a.UserID, body))
}
func (h *handler) deleteSubscription(w http.ResponseWriter, r *http.Request, a Actor) {
	id, err := pathID(r)
	if err == nil {
		err = h.s.EndSubscription(r.Context(), id, a.UserID, true)
	}
	respond(w, nil, err)
}
