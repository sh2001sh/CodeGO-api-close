// Package commerce owns payment orders and subscriptions. Every fulfillment
// changes its guarded business state and posts money in the same PG transaction.
package commerce

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

var (
	ErrNotFound            = errors.New("commerce: not found")
	ErrStateConflict       = errors.New("commerce: state conflict")
	ErrInvalid             = errors.New("commerce: invalid request")
	ErrPaymentMismatch     = errors.New("commerce: payment does not match order")
	ErrProviderUnavailable = errors.New("commerce: payment provider unavailable")
	ErrIgnoredEvent        = errors.New("commerce: unrelated payment event")
	ErrFundingPending      = errors.New("commerce: subscription funding has outstanding settlements")
)

type TransactionPoster interface {
	PostTx(context.Context, pgx.Tx, billing.Entry) (billing.PostResult, error)
}

// FundingDrain closes new admissions and waits for existing holds and usage
// events to reach the ledger before removing a subscription's remaining funds.
type FundingDrain interface {
	FreezeAndDrained(context.Context, pgx.Tx, int64) (bool, error)
}

// PaymentProvider verifies callbacks before any money is credited. Implementations
// obtain totals from the provider or signed callback, never from the browser.
type PaymentProvider interface {
	Name() string
	Checkout(context.Context, Order, string, string) (Checkout, error)
	Verify(context.Context, http.Header, []byte) (PaymentEvent, error)
}

// ProductQuote fixes a configured product's payment and credit grant on the
// server. This preserves providers whose browser request selects a SKU only.
type ProductQuote struct {
	ID          string        `json:"id"`
	AmountMinor int64         `json:"amount_minor"`
	Currency    string        `json:"currency"`
	Credits     credits.Micro `json:"credits"`
}

type ProductProvider interface {
	QuoteProduct(context.Context, string) (ProductQuote, error)
}

type Checkout struct {
	Reference string `json:"reference"`
	URL       string `json:"url"`
	QRCodeURL string `json:"qrcode_url,omitempty"`
}

type PaymentEvent struct {
	ID          string
	TradeNo     string
	Reference   string
	AmountMinor int64
	Currency    string
	Paid        bool
	Refunded    bool
	State       string `json:"state,omitempty"`
}

type Config struct {
	Now      func() time.Time
	OrderTTL time.Duration
	// TopupCreditsPerMinor fixes pricing server-side: micro-credits per payment
	// minor unit. A client may select an amount but cannot choose its conversion.
	TopupCreditsPerMinor credits.Micro
	Currency             string
	ProviderPricing      map[string]TopupPrice
	ReturnOrigins        []string
	// Set for accounts shared with live gateways. Nil is for offline imports
	// and isolated tests that cannot have outstanding gateway settlements.
	FundingDrain                   FundingDrain
	WalletRecovery                 WalletRecovery
	SubscriptionConversionDisabled bool
	CashBoxes                      CashBoxMarket
	MonthlyBenefits                MonthlyBenefits
	GroupCheckouts                 GroupCheckoutMarket
}

type TopupPrice struct {
	Currency        string        `json:"currency"`
	CreditsPerMinor credits.Micro `json:"credits_per_minor"`
}

func (c Config) withDefaults() Config {
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.OrderTTL == 0 {
		c.OrderTTL = 30 * time.Minute
	}
	if c.TopupCreditsPerMinor == 0 {
		c.TopupCreditsPerMinor = credits.Micro(credits.PerCredit / 100)
	}
	if c.Currency == "" {
		c.Currency = "usd"
	}
	return c
}

type Service struct {
	pool      *pgxpool.Pool
	poster    TransactionPoster
	providers map[string]PaymentProvider
	cfg       Config
}

func New(pool *pgxpool.Pool, poster TransactionPoster, providers []PaymentProvider, cfg Config) *Service {
	m := make(map[string]PaymentProvider, len(providers))
	for _, p := range providers {
		m[p.Name()] = p
	}
	return &Service{pool: pool, poster: poster, providers: m, cfg: cfg.withDefaults()}
}

type Order struct {
	Selection               CheckoutSelection `json:"checkout_selection,omitempty"`
	ID                      int64             `json:"id"`
	UserID                  int64             `json:"user_id"`
	PlanID                  *int64            `json:"plan_id,omitempty"`
	AmountMinor             int64             `json:"amount_minor"`
	Credits                 credits.Micro     `json:"credits"`
	PeriodSeconds           int64             `json:"period_seconds"`
	Currency                string            `json:"currency"`
	Kind                    string            `json:"kind"`
	Provider                string            `json:"provider"`
	TradeNo                 string            `json:"trade_no"`
	State                   string            `json:"state"`
	ProviderReference       *string           `json:"provider_reference,omitempty"`
	PaymentURL              string            `json:"payment_url"`
	CreatedAt               time.Time         `json:"created_at"`
	ExpiresAt               time.Time         `json:"expires_at"`
	PaidAt                  *time.Time        `json:"paid_at,omitempty"`
	GroupBuyEnabled         bool              `json:"group_buy_enabled"`
	GroupBuyTarget          int               `json:"group_buy_target"`
	GroupBuyBonus           credits.Micro     `json:"group_buy_bonus"`
	GroupBuyLifetimeSeconds int64             `json:"group_buy_lifetime_seconds"`
	ProductID               string            `json:"product_id,omitempty"`
	PeriodCredits           credits.Micro     `json:"period_credits"`
	ResetPeriod             string            `json:"reset_period"`
	ResetCustomSeconds      int64             `json:"reset_custom_seconds"`
	LegacyPeriodic          bool              `json:"legacy_periodic"`
	DurationUnit            string            `json:"duration_unit"`
	DurationValue           int               `json:"duration_value"`
	CustomSeconds           int64             `json:"custom_seconds"`
	FulfillmentState        string            `json:"fulfillment_state"`
	GroupBuyBonus2          credits.Micro     `json:"group_buy_bonus2_micro"`
	GroupBuyBonus3          credits.Micro     `json:"group_buy_bonus3_micro"`
	GroupBuyBonus5          credits.Micro     `json:"group_buy_bonus5_micro"`
	PurchaseType            string            `json:"purchase_type"`
	TargetSubscriptionID    int64             `json:"target_subscription_id"`
	FuelExpiresAt           *time.Time        `json:"fuel_expires_at,omitempty"`
}

