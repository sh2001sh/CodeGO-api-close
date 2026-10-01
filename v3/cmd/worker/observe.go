package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
)

// observer runs reconciliation on a schedule and exposes billing health:
// alert on codego_ledger_reconcile_drifted > 0, codego_ledger_reconcile_behind
// > 0, or a growing codego_ledger_backlog_entries.
type observer struct {
	rec    *ledger.Reconciler
	worker *ledger.Worker
	log    *slog.Logger
	reg    *prometheus.Registry

	mu   sync.Mutex
	last ledger.ReconcileResult
	at   time.Time
}

func newObserver(rec *ledger.Reconciler, worker *ledger.Worker, log *slog.Logger) *observer {
	o := &observer{rec: rec, worker: worker, log: log, reg: prometheus.NewRegistry()}
	o.reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	result := func(name, help string, f func(ledger.ReconcileResult) int) {
		o.reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Namespace: "codego", Subsystem: "ledger", Name: name, Help: help},
			func() float64 { o.mu.Lock(); defer o.mu.Unlock(); return float64(f(o.last)) }))
	}
	result("reconcile_checked", "Accounts compared exactly in the last pass.", func(r ledger.ReconcileResult) int { return r.Checked })
	result("reconcile_in_flight", "Accounts skipped for in-flight charges in the last pass.", func(r ledger.ReconcileResult) int { return r.InFlight })
	result("reconcile_missing", "Accounts with no Redis balance in the last pass.", func(r ledger.ReconcileResult) int { return r.Missing })
	result("reconcile_drifted", "Accounts whose Redis balance disagreed with the ledger at equal version.", func(r ledger.ReconcileResult) int { return r.Drifted })
	result("reconcile_behind", "Accounts where Redis missed ledger charges.", func(r ledger.ReconcileResult) int { return r.Behind })
	result("reconcile_stuck", "Behind accounts that could not be repaired automatically.", func(r ledger.ReconcileResult) int { return r.Stuck })
	o.reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Namespace: "codego", Subsystem: "ledger", Name: "reconcile_age_seconds",
		Help: "Seconds since the last completed reconcile pass."}, func() float64 {
		o.mu.Lock()
		defer o.mu.Unlock()
		if o.at.IsZero() {
			return -1
		}
		return time.Since(o.at).Seconds()
	}))
	o.reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Namespace: "codego", Subsystem: "ledger", Name: "backlog_entries",
		Help: "Billing events not yet posted to the ledger (pending + undelivered)."}, func() float64 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		pending, lag, err := o.worker.Backlog(ctx)
		if err != nil {
			return -1
		}
		return float64(pending + lag)
	}))
	return o
}

func (o *observer) reconcile(ctx context.Context) error {
	res, err := o.rec.Run(ctx)
	if err == nil {
		_, err = o.rec.RunReserved(ctx)
	}
	switch {
	case err != nil && ctx.Err() == nil:
		o.log.Error("ledger: reconcile pass failed", "err", err)
	case err == nil:
		o.mu.Lock()
		o.last, o.at = res, time.Now()
		o.mu.Unlock()
		level := slog.LevelInfo
		if res.NeedsAttention() {
			level = slog.LevelError
		}
		o.log.Log(ctx, level, "ledger: reconcile pass", "checked", res.Checked, "in_flight", res.InFlight,
			"missing", res.Missing, "drifted", res.Drifted, "behind", res.Behind, "repaired", res.Repaired, "stuck", res.Stuck)
	}
	return err
}

func (o *observer) serve(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(o.reg, promhttp.HandlerOpts{}))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
