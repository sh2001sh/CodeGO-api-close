package commerce

import (
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type PaymentMethod struct {
	Provider        string `json:"provider"`
	Currency        string `json:"currency"`
	CreditsPerMinor int64  `json:"credits_per_minor"`
}

func (s *Service) PaymentMethods() []PaymentMethod {
	result := make([]PaymentMethod, 0, len(s.providers))
	for name := range s.providers {
		price := s.topupPrice(name)
		result = append(result, PaymentMethod{name, price.Currency, int64(price.CreditsPerMinor)})
	}
	slices.SortFunc(result, func(a, b PaymentMethod) int {
		if a.Provider < b.Provider {
			return -1
		}
		if a.Provider > b.Provider {
			return 1
		}
		return 0
	})
	return result
}

func (h *handler) registerCompatibility(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/commerce/providers", h.authorized(false, h.methods))
	mux.HandleFunc("GET /api/user/topup/info", h.authorized(false, h.methods))
	mux.HandleFunc("GET /api/user/topup/self", h.authorized(false, h.orders))
	mux.HandleFunc("GET /api/user/topup", h.authorized(true, func(w http.ResponseWriter, r *http.Request, a Actor) {
		r.URL.Path = "/api/commerce/admin/orders"
		h.orders(w, r, a)
	}))
	for _, provider := range []string{"stripe", "epay", "creem", "xunhu", "nowpayments", "waffo", "waffo_pancake"} {
		name := provider
		pathName := name
		if pathName == "waffo_pancake" {
			pathName = "waffo-pancake"
		}
		path := "/api/user/" + pathName + "/pay"
		if name == "epay" {
			path = "/api/user/pay"
		}
		mux.HandleFunc("POST "+path, h.authorized(false, func(w http.ResponseWriter, r *http.Request, a Actor) { h.legacyPay(w, r, a, name, false) }))
		mux.HandleFunc("POST /api/subscription/"+pathName+"/pay", h.authorized(false, func(w http.ResponseWriter, r *http.Request, a Actor) { h.legacyPay(w, r, a, name, true) }))
	}
	for _, path := range []string{"/api/user/epay/notify", "/api/subscription/epay/notify"} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) { r.SetPathValue("provider", "epay"); h.webhook(w, r) })
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) { r.SetPathValue("provider", "epay"); h.webhook(w, r) })
	}
	mux.HandleFunc("POST /api/creem/webhook", func(w http.ResponseWriter, r *http.Request) { r.SetPathValue("provider", "creem"); h.webhook(w, r) })
	mux.HandleFunc("POST /api/nowpayments/ipn", func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("provider", "nowpayments")
		h.webhook(w, r)
	})
	for _, name := range []string{"nowpayments", "waffo", "waffo_pancake"} {
		provider := name
		pathName := name
		if pathName == "waffo_pancake" {
			pathName = "waffo-pancake"
		}
		mux.HandleFunc("POST /api/"+pathName+"/webhook", func(w http.ResponseWriter, r *http.Request) { r.SetPathValue("provider", provider); h.webhook(w, r) })
	}
	for _, path := range []string{"/api/user/xunhu/notify", "/api/subscription/xunhu/notify"} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) { r.SetPathValue("provider", "xunhu"); h.webhook(w, r) })
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) { r.SetPathValue("provider", "xunhu"); h.webhook(w, r) })
	}
	for _, path := range []string{"/api/user/epay/return", "/api/subscription/epay/return", "/api/user/xunhu/return", "/api/subscription/xunhu/return"} {
		mux.HandleFunc("GET "+path, h.paymentReturn)
		mux.HandleFunc("POST "+path, h.paymentReturn)
	}
}

func (h *handler) methods(w http.ResponseWriter, _ *http.Request, _ Actor) {
	respond(w, h.s.PaymentMethods(), nil)
}

type legacyPayInput struct {
	Amount               int64         `json:"amount"`
	PlanID               int64         `json:"plan_id"`
	PaymentMethod        string        `json:"payment_method"`
	WalletType           string        `json:"wallet_type,omitempty"`
	SuccessURL           string        `json:"success_url,omitempty"`
	CancelURL            string        `json:"cancel_url,omitempty"`
	ProductID            string        `json:"product_id,omitempty"`
	PurchaseType         string        `json:"purchase_type,omitempty"`
	GroupBuyID           int64         `json:"group_buy_id,omitempty"`
	TargetSubscriptionID int64         `json:"target_subscription_id,omitempty"`
	RequestID            string        `json:"request_id,omitempty"`
	FuelCredits          credits.Micro `json:"fuel_credits,omitempty"`
	LegacyFuel           int64         `json:"quota,omitempty"`
	PayCurrency          string        `json:"pay_currency,omitempty"`
	PayMethodIndex       *int          `json:"pay_method_index,omitempty"`
	PayMethodType        string        `json:"pay_method_type,omitempty"`
	PayMethodName        string        `json:"pay_method_name,omitempty"`
}

