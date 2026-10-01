package commerce

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func (h *handler) registerWallet(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/wallet/transfers", h.authorized(false, h.walletOverview))
	mux.HandleFunc("GET /api/wallet/transfers/recipients/{external_id}", h.authorized(false, h.walletRecipient))
	mux.HandleFunc("POST /api/wallet/transfers", h.authorized(false, h.walletTransfer))
	mux.HandleFunc("PUT /api/wallet/transfers/payment-password", h.authorized(false, h.walletPassword))
	mux.HandleFunc("POST /api/wallet/transfers/payment-password/email-code", h.authorized(false, h.walletEmailCode))
}

func (h *handler) walletOverview(w http.ResponseWriter, r *http.Request, a Actor) {
	page, _ := strconv.Atoi(r.URL.Query().Get("p"))
	if page == 0 {
		page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if size == 0 {
		size = 10
	}
	out, err := h.s.WalletOverview(r.Context(), a.UserID, page, size)
	respondWallet(w, out, err)
}

func (h *handler) walletRecipient(w http.ResponseWriter, r *http.Request, a Actor) {
	out, err := h.s.WalletRecipient(r.Context(), a.UserID, r.PathValue("external_id"))
	respondWallet(w, out, err)
}

func (h *handler) walletTransfer(w http.ResponseWriter, r *http.Request, a Actor) {
	var in WalletTransferInput
	if err := readJSON(w, r, &in); err != nil {
		respondWallet(w, nil, ErrInvalid)
		return
	}
	out, err := h.s.CreateWalletTransfer(r.Context(), a.UserID, in)
	respondWallet(w, out, err)
}

func (h *handler) walletPassword(w http.ResponseWriter, r *http.Request, a Actor) {
	var in WalletPasswordInput
	if err := readJSON(w, r, &in); err != nil {
		respondWallet(w, nil, ErrInvalid)
		return
	}
	err := h.s.ConfigureWalletPassword(r.Context(), a.UserID, in)
	respondWallet(w, struct {
		PasswordSet bool `json:"password_set"`
	}{true}, err)
}

func (h *handler) walletEmailCode(w http.ResponseWriter, r *http.Request, a Actor) {
	if h.s == nil || h.s.cfg.WalletRecovery == nil {
		respondWallet(w, nil, ErrWalletEmailUnavailable)
		return
	}
	email, err := h.s.cfg.WalletRecovery.Send(r.Context(), a.UserID)
	respondWallet(w, struct {
		EmailMasked string `json:"email_masked"`
	}{email}, err)
}

func respondWallet(w http.ResponseWriter, value any, err error) {
	if errors.Is(err, gateway.ErrBillingUnavailable) {
		writeFailure(w, http.StatusServiceUnavailable, "wallet balance temporarily unavailable")
		return
	}
	if errors.Is(err, ErrWalletPasswordLocked) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
			Code    string `json:"code"`
		}{false, err.Error(), "PAYMENT_PASSWORD_LOCKED"})
		return
	}
	if errors.Is(err, ErrWalletEmailUnavailable) {
		writeFailure(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if errors.Is(err, ErrWalletEmailDelivery) {
		writeFailure(w, http.StatusServiceUnavailable, ErrWalletEmailDelivery.Error())
		return
	}
	if errors.Is(err, ErrWalletEmailCodeLocked) || errors.Is(err, ErrWalletEmailRateLimited) {
		writeFailure(w, http.StatusTooManyRequests, err.Error())
		return
	}
	if errors.Is(err, ErrWalletEmailRequired) || errors.Is(err, ErrWalletEmailUnverified) || errors.Is(err, ErrWalletEmailCodeInvalid) {
		writeFailure(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, ErrWalletPasswordWrong) || errors.Is(err, ErrWalletPasswordNotSet) || errors.Is(err, ErrWalletAccountPassword) || errors.Is(err, ErrWalletSelf) || errors.Is(err, ErrWalletInsufficient) || errors.Is(err, ErrWalletRewardLocked) {
		writeFailure(w, http.StatusBadRequest, err.Error())
		return
	}
	respond(w, value, err)
}
