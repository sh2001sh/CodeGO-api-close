package billing

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync/atomic"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// AccountResolver maps a user to the billing account that pays for requests.
type AccountResolver interface {
	WalletAccount(ctx context.Context, userID int64) (int64, error)
}

// BalanceLoader reads an account's balance from the PostgreSQL ledger. The
// Settler installs it into Redis atomically and only if absent; the loader
// never writes Redis itself.
//
// The value must already include every charge still pending on
// redisx.StreamBillingEvents for the account. The simplest way to keep that
// true is to never lose balance hashes: run Redis with
// maxmemory-policy noeviction and persistence, so a load only happens for
// accounts that have never been in Redis.
type BalanceLoader interface {
	LedgerBalance(ctx context.Context, accountID int64) (balance credits.Micro, version int64, err error)
}

// Config tunes the settler. Zero values select defaults.
type Config struct {
	// OverdraftAllowance lets a reservation proceed while balance - held is
	// down to -allowance. Default 0: never admit beyond the balance.
	OverdraftAllowance credits.Micro
	// OverdraftCap flags settlements that end below -cap. Default 0.
	OverdraftCap credits.Micro
	// ReservationExpiry must exceed the gateway's relay timeout. Default 35 min.
	ReservationExpiry time.Duration
	// DoneTTL keeps the finalized marker, making finalize and WAL replay
	// idempotent. Outage WALs must be replayed within it. Default 24 h.
	DoneTTL  time.Duration
	Estimate pricing.EstimateConfig
	// RedisTimeout bounds each billing script call, including the wait for a
	// pool connection. It must exceed worst-case latency under load (cold
	// bursts queue for connections), or a traffic spike is mistaken for a
	// Redis outage. The breaker keeps a real outage from costing every
	// request this long. Default 2 s.
	RedisTimeout time.Duration
	// WALDir enables outage mode (see degraded.go). Empty disables it: a
	// Redis outage then refuses every request with 503.
	WALDir string
	// DisableOutageAdmission keeps WAL settlement/replay for admitted work,
	// but refuses new local spending while distributed wallet closures cannot
	// be observed. Required for live wallet refunds and exact funding attribution.
	DisableOutageAdmission bool
	// OutageFraction is the share of an account's last known balance this
	// process may spend while Redis is down. Default 0.1.
	OutageFraction float64
	Now            func() time.Time
}

