package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"

	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/audit"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/credentials"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/limits"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers"
	"github.com/sh2001sh/new-api/v3/internal/gateway/routing"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/internal/security"
	"github.com/sh2001sh/new-api/v3/pkg/httpx"
	"github.com/sh2001sh/new-api/v3/pkg/metrics"
)

// prodHandler wires the real collaborators. Background loops remain available
// for in-flight settlement until closeFn joins them after HTTP shutdown.
// /readyz succeeds once auth invalidations are subscribed and a catalog
// snapshot is loaded, and fails if any required background loop exits.
//
// closeFn must run after the HTTP server has drained, because in-flight
// requests still finalize into the outage WAL during shutdown.
func prodHandler(ctx context.Context, deps *boot.Deps, log *slog.Logger) (handler http.Handler, closeFn func(), err error) {
	h, runtime, err := buildProd(ctx, deps, log)
	if err != nil {
		return nil, nil, err
	}
	return h, runtime.Close, nil
}

// prodBuild holds the collaborators buildProd assembles, in dependency
// order, so each build step can register its own runtime loops and close
// funcs as it goes.
type prodBuild struct {
	deps    *boot.Deps
	log     *slog.Logger
	runtime *prodRuntime

	trustedProxies []netip.Prefix
	store          *catalog.Store
	auth           *identity.Authorizer
	settler        *billing.Settler
	transports     *httpx.Pool
	clients        gateway.ClientProvider
	leases         *limits.Controller
	authFailures   *limits.AuthFailures
	planner        *routing.Planner
	targetPolicy   gateway.TargetPolicy
	requestGuard   gateway.RequestGuard
	registry       map[string]gateway.Provider
	native         nativeHandlers
	requests       *audit.RequestRecorder
}

func buildProd(ctx context.Context, deps *boot.Deps, log *slog.Logger) (handler http.Handler, runtime *prodRuntime, err error) {
	if log == nil {
		log = slog.Default()
	}
	runtime = newProdRuntime(ctx, log)
	defer func() {
		if err != nil {
			runtime.Close()
		}
	}()
	b := &prodBuild{deps: deps, log: log, runtime: runtime}
	if err = b.loadCatalogAndAuth(ctx); err != nil {
		return nil, runtime, err
	}
	if err = b.buildBilling(); err != nil {
		return nil, runtime, err
	}
	b.requests = audit.NewRequestRecorder(runtime.ctx, deps.PG.Pool, log)
	runtime.close = append(runtime.close, b.requests.Close)
	if err = b.buildNative(); err != nil {
		return nil, runtime, err
	}
	return b.buildGatewayMux(ctx)
}