func (h *handler) legacyPay(w http.ResponseWriter, r *http.Request, a Actor, provider string, subscription bool) {
	var input legacyPayInput
	if err := readJSON(w, r, &input); err != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	action, purchaseType, err := h.resolveLegacyPayAction(provider, subscription, &input)
	if err != nil {
		respond(w, nil, err)
		return
	}
	o, err := h.s.Create(r.Context(), CreateOrder{UserID: a.UserID, AmountMinor: input.Amount * paymentScale(h.s.topupPrice(provider).Currency), PlanID: input.PlanID,
		Provider: provider, SuccessURL: input.SuccessURL, CancelURL: input.CancelURL, ProductID: input.ProductID,
		PurchaseAction: action, TargetSubscriptionID: input.TargetSubscriptionID, RequestID: input.RequestID,
		PurchaseType: purchaseType, GroupBuyID: input.GroupBuyID, FuelCredits: input.FuelCredits,
		Selection: CheckoutSelection{PaymentMethod: input.PaymentMethod, PayCurrency: input.PayCurrency,
			PayMethodType: input.PayMethodType, PayMethodName: input.PayMethodName}})
	if err != nil {
		respond(w, nil, err)
		return
	}
	writeLegacyPayResponse(w, provider, o)
}

// resolveLegacyPayAction validates the legacy request shape and normalizes
// it into the (action, purchaseType) pair Create expects, mutating input
// for the fuel-credits legacy-quota conversion and return-URL defaulting.
func (h *handler) resolveLegacyPayAction(provider string, subscription bool, input *legacyPayInput) (action, purchaseType string, err error) {
	if input.PayMethodIndex != nil && *input.PayMethodIndex != 0 && input.PayMethodType == "" && input.PayMethodName == "" {
		return "", "", ErrInvalid
	}
	isFuel := input.PurchaseType == "subscription_fuel" || input.PurchaseType == "fuel"
	if subscription && input.PlanID <= 0 && !isFuel {
		return "", "", ErrInvalid
	}
	if subscription {
		if isFuel {
			purchaseType = "fuel"
			if input.LegacyFuel != 0 {
				if input.LegacyFuel <= 0 || input.LegacyFuel > math.MaxInt64/2 || input.FuelCredits != 0 {
					return "", "", ErrInvalid
				}
				input.FuelCredits = credits.Micro(input.LegacyFuel * 2)
			}
		} else if input.PurchaseType != "" && input.PurchaseType != "normal" && !isGroupPurchase(input.PurchaseType) {
			return "", "", ErrInvalid
		} else {
			action = "auto"
			purchaseType = input.PurchaseType
		}
	}
	scale := paymentScale(h.s.topupPrice(provider).Currency)
	if !subscription && input.ProductID == "" && (input.Amount <= 0 || input.Amount > math.MaxInt64/scale) {
		return "", "", ErrInvalid
	}
	if len(h.s.cfg.ReturnOrigins) > 0 {
		if input.SuccessURL == "" {
			input.SuccessURL = h.s.cfg.ReturnOrigins[0] + "/console/topup?show_history=true"
		}
		if input.CancelURL == "" {
			input.CancelURL = h.s.cfg.ReturnOrigins[0] + "/console/topup"
		}
	}
	return action, purchaseType, nil
}

// writeLegacyPayResponse renders the legacy response shape, which exposes
// the same payment URL under several historical field names and, for epay,
// also splits its query string into a standalone form payload.
func writeLegacyPayResponse(w http.ResponseWriter, provider string, o Order) {
	data := map[string]any{"pay_link": o.PaymentURL, "checkout_url": o.PaymentURL, "payment_url": o.PaymentURL,
		"pay_url": o.PaymentURL, "order_id": o.TradeNo, "amount_due": json.Number(formatCurrencyMinor(o.AmountMinor, o.Currency))}
	if o.ProviderReference != nil {
		data["payment_id"] = *o.ProviderReference
	}
	checkoutURL := o.PaymentURL
	if provider == "epay" {
		if u, err := url.Parse(o.PaymentURL); err == nil {
			form := make(map[string]string)
			for name, values := range u.Query() {
				form[name] = values[0]
			}
			data["form"] = form
			u.RawQuery = ""
			checkoutURL = u.String()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "success", "data": data, "url": checkoutURL})
}

// Browser return URLs only redirect to a configured local origin. Crediting
// remains the responsibility of the independently verified provider callback.
func (h *handler) paymentReturn(w http.ResponseWriter, r *http.Request) {
	if len(h.s.cfg.ReturnOrigins) == 0 {
		respond(w, nil, ErrProviderUnavailable)
		return
	}
	http.Redirect(w, r, strings.TrimRight(h.s.cfg.ReturnOrigins[0], "/")+"/console/topup?show_history=true", http.StatusSeeOther)
}
