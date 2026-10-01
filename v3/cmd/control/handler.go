package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/api"
	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/audit"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/catalogcontrol"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/community"
	"github.com/sh2001sh/new-api/v3/internal/control"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/internal/identity/oidc"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
	"github.com/sh2001sh/new-api/v3/internal/security"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// controlStack holds the server state controlHandler assembles. Each build
// step runs in dependency order and registers its own routes on mux, so
// later steps may depend on earlier ones (e.g. channel market depends on
// the marketplace and security services).
type controlStack struct {
	deps *boot.Deps
	cfg  config
	log  *slog.Logger
	mux  *http.ServeMux

	accounts *ledger.Accounts
	poster   *ledger.Poster
	id       *identity.Control
	srv      *control.Server

	commerceService *commerce.Service
	marketService   *marketplace.Service
	securityAudit   *security.Guard
}

func controlHandler(deps *boot.Deps, cfg config, assets string, log *slog.Logger) (http.Handler, error) {
	if log == nil {
		log = slog.Default()
	}
	st := &controlStack{deps: deps, cfg: cfg, log: log}
	if err := st.buildIdentity(); err != nil {
		return nil, err
	}
	if err := st.buildCommerce(); err != nil {
		return nil, err
	}
	st.buildMarketplace()
	if err := st.buildSecurityAndChannelMarket(); err != nil {
		return nil, err
	}
	st.registerAudit()
	if err := st.buildOIDCAndCommunity(); err != nil {
		return nil, err
	}
	if assets != "" {
		st.mux.Handle("/", control.Static(assets))
	}
	return st.srv.Guard(api.DomainHandler(st.mux, notFoundHandler)), nil
}

func notFoundHandler(w http.ResponseWriter, r *http.Request, _ error) {
	if strings.HasPrefix(r.URL.Path, "/api/oidc/") {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Pragma", "no-cache")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_request"})
		return
	}
	control.Fail(w, http.StatusBadRequest, "invalid_parameters", "路径或查询参数无效")
}

// buildIdentity wires authentication, the control server and mux, and
// registers the routes that only need identity: openapi, user/token/oauth/
// passkey, admin catalog control, and billing.
func (st *controlStack) buildIdentity() error {
	deps, log := st.deps, st.log
	st.accounts, st.poster = ledger.NewAccounts(deps.PG.Pool), ledger.NewPoster(deps.PG.Pool, deps.Redis)
	st.cfg.Identity.BudgetPoster = st.poster
	var err error
	st.id, err = identity.NewControl(deps.PG.Pool, st.cfg.Identity, log)
	if err != nil {
		return err
	}
	st.srv, err = control.New(control.Config{PublicURL: st.cfg.Identity.PublicURL,
		Authenticate: func(r *http.Request) (control.Principal, error) {
			u, err := st.id.AuthenticateRequest(r)
			return control.Principal{UserID: u.ID, Admin: u.IsAdmin(), Root: u.Role == "root"}, err
		}, Ready: func(ctx context.Context) error {
			if err := deps.PG.Ping(ctx); err != nil {
				return err
			}
			return deps.Redis.Ping(ctx).Err()
		}}, log)
	if err != nil {
		return err
	}
	st.mux = st.srv.Mux()
	st.mux.HandleFunc("GET /api/openapi.json", api.Specification)
	idHandler := st.id.Handler()
	for _, path := range []string{"/api/user/", "/api/token/", "/api/oauth/", "/api/passkey/", "/api/passkey"} {
		st.mux.Handle(path, idHandler)
	}
	catalogcontrol.New(deps.PG.Pool, deps.Crypto, log).Register(st.mux, st.srv.RequireAdmin)
	st.srv.RegisterBilling(deps.PG.Pool, st.accounts, st.poster)
	return nil
}