// loadCatalogAndAuth loads the catalog snapshot and starts the loops it and
// identity invalidations depend on, failing startup if auth invalidations
// aren't subscribed within 10 s.
func (b *prodBuild) loadCatalogAndAuth(ctx context.Context) error {
	var err error
	b.trustedProxies, err = loadTrustedProxies()
	if err != nil {
		return err
	}
	b.store = catalog.NewStore(b.deps.PG.Pool, b.deps.Redis, b.deps.Crypto, b.log)
	if err := b.store.Load(ctx); err != nil {
		return fmt.Errorf("load catalog: %w", err)
	}
	b.runtime.start("catalog store", b.store.Run)

	b.auth = identity.New(b.deps.PG.Pool, b.deps.Redis, identity.Config{}, b.log)
	b.runtime.start("identity invalidations", b.auth.Run)
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-b.auth.Ready():
	case <-timer.C:
		return errors.New("identity invalidation subscription not active after 10 s")
	case <-b.runtime.ctx.Done():
		return errors.New("gateway background loop exited during startup")
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (b *prodBuild) buildBilling() error {
	accounts := ledger.NewAccounts(b.deps.PG.Pool)
	settler, err := billing.New(b.deps.Redis, b.store.Current, accounts, accounts,
		billing.Config{WALDir: walDir(), DisableOutageAdmission: true}, b.log)
	if err != nil {
		return err
	}
	b.settler = settler
	b.runtime.close = append(b.runtime.close, settler.Close)
	b.runtime.start("billing wal replay", settler.Run)
	return nil
}

// buildNative wires routing, limits, abuse guarding and the native provider
// stack that the gateway and model-discovery handlers both depend on.
func (b *prodBuild) buildNative() error {
	deps, log := b.deps, b.log
	b.transports = httpx.NewPool(httpx.TransportConfig{})
	credentialTransports := credentials.NewTransportPool(credentials.TransportConfig{})
	b.runtime.close = append(b.runtime.close, b.transports.CloseIdle, credentialTransports.CloseIdle)
	b.clients = boot.TargetClients(credentialTransports, b.transports)
	b.leases = limits.New(deps.Redis, limits.Config{})
	b.authFailures = limits.NewAuthFailures(limits.FailureConfig{})
	b.planner = routing.New(b.store.Current, routing.Config{})
	b.targetPolicy = gateway.NewSensitiveWordPolicy(func() map[string]json.RawMessage {
		if snapshot := b.store.Current(); snapshot != nil {
			return snapshot.Settings
		}
		return nil
	}, log)
	guard, err := security.New(deps.PG.Pool, deps.Redis, security.Config{Enabled: os.Getenv("REQUEST_ABUSE_GUARD_ENABLED") == "true"})
	if err != nil {
		return err
	}
	b.requestGuard = func(ctx context.Context, req *gateway.Request) error {
		return guard.Check(ctx, req.Principal.UserID, req.ID)
	}
	b.registry = providers.Registry(b.clients)
	native, err := assembleNative(deps, b.auth, b.planner, b.settler, b.leases, b.authFailures, b.registry,
		b.transports, b.clients, b.targetPolicy, b.requestGuard, b.trustedProxies, log, b.requests)
	if err != nil {
		return err
	}
	b.native = native
	b.runtime.close = append(b.runtime.close, native.close)
	return nil
}

// buildGatewayMux assembles the gateway handler, registers routes, confirms
// the catalog and background loops are still healthy, and wraps the mux so
// requests drain cleanly during shutdown.
func (b *prodBuild) buildGatewayMux(ctx context.Context) (http.Handler, *prodRuntime, error) {
	deps, log, runtime := b.deps, b.log, b.runtime
	samples, closeSamples, err := newAuditSampler(runtime.ctx, deps.PG.Pool, log)
	if err != nil {
		return nil, runtime, err
	}
	runtime.close = append(runtime.close, closeSamples)

	rec := metrics.New()
	b.requests.Register(rec.Registry())
	registerPoolMetrics(rec, deps)
	registerOutageMetrics(rec, b.settler)
	marketBatchKey := deps.Crypto.DeriveKey("market-batch")
	g, err := gateway.New(gateway.Deps{
		Config: gateway.Config{TrustedProxies: b.trustedProxies, RequestID: func(r *http.Request) string {
			return boot.MarketBatchRequestID(r, marketBatchKey)
		}},
		Authorizer:   b.auth,
		Planner:      b.native.live,
		Settler:      b.settler,
		Limits:       b.leases,
		AuthFailures: b.authFailures,
		Providers:    b.registry,
		Transports:   b.transports,
		Clients:      b.clients,
		Samples:      samples,
		Requests:     b.requests,
		TargetPolicy: b.targetPolicy,
		RequestGuard: b.requestGuard,
		Metrics:      rec,
		Logger:       log,
	})
	if err != nil {
		return nil, runtime, err
	}
	mux := http.NewServeMux()
	models, err := gateway.NewModelDiscovery(gateway.ModelDiscoveryConfig{
		Authorizer: b.auth, Planner: b.planner, Snapshot: b.store.Current,
		TrustedProxies: b.trustedProxies, AuthFailures: b.authFailures,
	})
	if err != nil {
		return nil, runtime, err
	}
	models.Register(mux)
	g.Register(mux)
	b.native.live.Register(mux)
	b.native.auxiliary.Register(mux)
	b.native.workflow.Register(mux)
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	if b.store.Current() == nil {
		return nil, runtime, errors.New("catalog snapshot unavailable during startup")
	}
	if runtime.ctx.Err() != nil {
		return nil, runtime, errors.New("gateway background loop exited during startup")
	}
	runtime.ready.Store(true)
	return runtime.Wrap(ctx, b.native.auxiliary.Wrap(b.native.live.Wrap(mux))), runtime, nil
}

// registerPoolMetrics exposes Redis and PostgreSQL pool saturation, the
// first thing to check when reserve/finalize tails grow.
func registerPoolMetrics(rec *metrics.Recorder, deps *boot.Deps) {
	gauge := func(name, help string, f func() float64) {
		rec.Registry().MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Namespace: "codego", Name: name, Help: help}, f))
	}
	counter := func(name, help string, f func() float64) {
		rec.Registry().MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Namespace: "codego", Name: name, Help: help}, f))
	}
	rs := func() *redis.PoolStats { return deps.Redis.PoolStats() }
	gauge("redis_pool_conns", "Open Redis connections.", func() float64 { return float64(rs().TotalConns) })
	gauge("redis_pool_idle_conns", "Idle Redis connections.", func() float64 { return float64(rs().IdleConns) })
	counter("redis_pool_waits_total", "Times a command waited for a free Redis connection.", func() float64 { return float64(rs().WaitCount) })
	counter("redis_pool_wait_seconds_total", "Time spent waiting for Redis connections.", func() float64 { return float64(rs().WaitDurationNs) / 1e9 })
	counter("redis_pool_timeouts_total", "Redis connection wait timeouts.", func() float64 { return float64(rs().Timeouts) })
	gauge("pg_pool_acquired_conns", "PostgreSQL connections in use.", func() float64 { return float64(deps.PG.Stat().AcquiredConns()) })
	counter("pg_pool_empty_acquire_total", "Acquires that had to wait for a PostgreSQL connection.", func() float64 {
		return float64(deps.PG.Stat().EmptyAcquireCount())
	})
}

