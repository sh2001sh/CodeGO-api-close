package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/incentives"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
	"github.com/sh2001sh/new-api/v3/internal/security"
)

// services holds everything the worker runs. build constructs them in
// dependency order; close releases them in reverse.
type services struct {
	pub           *catalog.Publisher
	store         *catalog.Store
	relay         *catalog.OutboxRelay
	sweeper       *billing.Settler
	poster        *ledger.Poster
	commerce      *commerce.Service
	rewards       *incentives.Service
	market        *marketplace.Service
	channelMarket *channelmarket.Service
	guard         *security.Guard
	posting       []*ledger.Worker
	obs           *observer
	native        *nativeJobs
	refunds       *commerce.UserRefunds
}

func (s *services) close() {
	if s.native != nil {
		s.native.close()
	}
	if s.sweeper != nil {
		s.sweeper.Close()
	}
}

// buildServices wires the worker's modules. On error it releases whatever it
// already built.
func buildServices(ctx context.Context, deps *boot.Deps, cfg workerConfig, log *slog.Logger) (s *services, err error) {
	s = &services{}
	defer func() {
		if err != nil {
			s.close()
		}
	}()
	if err = s.buildCatalog(ctx, deps, log); err != nil {
		return nil, err
	}
	accounts := ledger.NewAccounts(deps.PG.Pool)
	noSnapshot := func() *catalog.Snapshot { return nil } // the sweeper never prices
	if s.sweeper, err = billing.New(deps.Redis, noSnapshot, accounts, accounts, billing.Config{
		WALDir: os.Getenv("V3_BILLING_WAL_DIR"), DisableOutageAdmission: true,
	}, log); err != nil {
		return nil, err
	}
	s.poster = ledger.NewPoster(deps.PG.Pool, deps.Redis)
	if err = s.buildBusiness(deps, accounts, log); err != nil {
		return nil, err
	}
	if err = s.buildLedger(deps, cfg, log); err != nil {
		return nil, err
	}
	s.obs = newObserver(ledger.NewReconciler(deps.PG.Pool, deps.Redis, log), s.posting[0], log)
	registerCapacityMetrics(s.obs.reg, deps.PG.Pool, len(s.posting))
	if s.native, err = newNativeJobs(deps, s.sweeper, s.store.Current, log); err != nil {
		return nil, err
	}
	s.native.requests.Register(s.obs.reg)
	return s, nil
}

// buildCatalog publishes the first snapshot and loads it, so the worker never
// starts maintenance against an empty catalog.
func (s *services) buildCatalog(ctx context.Context, deps *boot.Deps, log *slog.Logger) error {
	s.pub = catalog.NewPublisher(deps.PG.Pool, deps.Redis, deps.Crypto, deps.Crypto, log)
	if err := s.pub.PublishNow(ctx); err != nil {
		return fmt.Errorf("initial catalog publish: %w", err)
	}
	s.store = catalog.NewStore(deps.PG.Pool, deps.Redis, deps.Crypto, log)
	if err := s.store.Load(ctx); err != nil {
		return fmt.Errorf("initial worker catalog load: %w", err)
	}
	s.relay = catalog.NewOutboxRelay(deps.PG.Pool, deps.Redis, s.pub, log)
	return nil
}