// buildCommerce wires payment providers and wallet recovery into the
// commerce service, and registers commerce and user-refund routes.
func (st *controlStack) buildCommerce() error {
	deps, cfg := st.deps, st.cfg
	payments, pricing, refunds, err := boot.LoadPayments(deps.PG.Pool, cfg.Identity.PublicURL)
	if err != nil {
		return err
	}
	if len(payments) == 0 {
		st.log.Warn("payment checkout unavailable: payment provider credentials are not configured")
	}
	var recovery commerce.WalletRecovery
	if cfg.SMTP != nil {
		sender, err := commerce.NewSMTPWalletSender(*cfg.SMTP)
		if err != nil {
			return err
		}
		recovery, err = commerce.NewWalletRecovery(deps.PG.Pool, sender, commerce.WalletRecoveryConfig{Key: cfg.Identity.SessionSecret})
		if err != nil {
			return err
		}
	}
	st.commerceService = commerce.New(deps.PG.Pool, st.poster, payments, commerce.Config{
		ProviderPricing: pricing,
		ReturnOrigins:   []string{cfg.Identity.PublicURL},
		FundingDrain:    ledger.NewDrainChecker(deps.Redis),
		WalletRecovery:  recovery,
	})
	commerceAuth := func(r *http.Request) (commerce.Actor, error) {
		u, err := st.id.AuthenticateRequest(r)
		return commerce.Actor{UserID: u.ID, Role: u.Role}, err
	}
	st.commerceService.Register(st.mux, commerceAuth)
	commerce.NewUserRefunds(deps.PG.Pool, st.poster, deps.Redis, refunds).RegisterUserRefunds(st.mux, commerceAuth)
	return nil
}

// buildMarketplace wires the marketplace service into the already-built
// commerce service and registers group-buy and blind-box routes.
func (st *controlStack) buildMarketplace() {
	st.marketService = marketplace.New(st.deps.PG.Pool, st.poster, st.accounts, st.commerceService, st.commerceService, marketplace.Config{})
	st.commerceService.SetCashBoxMarket(st.marketService)
	st.commerceService.SetMonthlyBenefits(st.marketService)
	st.commerceService.SetGroupCheckoutMarket(st.marketService)
	marketHandler := st.marketService.Handler(func(r *http.Request) (int64, bool, error) {
		u, err := st.id.AuthenticateRequest(r)
		return u.ID, u.IsAdmin(), err
	})
	st.mux.Handle("/api/group-buy/", marketHandler)
	st.mux.Handle("/api/blind-box/", marketHandler)
}

// buildSecurityAndChannelMarket wires account-protection security auditing
// and the channel marketplace, which depends on it for shared audit routes.
func (st *controlStack) buildSecurityAndChannelMarket() error {
	deps, cfg, log := st.deps, st.cfg, st.log
	var err error
	st.securityAudit, err = security.New(deps.PG.Pool, deps.Redis, security.Config{})
	if err != nil {
		return err
	}
	securityAuth := func(r *http.Request) (security.Actor, error) {
		u, err := st.id.AuthenticateRequest(r)
		// The retained owner alias stays owner-scoped even for admin sessions.
		global := strings.HasPrefix(r.URL.Path, "/api/security-audit/") || strings.HasPrefix(r.URL.Path, "/api/marketplace/admin/security-audit/")
		return security.Actor{UserID: u.ID, Admin: u.IsAdmin() && global}, err
	}
	st.securityAudit.Register(st.mux, securityAuth)
	marketConfig := channelmarket.Config{
		SecurityAuditHandler: st.securityAudit.Handler(securityAuth),
		GiftBoxes:            st.marketService.GiftBoxes,
		WelfareTransfer: func(ctx context.Context, userID int64, external string, amount int64, password, requestID string) error {
			_, err := st.commerceService.CreateWalletTransfer(ctx, userID, commerce.WalletTransferInput{
				RecipientExternalID: external, Amount: credits.Micro(amount), PaymentPassword: password, RequestID: requestID,
			})
			return err
		},
		IssueKey: func(ctx context.Context, userID int64, group string) (channelmarket.BoundToken, error) {
			key, raw, err := st.id.CreateKey(ctx, userID, identity.KeyInput{Name: "Marketplace", Group: &group})
			if errors.Is(err, identity.ErrForbidden) {
				err = channelmarket.ErrNotFound
			}
			return channelmarket.BoundToken{TokenID: key.ID, TokenGroup: group, APIKey: raw}, err
		},
	}
	if cfg.InternalGatewayURL != "" {
		marketConfig.BatchRelay, err = boot.NewMarketBatchRelay(deps.PG.Pool, st.id, boot.MarketBatchConfig{
			GatewayBaseURL: cfg.InternalGatewayURL, SigningKey: deps.Crypto.DeriveKey("market-batch"),
		})
		if err != nil {
			return err
		}
	} else {
		log.Warn("marketplace batch tests unavailable: internal gateway URL is not configured")
	}
	channelmarket.New(deps.PG.Pool, deps.Crypto, st.poster, marketConfig, log).Register(st.mux, func(r *http.Request) (channelmarket.Actor, error) {
		u, err := st.id.AuthenticateRequest(r)
		if err == nil && r.Method == http.MethodPost && r.URL.Path == "/api/marketplace/channels" {
			// Public seller metadata needs the same persistent identity as NodeBB.
			_, err = oidc.EnsureSubject(r.Context(), deps.PG.Pool, u.ID)
		}
		return channelmarket.Actor{UserID: u.ID, Admin: u.IsAdmin()}, err
	})
	return nil
}

