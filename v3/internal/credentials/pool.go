// Package credentials refreshes subscription credentials outside the gateway
// hot path. PostgreSQL owns the encrypted secrets; catalog invalidations publish
// refreshed credentials to gateways using the existing snapshot pipeline.
package credentials

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Fingerprint is stable for a credential and is persisted independently of its
// rotating tokens. Supported TLS profiles are chrome and firefox.
type Fingerprint struct {
	UserAgent  string `json:"user_agent"`
	TLSProfile string `json:"tls_profile"`
}

// Credential contains plaintext only in process memory. Never format it in logs.
type Credential struct {
	ID          int64
	Provider    string
	Secret      []byte
	ProxyURL    string
	ExpiresAt   time.Time
	UpdatedAt   time.Time
	Fingerprint Fingerprint
}

// Refresher exchanges a refresh token and returns the preserved credential
// document with fresh tokens, expiry and the same client identity.
type Refresher interface {
	Refresh(context.Context, Credential) (Credential, error)
}

// Store serializes refreshes across workers and encrypts persisted secrets.
type Store interface {
	List(context.Context) ([]Credential, error)
	Refresh(context.Context, Credential, Refresher) (Credential, error)
	RunExclusive(context.Context, func(context.Context) error) error
}

type Config struct {
	RefreshBefore   time.Duration
	ReloadEvery     time.Duration
	RequestTimeout  time.Duration
	RetryBackoff    time.Duration
	MaxBackoff      time.Duration
	CircuitFailures int
	CircuitCooldown time.Duration
	QPS             int
	ProviderQPS     map[string]int
}

func (c Config) withDefaults() Config {
	if c.RefreshBefore == 0 {
		c.RefreshBefore = 5 * time.Minute
	}
	if c.ReloadEvery == 0 {
		c.ReloadEvery = time.Minute
	}
	if c.RequestTimeout == 0 {
		c.RequestTimeout = 20 * time.Second
	}
	if c.RetryBackoff == 0 {
		c.RetryBackoff = 5 * time.Second
	}
	if c.MaxBackoff == 0 {
		c.MaxBackoff = 30 * time.Minute
	}
	if c.CircuitFailures == 0 {
		c.CircuitFailures = 3
	}
	if c.CircuitCooldown == 0 {
		c.CircuitCooldown = time.Minute
	}
	if c.QPS == 0 {
		c.QPS = 2
	}
	return c
}

type Pool struct {
	store      Store
	refreshers map[string]Refresher
	cfg        Config
	log        *slog.Logger
}

func New(store Store, refreshers map[string]Refresher, cfg Config, log *slog.Logger) (*Pool, error) {
	cfg = cfg.withDefaults()
	if store == nil || len(refreshers) == 0 {
		return nil, errors.New("credentials: store and refreshers are required")
	}
	if cfg.RefreshBefore < 0 || cfg.ReloadEvery <= 0 || cfg.RequestTimeout <= 0 ||
		cfg.RetryBackoff <= 0 || cfg.MaxBackoff < cfg.RetryBackoff || cfg.CircuitCooldown <= 0 ||
		cfg.CircuitFailures < 1 || cfg.QPS < 1 || cfg.QPS > 1000000 {
		return nil, errors.New("credentials: invalid refresh configuration")
	}
	copyRefreshers := make(map[string]Refresher, len(refreshers))
	for provider, refresher := range refreshers {
		if provider == "" || refresher == nil {
			return nil, errors.New("credentials: empty provider or refresher")
		}
		provider = canonicalProvider(provider)
		if _, duplicate := copyRefreshers[provider]; duplicate {
			return nil, errors.New("credentials: conflicting refresher provider aliases")
		}
		copyRefreshers[provider] = refresher
	}
	copyQPS := make(map[string]int, len(cfg.ProviderQPS))
	for provider, qps := range cfg.ProviderQPS {
		if qps < 1 || qps > 1000000 {
			return nil, errors.New("credentials: provider QPS out of range")
		}
		provider = canonicalProvider(provider)
		if _, duplicate := copyQPS[provider]; duplicate {
			return nil, errors.New("credentials: conflicting QPS provider aliases")
		}
		copyQPS[provider] = qps
	}
	cfg.ProviderQPS = copyQPS
	if log == nil {
		log = slog.Default()
	}
	return &Pool{store: store, refreshers: copyRefreshers, cfg: cfg, log: log}, nil
}

// Run is a singleton across PostgreSQL workers. Each provider has its own heap,
// rate limit and circuit, so one failing token endpoint cannot block another.
func (p *Pool) Run(ctx context.Context) error {
	return p.store.RunExclusive(ctx, p.run)
}

// startProviders launches one runProvider goroutine per refresher, tracked by
// wg, and returns the per-provider update channels used to feed it reloaded
// credentials.
func (p *Pool) startProviders(ctx context.Context, wg *sync.WaitGroup) map[string]chan []Credential {
	updates := make(map[string]chan []Credential, len(p.refreshers))
	for provider, refresher := range p.refreshers {
		in := make(chan []Credential, 1)
		updates[provider] = in
		qps := p.cfg.QPS
		if v := p.cfg.ProviderQPS[provider]; v != 0 {
			qps = v
		}
		wg.Add(1)
		go func() { defer wg.Done(); p.runProvider(ctx, provider, refresher, in, qps) }()
	}
	return updates
}

// publishCredentials replaces the queued credentials for provider's update
// channel with the newest view, so a slow refresh under one provider cannot
// stall reloads for the others.
func publishCredentials(ctx context.Context, in chan []Credential, creds []Credential) error {
	select {
	case in <- creds:
		return nil
	default:
	}
	select {
	case <-in:
	default:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case in <- creds:
		return nil
	}
}

// reloadCredentials lists every credential, groups it by canonical provider
// and publishes each provider's group to its update channel.
func (p *Pool) reloadCredentials(ctx context.Context, updates map[string]chan []Credential) error {
	creds, err := p.store.List(ctx)
	if err != nil {
		return err
	}
	grouped := make(map[string][]Credential)
	for _, c := range creds {
		provider := canonicalProvider(c.Provider)
		if _, ok := updates[provider]; ok {
			grouped[provider] = append(grouped[provider], c)
		}
	}
	for provider, in := range updates {
		if err := publishCredentials(ctx, in, grouped[provider]); err != nil {
			return err
		}
	}
	return nil
}

func (p *Pool) run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer func() { cancel(); wg.Wait() }()
	updates := p.startProviders(ctx, &wg)
	if err := p.reloadCredentials(ctx, updates); err != nil {
		return err
	}
	ticker := time.NewTicker(p.cfg.ReloadEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := p.reloadCredentials(ctx, updates); err != nil && ctx.Err() == nil {
				p.log.Error("credentials: reload failed", "err", err)
			}
		}
	}
}

// Match gateway and legacy migration names while sharing one endpoint budget
// between canonical IDs and their existing catalog aliases.
func canonicalProvider(provider string) string {
	switch provider {
	case "claude":
		return "anthropic"
	case "gemini-cli":
		return "gemini"
	default:
		return provider
	}
}