func (s *services) buildBusiness(deps *boot.Deps, accounts *ledger.Accounts, log *slog.Logger) error {
	providers, topupPrices, refundProvider, err := boot.LoadPayments(deps.PG.Pool, os.Getenv("V3_PUBLIC_URL"))
	if err != nil {
		return fmt.Errorf("worker payments: %w", err)
	}
	s.commerce = commerce.New(deps.PG.Pool, s.poster, providers, commerce.Config{
		FundingDrain:    ledger.NewDrainChecker(deps.Redis),
		ProviderPricing: topupPrices,
	})
	s.buildIncentives(deps)
	s.market = marketplace.New(deps.PG.Pool, s.poster, accounts, s.commerce, s.commerce, marketplace.Config{})
	s.commerce.SetCashBoxMarket(s.market)
	s.commerce.SetMonthlyBenefits(s.market)
	s.commerce.SetGroupCheckoutMarket(s.market)
	s.refunds = commerce.NewUserRefunds(deps.PG.Pool, s.poster, deps.Redis, refundProvider)

	batchIdentity, err := boot.NewMarketBatchIdentity(deps.PG.Pool, os.Getenv("V3_SECRET_KEY"), log)
	if err != nil {
		return err
	}
	batchRelay, err := boot.NewMarketBatchRelay(deps.PG.Pool, batchIdentity, boot.MarketBatchConfig{
		GatewayBaseURL: os.Getenv("V3_INTERNAL_GATEWAY_URL"), SigningKey: deps.Crypto.DeriveKey("market-batch"),
	})
	if err != nil {
		return err
	}
	s.channelMarket = channelmarket.New(deps.PG.Pool, deps.Crypto, s.poster, channelmarket.Config{BatchRelay: batchRelay}, log)
	if s.guard, err = security.New(deps.PG.Pool, deps.Redis, security.Config{Enabled: os.Getenv("REQUEST_ABUSE_GUARD_ENABLED") == "true"}); err != nil {
		return fmt.Errorf("worker account request protection: %w", err)
	}
	return nil
}

func (s *services) buildLedger(deps *boot.Deps, cfg workerConfig, log *slog.Logger) error {
	host, _ := os.Hostname()
	for i := 0; i < cfg.ledgerConsumers; i++ {
		worker, err := ledger.NewWorker(deps.PG.Pool, deps.Redis, ledger.WorkerConfig{
			Consumer:       fmt.Sprintf("%s-%d-%d", host, os.Getpid(), i),
			Batch:          int64(cfg.ledgerBatchSize),
			Marketplace:    s.market,
			UsageBatchHook: s.channelMarket.AccrueUsageBatchTx,
			UsageHook:      s.guard.RecordUsageTx,
		}, log)
		if err != nil {
			return err
		}
		s.posting = append(s.posting, worker)
	}
	return nil
}

// workerConfig is the environment the worker reads beyond cmd/internal/boot.
type workerConfig struct {
	reconcileEvery  time.Duration
	retentionDays   int // 0 disables audit sample cleanup
	metricsAddr     string
	ledgerConsumers int
	ledgerBatchSize int
}

func loadWorkerConfig() (workerConfig, error) {
	cfg := workerConfig{reconcileEvery: 5 * time.Minute, metricsAddr: os.Getenv("V3_WORKER_METRICS_ADDR"), ledgerConsumers: 1, ledgerBatchSize: 500}
	for _, item := range []struct {
		name    string
		target  *int
		maximum int
	}{
		{"V3_LEDGER_CONSUMERS", &cfg.ledgerConsumers, 32},
		{"V3_LEDGER_BATCH_SIZE", &cfg.ledgerBatchSize, 5000},
	} {
		if raw := os.Getenv(item.name); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > item.maximum {
				return cfg, fmt.Errorf("%s must be between 1 and %d", item.name, item.maximum)
			}
			*item.target = n
		}
	}
	if v := os.Getenv("V3_RECONCILE_EVERY"); v != "" {
		every, err := time.ParseDuration(v)
		if err != nil || every <= 0 {
			return cfg, fmt.Errorf("V3_RECONCILE_EVERY must be a positive duration, got %q", v)
		}
		cfg.reconcileEvery = every
	}
	if raw := os.Getenv("V3_AUDIT_SAMPLE_RETENTION_DAYS"); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil || days < 1 || days > 3650 {
			return cfg, fmt.Errorf("V3_AUDIT_SAMPLE_RETENTION_DAYS must be between 1 and 3650")
		}
		cfg.retentionDays = days
	}
	if cfg.metricsAddr == "" {
		cfg.metricsAddr = "127.0.0.1:9102"
	}
	return cfg, nil
}
