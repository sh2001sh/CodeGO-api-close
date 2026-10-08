package main

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

func registerCapacityMetrics(reg *prometheus.Registry, pool *pgxpool.Pool, consumers int) {
	reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: "codego", Subsystem: "ledger", Name: "consumers",
		Help: "Configured ledger consumers in this worker process.",
	}, func() float64 { return float64(consumers) }))
	for _, metric := range []struct {
		name, help string
		value      func(*pgxpool.Stat) float64
	}{
		{"max_connections", "Configured maximum PostgreSQL pool connections in this worker.", func(s *pgxpool.Stat) float64 { return float64(s.MaxConns()) }},
		{"acquired_connections", "PostgreSQL connections currently in use by this worker.", func(s *pgxpool.Stat) float64 { return float64(s.AcquiredConns()) }},
		{"idle_connections", "PostgreSQL connections immediately available in this worker pool.", func(s *pgxpool.Stat) float64 { return float64(s.IdleConns()) }},
	} {
		reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: "codego", Subsystem: "pg_pool", Name: metric.name, Help: metric.help,
		}, func() float64 { return metric.value(pool.Stat()) }))
	}
	reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{
		Namespace: "codego", Subsystem: "pg_pool", Name: "empty_acquires_total",
		Help: "Pool acquisitions that waited because no PostgreSQL connection was available.",
	}, func() float64 { return float64(pool.Stat().EmptyAcquireCount()) }))
	reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{
		Namespace: "codego", Subsystem: "pg_pool", Name: "canceled_acquires_total",
		Help: "PostgreSQL pool acquisitions canceled while waiting.",
	}, func() float64 { return float64(pool.Stat().CanceledAcquireCount()) }))
}
