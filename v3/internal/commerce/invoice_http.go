package commerce

import (
	"net/http"
	"strconv"
	"strings"
)

func (h *handler) registerInvoices(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/invoices/eligible-orders", h.authorized(false, h.invoiceEligible))
	mux.HandleFunc("GET /api/invoices/requests", h.authorized(false, h.invoiceRequests))
	mux.HandleFunc("POST /api/invoices/requests", h.authorized(false, h.invoiceCreate))
	mux.HandleFunc("GET /api/invoices/admin/requests", h.authorized(true, h.invoiceRequests))
	mux.HandleFunc("PUT /api/invoices/admin/requests/{id}", h.authorized(true, h.invoiceUpdate))
}

func (h *handler) invoiceEligible(w http.ResponseWriter, r *http.Request, a Actor) {
	result, err := h.s.ListInvoiceEligibleOrders(r.Context(), a.UserID)
	respond(w, result, err)
}

func (h *handler) invoiceRequests(w http.ResponseWriter, r *http.Request, a Actor) {
	userID := a.UserID
	if r.URL.Path == "/api/invoices/admin/requests" {
		userID = 0
	}
	page, size := 1, 20
	if value := r.URL.Query().Get("p"); value != "" {
		var err error
		page, err = strconv.Atoi(value)
		if err != nil {
			respond(w, nil, ErrInvalid)
			return
		}
	}
	if value := r.URL.Query().Get("page_size"); value != "" {
		var err error
		size, err = strconv.Atoi(value)
		if err != nil {
			respond(w, nil, ErrInvalid)
			return
		}
	}
	result, err := h.s.ListInvoiceRequests(r.Context(), userID, strings.TrimSpace(r.URL.Query().Get("status")), page, size)
	respond(w, result, err)
}

func (h *handler) invoiceCreate(w http.ResponseWriter, r *http.Request, a Actor) {
	var input CreateInvoiceRequestInput
	if err := readJSON(w, r, &input); err != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	result, err := h.s.CreateInvoiceRequest(r.Context(), a.UserID, input)
	respond(w, result, err)
}

func (h *handler) invoiceUpdate(w http.ResponseWriter, r *http.Request, a Actor) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		respond(w, nil, ErrInvalid)
		return
	}
	var input UpdateInvoiceRequestInput
	if err = readJSON(w, r, &input); err != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	result, err := h.s.UpdateAdminInvoiceRequest(r.Context(), id, a.UserID, input)
	respond(w, result, err)
}