func (c Config) withDefaults() Config {
	if c.ReservationExpiry <= 0 {
		c.ReservationExpiry = 35 * time.Minute
	}
	if c.DoneTTL <= 0 {
		c.DoneTTL = 24 * time.Hour
	}
	if c.RedisTimeout <= 0 {
		c.RedisTimeout = 2 * time.Second
	}
	if c.OutageFraction <= 0 {
		c.OutageFraction = 0.1
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// reservationGrace keeps the reservation hash alive past its expiry so the
// sweeper can still read the held amount.
const reservationGrace = time.Hour

// Settler implements gateway.Settler on Redis.
type Settler struct {
	cfg      Config
	rdb      *redisx.Client
	snapshot func() *catalog.Snapshot
	accounts AccountResolver
	loader   BalanceLoader
	loads    accountFlight
	log      *slog.Logger

	br    *breaker
	local *localBalances
	wal   *wal // nil: outage mode disabled
	stats OutageStats
}

// OutageStats counts outage-mode activity since start.
type OutageStats struct {
	LocalReserves  atomic.Int64 // admitted from the local allowance
	LocalFinalizes atomic.Int64 // written to the WAL
	Refused        atomic.Int64 // refused: unknown account or allowance used up
	Replayed       atomic.Int64 // WAL records applied to Redis
}

// New returns a Settler. With cfg.WALDir set it also opens the WAL; call Run
// to replay it into Redis, and Close on shutdown.
func New(rdb *redisx.Client, snapshot func() *catalog.Snapshot, accounts AccountResolver, loader BalanceLoader, cfg Config, log *slog.Logger) (*Settler, error) {
	if log == nil {
		log = slog.Default()
	}
	cfg = cfg.withDefaults()
	if cfg.OutageFraction > 1 || math.IsNaN(cfg.OutageFraction) || math.IsInf(cfg.OutageFraction, 0) {
		return nil, fmt.Errorf("billing: outage fraction must be between zero and one")
	}
	s := &Settler{cfg: cfg, rdb: rdb, snapshot: snapshot, accounts: accounts, loader: loader, log: log,
		br:    &breaker{threshold: 3, cooldown: time.Second, now: cfg.Now},
		local: newLocalBalances(cfg.OutageFraction)}
	if cfg.WALDir != "" {
		w, err := openWAL(cfg.WALDir)
		if err != nil {
			return nil, err
		}
		s.wal = w
	}
	return s, nil
}

// hold is the Settler's handle stored in gateway.Request.Reserve.
type hold struct {
	account           int64
	amount            credits.Micro
	keys              keys
	local             bool // admitted from the outage allowance; nothing is held in Redis
	price             catalog.Price
	multiplier        float64
	pricingInput      pricing.RequestInput
	funding           []fundingHold
	cardMultiplier    float64
	cardChannels      map[int64]bool
	budgetAccount     int64
	cards             []catalog.MultiplierCard
	targetPrices      map[string]targetPrice
	sourceMode        bool
	sourceLimits      map[int64]sourceLimit
	fundingPreference string
}

// Reserve holds the worst-case cost of req (Lua #1).
func (s *Settler) Reserve(ctx context.Context, req *gateway.Request) error {
	snapshot := s.snapshot()
	targetPrices, price, multiplier, err := s.freezeTargetPrices(req, snapshot)
	if err != nil {
		return err
	}
	stamp := req.Received
	if stamp.IsZero() {
		stamp = s.cfg.Now()
	}
	profile := catalog.AccountProfile{}
	if snapshot != nil {
		profile = snapshot.AccountProfiles[req.Principal.UserID]
	}
	account, err := s.resolveWalletAccount(ctx, req, profile)
	if err != nil {
		return err
	}
	cards, card, cardChannels := s.cardMultiplierFor(req, snapshot, profile, stamp)
	input := pricing.RequestInput{Body: req.Body, Headers: req.PricingHeaders, Now: stamp}
	amount, err := s.estimateTargetPrices(req, targetPrices, price, multiplier, input, cards, cardChannels)
	if err != nil {
		return fmt.Errorf("%w: estimate: %v", gateway.ErrBillingUnavailable, err)
	}
	k := keysFor(account, req.ID)
	preference, sources, sourceMode, err := s.selectFunding(ctx, req, snapshot, targetPrices, profile, stamp)
	if err != nil {
		if err == gateway.ErrInsufficientCredits {
			return err
		}
		return fmt.Errorf("%w: source policy: %v", gateway.ErrBillingUnavailable, err)
	}
	if len(sources) > 0 || req.Principal.BudgetLimited || sourceMode {
		return s.reserveFunding(ctx, req, &hold{account: account, amount: amount, keys: k, price: price, multiplier: multiplier, pricingInput: input, cardMultiplier: card, cardChannels: cardChannels, cards: cards, targetPrices: targetPrices, sourceMode: sourceMode, sourceLimits: sourceLimits(profile, req.Model), fundingPreference: preference}, sources)
	}
	return s.reserveViaRedis(ctx, req, account, amount, k, price, multiplier, input, card, cardChannels, cards, targetPrices)
}

// resolveWalletAccount returns the account that pays for req, preferring the
// cached profile value and falling back to the AccountResolver.
func (s *Settler) resolveWalletAccount(ctx context.Context, req *gateway.Request, profile catalog.AccountProfile) (int64, error) {
	account := profile.WalletAccountID
	if account > 0 {
		return account, nil
	}
	account, err := s.accounts.WalletAccount(ctx, req.Principal.UserID)
	if err != nil {
		return 0, fmt.Errorf("billing: resolve account for user %d: %w", req.Principal.UserID, err)
	}
	return account, nil
}

// cardMultiplierFor returns req's active multiplier cards and, when a card
// applies, the per-channel support map used to gate it.
func (s *Settler) cardMultiplierFor(req *gateway.Request, snapshot *catalog.Snapshot, profile catalog.AccountProfile, stamp time.Time) ([]catalog.MultiplierCard, float64, map[int64]bool) {
	cards := activeCards(profile.Cards, stamp)
	card := float64(1)
	if len(cards) > 0 {
		card = float64(cards[0].MultiplierPPM) / 1000000
	}
	var cardChannels map[int64]bool
	if card != 1 {
		cardChannels = make(map[int64]bool, len(req.Targets))
	}
	for _, target := range req.Targets {
		if card == 1 {
			break
		}
		channel := snapshot.Channels[target.ChannelID]
		supported := channel != nil && channel.MultiplierCardUserEnabled
		cardChannels[target.ChannelID] = supported
	}
	return cards, card, cardChannels
}

// reserveViaRedis performs the non-funding reservation path: an outage-mode
// local admission when the breaker is open, otherwise the Redis reserve
// script, with an outage fallback to the local allowance on failure.
func (s *Settler) reserveViaRedis(ctx context.Context, req *gateway.Request, account int64, amount credits.Micro, k keys, price catalog.Price, multiplier float64, input pricing.RequestInput, card float64, cardChannels map[int64]bool, cards []catalog.MultiplierCard, targetPrices map[string]targetPrice) error {
	fillHold := func() {
		h := req.Reserve.(*hold)
		h.price, h.multiplier, h.pricingInput, h.cardMultiplier, h.cardChannels = price, multiplier, input, card, cardChannels
		h.cards = cards
		h.targetPrices = targetPrices
	}
	if s.wal != nil && s.br.open() {
		err := s.reserveLocal(req, account, amount, k)
		if err == nil {
			fillHold()
		}
		return err
	}
	code, balance, err := s.runReserve(ctx, k, amount)
	if err == nil && code == codeNotLoaded {
		if err = s.installBalance(ctx, account, k); err == nil {
			code, balance, err = s.runReserve(ctx, k, amount)
		}
	}
	if err != nil && isOutage(ctx, err) {
		s.br.fail()
		if s.wal != nil {
			err := s.reserveLocal(req, account, amount, k)
			if err == nil {
				fillHold()
			}
			if err == nil {
				return nil
			}
		}
	}
	switch {
	case err != nil:
		return s.rejectReserve(ctx, req, &hold{account: account, keys: k}, err)
	case code == codeInsufficient:
		s.local.observe(account, credits.Micro(balance))
		return gateway.ErrInsufficientCredits
	case code != codeReserved:
		return fmt.Errorf("billing: reserve returned code %d for account %d", code, account)
	}
	s.br.ok()
	s.local.observe(account, credits.Micro(balance))
	req.Reserve = &hold{account: account, amount: amount, keys: k, price: price, multiplier: multiplier, pricingInput: input, cardMultiplier: card, cardChannels: cardChannels, cards: cards, targetPrices: targetPrices}
	return nil
}

func (s *Settler) runReserve(ctx context.Context, k keys, amount credits.Micro) (code, balance int64, err error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.RedisTimeout)
	defer cancel()
	expires := s.cfg.Now().Add(s.cfg.ReservationExpiry).UnixMilli()
	ttl := (s.cfg.ReservationExpiry + reservationGrace).Milliseconds()
	res, err := reserveScript.Run(ctx, s.rdb, []string{k.balance, k.reservation, k.done, redisx.KeyReservationOpen, k.holds},
		int64(amount), int64(s.cfg.OverdraftAllowance), expires, ttl, k.member).Int64Slice()
	if err != nil {
		return 0, 0, err
	}
	return res[0], res[2], nil
}

// requestPrice freezes pricing at reservation time. Invalid or missing prices
// refuse the request before upstream work; they never silently become free.
func (s *Settler) requestPrice(req *gateway.Request, snap *catalog.Snapshot) (catalog.Price, float64, error) {
	if snap == nil {
		return catalog.Price{}, 0, fmt.Errorf("%w: catalog not ready", gateway.ErrBillingUnavailable)
	}
	price, ok := snap.Prices[req.Model]
	if !ok {
		return catalog.Price{}, 0, fmt.Errorf("%w: no price for model %s", gateway.ErrBillingUnavailable, req.Model)
	}
	g, ok := snap.Groups[req.Principal.Group]
	if !ok {
		return catalog.Price{}, 0, fmt.Errorf("%w: unknown group %s", gateway.ErrBillingUnavailable, req.Principal.Group)
	}
	return price, g.Multiplier, nil
}