type Plan struct {
	UpgradeGroup            string           `json:"upgrade_group,omitempty"`
	ModelLimits             map[string]int64 `json:"model_limits,omitempty"`
	MembershipTier          string           `json:"membership_tier,omitempty"`
	ID                      int64            `json:"id"`
	Name                    string           `json:"name"`
	PriceMinor              int64            `json:"price_minor"`
	Currency                string           `json:"currency"`
	Credits                 credits.Micro    `json:"credits"`
	PeriodSeconds           int64            `json:"period_seconds"`
	Enabled                 bool             `json:"enabled"`
	GroupBuyEnabled         bool             `json:"group_buy_enabled"`
	GroupBuyTarget          int              `json:"group_buy_target"`
	GroupBuyBonus           credits.Micro    `json:"group_buy_bonus"`
	GroupBuyLifetimeSeconds int64            `json:"group_buy_lifetime_seconds"`
	PeriodCredits           credits.Micro    `json:"period_credits"`
	ResetPeriod             string           `json:"reset_period"`
	ResetCustomSeconds      int64            `json:"reset_custom_seconds"`
	InternalOnly            bool             `json:"internal_only"`
	MaxPurchasePerUser      int              `json:"max_purchase_per_user"`
	DurationUnit            string           `json:"duration_unit"`
	DurationValue           int              `json:"duration_value"`
	CustomSeconds           int64            `json:"custom_seconds"`
	GroupBuyBonus2          credits.Micro    `json:"group_buy_bonus2_micro"`
	GroupBuyBonus3          credits.Micro    `json:"group_buy_bonus3_micro"`
	GroupBuyBonus5          credits.Micro    `json:"group_buy_bonus5_micro"`
	PlanType                string           `json:"plan_type"`
	FuelEnabled             bool             `json:"fuel_enabled"`
	FuelUnitPriceMicro      int64            `json:"fuel_unit_price_micro"`
	FuelMinCredits          credits.Micro    `json:"fuel_min_credits"`
	FuelCreditStep          credits.Micro    `json:"fuel_credit_step"`
}

type Subscription struct {
	ID                 int64         `json:"id"`
	UserID             int64         `json:"user_id"`
	PlanID             int64         `json:"plan_id"`
	AccountID          int64         `json:"account_id"`
	State              string        `json:"state"`
	StartsAt           time.Time     `json:"starts_at"`
	ExpiresAt          time.Time     `json:"expires_at"`
	Balance            credits.Micro `json:"balance"`
	TotalCredits       credits.Micro `json:"total_credits"`
	UsedCredits        credits.Micro `json:"used_credits"`
	PeriodCredits      credits.Micro `json:"period_credits"`
	PeriodUsed         credits.Micro `json:"period_used"`
	LegacyPeriodic     bool          `json:"legacy_periodic"`
	LastResetAt        *time.Time    `json:"last_reset_at,omitempty"`
	NextResetAt        *time.Time    `json:"next_reset_at,omitempty"`
	ResetPeriod        string        `json:"reset_period"`
	ResetCustomSeconds int64         `json:"reset_custom_seconds"`
}

func tradeNumber() (string, error) {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "v3_" + hex.EncodeToString(b[:]), nil
}

const orderColumns = `id,user_id,plan_id,amount_minor,credits,period_seconds,currency,kind,provider,trade_no,state,
provider_reference,payment_url,created_at,expires_at,paid_at,group_buy_enabled,group_buy_target,group_buy_bonus,group_buy_lifetime_seconds,product_id,
period_credits,reset_period,reset_custom_seconds,legacy_periodic,duration_unit,duration_value,custom_seconds,fulfillment_state,
group_buy_bonus2_micro,group_buy_bonus3_micro,group_buy_bonus5_micro,purchase_type,target_subscription_id,fuel_expires_at,checkout_selection`

type scanner interface{ Scan(...any) error }

func scanOrder(row scanner) (Order, error) {
	var o Order
	err := row.Scan(&o.ID, &o.UserID, &o.PlanID, &o.AmountMinor, &o.Credits, &o.PeriodSeconds, &o.Currency, &o.Kind,
		&o.Provider, &o.TradeNo, &o.State, &o.ProviderReference, &o.PaymentURL, &o.CreatedAt, &o.ExpiresAt, &o.PaidAt,
		&o.GroupBuyEnabled, &o.GroupBuyTarget, &o.GroupBuyBonus, &o.GroupBuyLifetimeSeconds, &o.ProductID,
		&o.PeriodCredits, &o.ResetPeriod, &o.ResetCustomSeconds, &o.LegacyPeriodic, &o.DurationUnit, &o.DurationValue, &o.CustomSeconds, &o.FulfillmentState,
		&o.GroupBuyBonus2, &o.GroupBuyBonus3, &o.GroupBuyBonus5, &o.PurchaseType, &o.TargetSubscriptionID, &o.FuelExpiresAt, &o.Selection)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return o, err
}