// walDir retains settlements admitted before Redis failed until replay succeeds.
// Production never admits new requests from local outage allowances. The WAL
// requires durable, per-instance storage; "off" disables settlement WAL storage.
func walDir() string {
	switch v := os.Getenv("V3_BILLING_WAL_DIR"); v {
	case "":
		return "billing-wal"
	case "off":
		return ""
	default:
		return v
	}
}

// registerOutageMetrics exposes billing outage mode. Alert on
// codego_billing_wal_pending > 0 for longer than an expected Redis blip.
func registerOutageMetrics(rec *metrics.Recorder, s *billing.Settler) {
	stat := func(i int) func() float64 {
		return func() float64 {
			a, b, c, d, e := s.Stats()
			return float64([]int64{a, b, c, d, e}[i])
		}
	}
	reg := rec.Registry()
	reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Namespace: "codego", Subsystem: "billing", Name: "outage_reserves_total",
		Help: "Requests admitted from the local allowance while Redis was down."}, stat(0)))
	reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Namespace: "codego", Subsystem: "billing", Name: "outage_finalizes_total",
		Help: "Settlements written to the outage WAL."}, stat(1)))
	reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Namespace: "codego", Subsystem: "billing", Name: "outage_refused_total",
		Help: "Requests refused during a Redis outage (unknown account or allowance used up)."}, stat(2)))
	reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Namespace: "codego", Subsystem: "billing", Name: "wal_replayed_total",
		Help: "Outage settlements replayed into Redis."}, stat(3)))
	reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Namespace: "codego", Subsystem: "billing", Name: "wal_pending",
		Help: "Outage settlements not yet replayed into Redis."}, stat(4)))
}
