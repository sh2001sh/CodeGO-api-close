# CodeGo v3 — Target Architecture

This is a rebuild of the CodeGo API gateway, account control service and background workers. v3 has an independent Go module. `scripts/verify.sh boundaries` checks its real dependency graph and rejects imports from v2; nested Go modules alone do not enforce this separation.

## Structure

```
v3/
  cmd/           — Binaries: gateway, control, worker, migrate
  internal/      — Business modules
    gateway/     — Request pipeline, routing, streaming
    billing/     — Pricing, reservation, settlement
    catalog/     — Channels, models, capabilities
    identity/    — Auth, users, sessions
    commerce/    — Orders, subscriptions, payments
    marketplace/ — Group-buy, blind-box
    audit/       — Usage logs, projections
    community/   — Ratings, forum integration
    settings/    — Configuration snapshots
  pkg/           — Shared utilities
    pg/          — PostgreSQL 15+ with pgx
    redisx/      — Redis clients + Lua scripts
    httpx/       — HTTP transport helpers
    sse/         — SSE encoding
    metrics/     — Per-stage latency histograms (Prometheus)
    credits/     — Micro-credit amount type (1 credit = 1,000,000)

Logging uses the standard library `log/slog` directly.
  migrations/    — Versioned SQL (Atlas in production; embedded for tests)
  bench/         — mockupstream (terminal-state scenarios), replay recorder, k6 script
```

## Key Constraints

- **PostgreSQL 15+ only**: SQLite and MySQL are unsupported. Existing installations must migrate into PostgreSQL before using this version.
- **No v2 imports**: `depguard` enforces independence.
- **Credits language**: Native balances use `micro_credits int64`; legacy field names remain confined to offline source mapping and existing API compatibility.
- **Money writes**: Business transactions use `billing.Poster`/`ledger.Poster.PostTx`; gateways reserve and settle through Redis, and the ledger worker persists events. Opening imported balances are the offline migration exception.
- **Redis**: A persistent single-primary Redis instance with AOF and `noeviction` is required. Each gateway needs a durable, separately owned WAL volume.
- **Migration and deployment**: Isolated whole-stack testing, offline import rehearsal and reconciliation precede a whole-service switch. Existing v2 code stays available until the new installation is accepted and stable.

## Build

The main generation gateway and `/v1/models` accept optional `X-CodeGo-Group: <group>` after API key authentication. It selects one of the principal's allowed groups for that request, preserving account/private-group/pool checks and billing at the selected route. An explicit selection disables cross-group retry and never changes the stored key. Unauthorized groups return 403; empty or repeated headers return 400. Auxiliary, realtime and workflow endpoints do not support this override.

```bash
cd v3
# Offline machines: GOPROXY=off GOSUMDB=off GOFLAGS=-mod=mod (module cache only)
go build ./cmd/gateway
go build ./cmd/control
go build ./cmd/worker
go build ./cmd/migrate
```

## Test

```bash
go test ./...
go test -race ./...
V3_TEST_PG_DSN=postgres://... V3_TEST_REDIS_ADDR=... V3_TEST_COMMERCE_REDIS_ADDR=... CODEGO_TEST_REDIS_ADDR=... V3_MIGRATION_TEST_PG_DSN=postgres://... V3_MIGRATION_TEST_REDIS_ADDR=... go test -race -tags=pgintegration -p 1 -timeout=10m ./...
```

## Verify

Needs Docker; Go modules come from the host module cache.

```bash
bash scripts/verify.sh              # boundaries test lint race integration pgtest atlas bench
bash scripts/verify.sh lint race    # selected steps
```

The integration step creates private disposable PostgreSQL and Redis containers. Tests can drop their schemas; never point the test variables at a production database. The Atlas step validates the existing checksum without rewriting it. After editing migrations, generate `migrations/atlas.sum` explicitly and include it with the SQL change.

Whole-stack setup and the full-switch procedure are in [deploy/README.md](deploy/README.md). Required backend settings are `V3_PG_DSN`, `V3_REDIS_ADDR` and `V3_SECRET_KEY`; control additionally requires `V3_PUBLIC_URL` and an independent `V3_SESSION_SECRET`. Payment, OAuth and community integrations need their own provider configuration.

Backend API generation uses pinned `oapi-codegen` v2.5.0. If its dependencies are cached but the public Go proxy is unreachable, set `GOPROXY` to the local module download cache as a `file://` URL; `GOPROXY=off` can still reject the version lookup used by `go generate`.
