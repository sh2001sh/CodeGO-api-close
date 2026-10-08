package commerce

import (
	"context"
	"errors"
	"math"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type CreateOrder struct {
	UserID               int64
	AmountMinor          int64
	PlanID               int64
	Provider             string
	SuccessURL           string
	CancelURL            string
	ProductID            string
	PurchaseAction       string
	TargetSubscriptionID int64
	RequestID            string
	PurchaseType         string
	GroupBuyID           int64
	FuelCredits          credits.Micro
	Selection            CheckoutSelection
}

// Create snapshots price and duration before checkout. Updating a plan later
// cannot change the credit grant of a previously issued payment order.
func (s *Service) Create(ctx context.Context, in CreateOrder) (Order, error) {
	if !in.Selection.validFor(in.Provider) || (in.PurchaseType != "" && in.PurchaseType != "normal" && in.PurchaseType != "fuel" && !isGroupPurchase(in.PurchaseType)) || (in.PurchaseType == "fuel" && (in.PurchaseAction != "" || in.GroupBuyID != 0)) || (in.GroupBuyID != 0 && in.PurchaseType != "join_group") {
		return Order{}, ErrInvalid
	}
	if in.PurchaseType == "fuel" {
		return s.CreateSubscriptionFuel(ctx, in)
	}
	if in.PurchaseAction != "" {
		return s.CreatePackageCheckout(ctx, in)
	}
	var o Order
	if in.UserID <= 0 || (in.PlanID != 0 && in.ProductID != "") {
		return o, ErrInvalid
	}
	provider := s.providers[in.Provider]
	if provider == nil {
		return o, ErrProviderUnavailable
	}
	if !s.allowedReturn(in.SuccessURL) || !s.allowedReturn(in.CancelURL) {
		return o, ErrInvalid
	}
	o = Order{Selection: in.Selection, PurchaseType: in.PurchaseType, UserID: in.UserID, Provider: in.Provider, Kind: "topup", Currency: s.cfg.Currency, AmountMinor: in.AmountMinor, ResetPeriod: "never",
		GroupBuyTarget: 3, GroupBuyLifetimeSeconds: 86400}
	if err := s.priceOrder(ctx, &o, provider, in); err != nil {
		return o, err
	}
	trade, err := tradeNumber()
	if err != nil {
		return o, err
	}
	o.TradeNo, o.CreatedAt, o.ExpiresAt = trade, s.cfg.Now(), s.cfg.Now().Add(s.cfg.OrderTTL)
	o, err = s.insertOrder(ctx, o, in.GroupBuyID)
	if err != nil {
		return o, err
	}
	checkout, err := s.checkout(ctx, o, in.SuccessURL, in.CancelURL)
	if err == nil && (checkout.Reference == "" || !validCheckoutURL(checkout.URL)) {
		err = ErrProviderUnavailable
	}
	if err != nil {
		return o, errors.Join(err, s.failCheckout(ctx, o))
	}
	_, err = s.pool.Exec(ctx, `UPDATE v3_commerce.orders SET provider_reference=$2,payment_url=$3 WHERE id=$1`, o.ID, checkout.Reference, checkout.URL)
	if err != nil {
		return o, err
	}
	return s.GetOrder(ctx, in.UserID, trade)
}

// priceOrder fills in the order's kind, amount, credits and currency from
// whichever source was requested: a subscription plan, a provider-quoted
// product, or a plain top-up priced by the provider's per-minor rate.
func (s *Service) priceOrder(ctx context.Context, o *Order, provider PaymentProvider, in CreateOrder) error {
	switch {
	case in.PlanID != 0:
		p, err := scanPlan(s.pool.QueryRow(ctx, `SELECT `+planColumns+`
		    FROM v3_commerce.plans WHERE id=$1 AND enabled AND NOT internal_only`, in.PlanID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		o.PolicyVersion, o.PlanSnapshot = p.PolicyVersion, p
		o.Kind, o.PlanID, o.AmountMinor, o.Credits, o.Currency, o.PeriodSeconds = "subscription", &p.ID, p.PriceMinor, p.Credits, p.Currency, p.PeriodSeconds
		o.GroupBuyEnabled, o.GroupBuyTarget, o.GroupBuyBonus, o.GroupBuyLifetimeSeconds = p.GroupBuyEnabled, p.GroupBuyTarget, p.GroupBuyBonus, p.GroupBuyLifetimeSeconds
		o.PeriodCredits, o.ResetPeriod, o.ResetCustomSeconds, o.LegacyPeriodic = p.PeriodCredits, p.ResetPeriod, p.ResetCustomSeconds, p.PeriodCredits == 0 && p.ResetPeriod != "never"
		o.DurationUnit, o.DurationValue, o.CustomSeconds = p.DurationUnit, p.DurationValue, p.CustomSeconds
		o.GroupBuyBonus2, o.GroupBuyBonus3, o.GroupBuyBonus5 = p.GroupBuyBonus2, p.GroupBuyBonus3, p.GroupBuyBonus5
		return nil
	case in.ProductID != "":
		productProvider, ok := provider.(ProductProvider)
		if !ok {
			return ErrInvalid
		}
		quote, err := productProvider.QuoteProduct(ctx, in.ProductID)
		if err != nil {
			return err
		}
		if quote.ID != in.ProductID || quote.AmountMinor <= 0 || quote.Credits <= 0 || !validCurrency(quote.Currency) {
			return ErrInvalid
		}
		o.ProductID, o.AmountMinor, o.Credits, o.Currency = quote.ID, quote.AmountMinor, quote.Credits, quote.Currency
		return nil
	default:
		price := s.topupPrice(in.Provider)
		if in.AmountMinor <= 0 || !validCurrency(price.Currency) || price.CreditsPerMinor <= 0 || in.AmountMinor > math.MaxInt64/int64(price.CreditsPerMinor) {
			return ErrInvalid
		}
		o.Currency = price.Currency
		o.Credits = credits.Micro(in.AmountMinor) * price.CreditsPerMinor
		return nil
	}
}

func validCheckoutURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && u.User == nil && (u.Scheme == "https" || u.Scheme == "http")
}

func (s *Service) topupPrice(provider string) TopupPrice {
	if price, ok := s.cfg.ProviderPricing[provider]; ok {
		return price
	}
	return TopupPrice{Currency: s.cfg.Currency, CreditsPerMinor: s.cfg.TopupCreditsPerMinor}
}

func (s *Service) allowedReturn(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	for _, origin := range s.cfg.ReturnOrigins {
		v, err := url.Parse(origin)
		if err == nil && u.Scheme == v.Scheme && strings.EqualFold(u.Host, v.Host) {
			return true
		}
	}
	return false
}

func (s *Service) GetOrder(ctx context.Context, userID int64, tradeNo string) (Order, error) {
	return scanOrder(s.pool.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE trade_no=$1 AND user_id=$2`, tradeNo, userID))
}

// ListOrders uses an exclusive ID cursor so simultaneous new orders do not
// repeat rows across pages. userID=0 is the administrative all-user query.
func (s *Service) ListOrders(ctx context.Context, userID, before int64, limit int) ([]Order, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders
	    WHERE ($1::bigint=0 OR user_id=$1) AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT $3`, userID, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Order, 0, limit)
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	return result, rows.Err()
}
