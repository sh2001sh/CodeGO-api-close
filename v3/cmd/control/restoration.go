package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/adminops"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/desktop"
	"github.com/sh2001sh/new-api/v3/internal/incentives"
)

func auditHistoryRead(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	path := r.URL.Path
	return path == "/api/audit/events" || path == "/api/audit/events/export" || path == "/api/audit/requests" ||
		(strings.HasPrefix(path, "/api/audit/requests/") && strings.HasSuffix(path, "/attempts"))
}

func (st *controlStack) registerDesktop() {
	desktop.New(st.deps.PG.Pool, st.id, desktop.Config{
		PublicURL: st.cfg.Identity.PublicURL, Crypto: st.deps.Crypto,
	}).Register(st.mux)
}

func (st *controlStack) requireCatalogAdministrator(next http.Handler) http.Handler {
	admin, root := st.srv.RequireAdmin(next), st.srv.RequireRoot(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/settings" || strings.HasPrefix(r.URL.Path, "/api/settings/") || strings.HasPrefix(r.URL.Path, "/api/option/") {
			root.ServeHTTP(w, r)
			return
		}
		admin.ServeHTTP(w, r)
	})
}

func (st *controlStack) registerAdminTools() {
	adminops.New(st.deps.PG.Pool, st.deps.Crypto, st.cfg.AdminTools, st.log).Register(st.mux, func(r *http.Request) (adminops.Actor, error) {
		u, err := st.id.AuthenticateRequest(r)
		return adminops.Actor{UserID: u.ID, Role: u.Role}, err
	})
}

func (st *controlStack) registerIncentives() {
	st.rewards = incentives.New(st.deps.PG.Pool, st.poster, incentives.Config{
		Now:                 st.cfg.Identity.Now,
		ResetSubscriptionTx: st.commerceService.ResetRewardSubscriptionTx,
	})
	st.commerceService.SetBeforeOrderHook(func(ctx context.Context, tx pgx.Tx, order commerce.Order) error {
		return st.rewards.ReserveReferralTx(ctx, tx, order.ID)
	})
	st.commerceService.SetOrderReleasedHook(func(ctx context.Context, tx pgx.Tx, order commerce.Order) error {
		return st.rewards.ReleaseReferralTx(ctx, tx, order.ID)
	})
	st.commerceService.SetPaidPurchaseHook(func(ctx context.Context, tx pgx.Tx, order commerce.Order) error {
		var planID int64
		sourceType := "topup_order"
		if order.PlanID != nil {
			planID, sourceType = *order.PlanID, "subscription_order"
		}
		return st.rewards.PurchaseTx(ctx, tx, incentives.Purchase{
			UserID: order.UserID, OrderID: order.ID, PlanID: planID,
			AmountMinor: order.AmountMinor, SourceType: sourceType, SourceID: order.TradeNo,
		})
	})
	st.rewards.Register(st.mux, func(r *http.Request) (incentives.Actor, error) {
		u, err := st.id.AuthenticateRequest(r)
		return incentives.Actor{UserID: u.ID, Role: u.Role}, err
	})
}