// registerAudit registers the request-audit log routes. Token-scoped reads
// fall back to key authentication so programmatic log access keeps working.
func (st *controlStack) registerAudit() {
	deps, id := st.deps, st.id
	audit.New(deps.PG.Pool, audit.Config{Authenticate: func(r *http.Request) (audit.Principal, error) {
		if r.URL.Path == "/api/log/token" {
			p, err := id.AuthenticateKeyRequest(r)
			if err != nil {
				return audit.Principal{}, err
			}
			return audit.Principal{UserID: p.UserID, KeyID: p.KeyID}, nil
		}
		u, err := id.AuthenticateRequest(r)
		if err != nil && auditHistoryRead(r) {
			p, keyErr := id.AuthenticateKeyRequest(r)
			if keyErr != nil {
				return audit.Principal{}, keyErr
			}
			return audit.Principal{UserID: p.UserID, KeyID: p.KeyID}, nil
		}
		return audit.Principal{UserID: u.ID, Admin: u.IsAdmin()}, err
	}}).RegisterRoutes(st.mux)
}

// buildOIDCAndCommunity wires the OIDC provider (if configured) and the
// NodeBB community bridge.
func (st *controlStack) buildOIDCAndCommunity() error {
	deps, cfg, log := st.deps, st.cfg, st.log
	oidcService, err := oidc.New(deps.PG.Pool, cfg.OIDC, func(r *http.Request) (int64, error) {
		u, err := st.id.AuthenticateRequest(r)
		return u.ID, err
	}, log)
	if err != nil && !errors.Is(err, oidc.ErrDisabled) {
		return err
	}
	if oidcService != nil {
		st.mux.Handle("/.well-known/openid-configuration", oidcService.Handler())
		st.mux.Handle("/api/oidc/", oidcService.Handler())
	}
	communityService := community.New(deps.PG.Pool, community.Config{ServiceSecret: cfg.CommunitySecret})
	communityService.RegisterRoutes(st.mux)
	communityService.RegisterSessionRoutes(st.mux, func(r *http.Request) (int64, error) {
		u, err := st.id.AuthenticateRequest(r)
		return u.ID, err
	}, func(ctx context.Context, userID int64) (string, error) {
		subject, err := oidc.EnsureSubject(ctx, deps.PG.Pool, userID)
		if errors.Is(err, oidc.ErrInactive) {
			return "", community.ErrInactive
		}
		return subject, err
	})
	if cfg.CommunitySecret == "" {
		log.Warn("NodeBB service bridge unavailable: community service secret is not configured")
	}
	return nil
}

func auditHistoryRead(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	path := r.URL.Path
	return path == "/api/audit/events" || path == "/api/audit/events/export" || path == "/api/audit/requests" ||
		(strings.HasPrefix(path, "/api/audit/requests/") && strings.HasSuffix(path, "/attempts"))
}
