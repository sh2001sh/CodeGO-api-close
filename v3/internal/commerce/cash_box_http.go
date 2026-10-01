package commerce

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

func currencyNumber(amount int64, currency string) json.Number {
	return json.Number(formatCurrencyMinor(amount, currency))
}

func (h *handler) registerCashBoxes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/blind-box/amount", h.authorized(false, h.cashBoxAmount))
	mux.HandleFunc("POST /api/blind-box/pay", h.authorized(false, h.cashBoxPay))
	mux.HandleFunc("GET /api/blind-box/orders/{trade_no}", h.authorized(false, func(w http.ResponseWriter, r *http.Request, a Actor) {
		o, err := h.s.CashBoxOrder(r.Context(), a.UserID, r.PathValue("trade_no"))
		respond(w, o, err)
	}))
	mux.HandleFunc("POST /api/blind-box/orders/{trade_no}/cancel", h.authorized(false, func(w http.ResponseWriter, r *http.Request, a Actor) {
		respond(w, nil, h.s.CancelCashBox(r.Context(), a.UserID, r.PathValue("trade_no")))
	}))
	for _, name := range []string{"epay", "xunhu"} {
		provider := name
		for _, method := range []string{"GET", "POST"} {
			mux.HandleFunc(method+" /api/blind-box/"+provider+"/notify", func(w http.ResponseWriter, r *http.Request) {
				r.SetPathValue("provider", provider)
				h.webhook(w, r)
			})
			mux.HandleFunc(method+" /api/blind-box/"+provider+"/return", h.cashBoxReturn)
		}
	}
}

type cashBoxRequest struct {
	Quantity      int    `json:"quantity"`
	PoolID        int64  `json:"pool_id,omitempty"`
	PaymentMethod string `json:"payment_method,omitempty"`
	SuccessURL    string `json:"success_url,omitempty"`
	CancelURL     string `json:"cancel_url,omitempty"`
}

func (h *handler) cashBoxAmount(w http.ResponseWriter, r *http.Request, a Actor) {
	var in cashBoxRequest
	if readJSON(w, r, &in) != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	q, err := h.s.QuoteCashBox(r.Context(), a.UserID, in.PoolID, in.Quantity)
	respond(w, formatCurrencyMinor(q.AmountMinor, "cny"), err)
}

func (h *handler) cashBoxPay(w http.ResponseWriter, r *http.Request, a Actor) {
	var in cashBoxRequest
	if readJSON(w, r, &in) != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	provider := "epay"
	if in.PaymentMethod == "xunhu" {
		provider = "xunhu"
	}
	if len(h.s.cfg.ReturnOrigins) > 0 {
		origin := strings.TrimRight(h.s.cfg.ReturnOrigins[0], "/")
		if in.SuccessURL == "" {
			in.SuccessURL = origin + "/blind-box?pay=pending"
		}
		if in.CancelURL == "" {
			in.CancelURL = origin + "/blind-box?pay=fail"
		}
	}
	o, err := h.s.CreateCashBox(r.Context(), CreateCashBox{UserID: a.UserID, PoolID: in.PoolID, Quantity: in.Quantity,
		Provider: provider, PaymentMethod: in.PaymentMethod, SuccessURL: in.SuccessURL, CancelURL: in.CancelURL})
	if err != nil {
		respond(w, nil, err)
		return
	}
	data := map[string]any{"order_id": o.TradeNo, "amount_due": currencyNumber(o.AmountMinor, o.Currency), "quantity": in.Quantity}
	checkoutURL := o.PaymentURL
	if provider == "epay" {
		u, err := url.Parse(o.PaymentURL)
		if err != nil {
			respond(w, nil, ErrProviderUnavailable)
			return
		}
		form := make(map[string]string)
		for name, values := range u.Query() {
			form[name] = values[0]
		}
		data["form"] = form
		u.RawQuery = ""
		checkoutURL = u.String()
	} else {
		data["pay_url"] = o.PaymentURL
		var qr string
		if err := h.s.pool.QueryRow(r.Context(), `SELECT checkout_qrcode_url FROM v3_commerce.orders WHERE id=$1`, o.ID).Scan(&qr); err != nil {
			respond(w, nil, err)
			return
		}
		data["qrcode_url"] = qr
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "success", "data": data, "url": checkoutURL})
}

func (h *handler) cashBoxReturn(w http.ResponseWriter, r *http.Request) {
	if len(h.s.cfg.ReturnOrigins) == 0 {
		respond(w, nil, ErrProviderUnavailable)
		return
	}
	// Browser returns never fulfill inventory; only verified webhooks do.
	http.Redirect(w, r, strings.TrimRight(h.s.cfg.ReturnOrigins[0], "/")+"/blind-box?pay=pending", http.StatusSeeOther)
}
