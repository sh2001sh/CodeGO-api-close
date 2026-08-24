# Main Goal
Improve production API latency and reliability across Nginx, application timing, edge/direct baselines, and PostgreSQL, then deploy and verify the complete local change set.

# Status
- Production Nginx SSE/Upgrade, buffering, keepalive, TLS, HTTP/2, and detailed timing-log changes are live and validated.
- Cloudflare HTTP/3 is enabled. Cloudflare and localhost-origin baselines exist; EdgeOne still needs an authorized test hostname.
- Production PostgreSQL memory, WAL, autovacuum, I/O timing, pg_stat_statements, and index changes are live.
- Local code adds failed-request phase timing, transport/retry/URL fixes, group-status caching/index migration, ledger reconciliation throttling, and 72-hour published-outbox cleanup.
- Full Go tests and the frontend production build pass.

# Key Context
- Production still runs `sha-6a56770`. The two 20260819 migrations and ledger backfill are now applied, but container replacement stopped because the old N+1 verifier produced false ledger inconsistencies under concurrent writes.
- Preserve all user changes in blind-box, routing, provider, workflow, HTTP client, database, and frontend files.
- Do not commit the generated `.backend-live.out.log` change.
- Production PostgreSQL retains the existing unique indexes; duplicate GORM-created indexes have been removed and schema tags now reuse the surviving names.
- About 1.26 million published outbox rows are eligible for 72-hour cleanup. The query uses the existing `published_at` index and deletes 5,000 rows per minute.
- Converting the active 2-3 GB logs/outbox tables to native partitions requires an online shadow-table/backfill/cutover project; it is not part of this no-downtime release.
- Source ports remain restricted to Cloudflare IPs; do not expose them only for a direct public benchmark.

# Verification
- `go test ./... -count=1` passed.
- `npm run build:check` in `web/default` passed.
- Focused billing, gateway, and store tests passed after fixing ledger worker test database isolation.
- `git diff --check` passed.
- Docker daemon is unavailable locally; CI must perform the final container build.
- The migration fix now derives SQL syntax from `LogDB.Dialector.Name()` and has PostgreSQL/MySQL/SQLite regression coverage; the full Go suite passes again.
- A single-statement ledger consistency query now replaces the N+1 verifier, uses one MVCC snapshot, detects missing snapshots, and passes SQLite plus full-suite tests. Production was independently checked with the equivalent set query and has zero real inconsistencies.

# Next Actions
- Commit and push the set-based ledger verification fix, excluding `.backend-live.out.log`.
- Wait for replacement multi-architecture images and manifests, then rerun the backed-up deployment flow.
- Verify schema migrations, tables/columns/indexes, service health, timing logs, outbox cleanup, pg_stat_statements load, and disk headroom.
- Capture post-deploy Cloudflare and origin baselines and document the EdgeOne and partition-migration prerequisites.

# Blockers
- EdgeOne cannot be benchmarked truthfully until a test hostname is routed through an authorized EdgeOne property.
