# Offline v2 import

Set `V3_SOURCE_PG_DSN` to the v2 PostgreSQL database with a read-only login and
`V3_PG_DSN` to the separate v3 database. Set `V3_SECRET_KEY` to the target's base64
AES-256 encryption key. Source channel-market credentials additionally require
`V3_MIGRATION_SOURCE_CRYPTO_SECRET`, the old encryption secret; retained background
results use the same source secret. Reports expose
IDs, counts and amounts, never passwords, API keys or provider credentials.

`migrate schema` applies embedded versioned SQL to a new target database and
records checksums. Repeating it is safe; modifying an applied migration fails.
An existing Atlas-managed schema must continue using Atlas.

`migrate import` previews all mapped data. Stop every v2 gateway, control and
background writer and drain pending billing/provider work, then run
`migrate import -apply -offline`. The flag asserts the operator stopped writers;
it does not stop them. The source is a READ ONLY REPEATABLE READ snapshot, with
no write locks or changes. All target writes share one SERIALIZABLE transaction
and migration advisory lock. Pending checkout discounts, monthly benefits and
group intent are initialized in that same transaction after source cards and
orders exist. Failure or interruption leaves no partial database import.

For existing uploaded files, first run `migrate files` with
`V3_SOURCE_FILE_STORAGE_DIR` (the read-only v2 file volume) and
`V3_FILES_DIR` (the separate v3 volume used by gateway and worker), then
`migrate files -apply -offline`. This copies exact valid local IDs, owners,
times, hashes and bytes through the native file store. Every source size/checksum
is verified before any copy; each file is published atomically. An interrupted
asset step may retain completed files and is safely resumable. Database import
and check require matching target assets. Expired files are explicitly reported
as `excluded_expired`; upstream credential mappings are reported as
`rebuilt_lazily` and re-uploaded when used. Source assets are never changed.

For completed, failed or cancelled background responses, first run
`migrate background`, then `migrate background -apply -offline`. The asset step
requires the source DSN and encryption secret plus the target's `V3_REDIS_ADDR`,
optional `V3_REDIS_PASSWORD` and `V3_SECRET_KEY`. It uses the same Redis database,
namespace and derived encryption key as gateway and worker. Original response IDs,
owners, result JSON, ordered events and dates remain usable through the native
background API. Request/authentication secrets are omitted; imported terminal
results never dispatch upstream or create another debit. Source history has no
expiry, so the copied terminal history remains persistent. Every source record
is validated before writes, each job and its events publish atomically, and
unchanged retries are safe. Main database import and check refuse missing,
changed or corrupt copied results. Pending or unsettled jobs still block migration.

The importer retains native typed identities, keys, provider configuration,
pricing, catalog metadata, subscriptions and cycles, orders and refunds,
redemptions, wallet transfers and invoice records, current group buys and blind
boxes, channel-market ownership/pools/settlements, ratings, passkeys and OAuth
bindings, original scoped security audits and durable request restrictions.
The native request guard consumes imported strikes, restriction times and blocked
states when `REQUEST_ABUSE_GUARD_ENABLED=true`, preserving the existing default.
Available historical logs and canonical ledger entries are mapped to
separate historical tables; they never debit the wallet a second time. Current
funding attribution and monetary reward transfer holds remain distinct from
wallet openings. Every populated unsupported operational source explicitly
blocks application until its native contract and mapping exist.

Monetary v2 quota units convert exactly to micro credits (`× 2`); USD fields use
exact decimal arithmetic. Each account amount must fit bigint; report aggregates
use arbitrary precision. The canonical current wallet is the
`billing.balance_snapshots` entry for `claude_wallet`. Projection differences,
nonzero active reservations, invalid money and overflows block application.
Immutable opening entries are idempotent. Reimport never resets a used balance.

The old points/GPT wallet, bonus-point balances, obsolete `blind_box_credits`
and pet systems are retired by product scope. Their source rows/amounts appear
under `retired_features`, stay in the read-only source, and never become current
wallet money. Their positive balances do not block migration.

Reset opportunity accrual/history remains in the source and is counted under
`deferred_game_history`. Its original lifetime use records still set the retained
subscription financial restriction, so an old used subscription cannot regain
conversion rights. Paid orders retain actual provider transaction IDs separately
from signed merchant references; missing historical transaction evidence is counted
as `orders_missing_provider_transaction` and remains unavailable for refund.

Persisted rates, cache prices, group multipliers and billing expressions migrate
to the catalog. Missing options use frozen v2 built-ins. Imported prices retain
`money_quantum=2`: rational arithmetic rounds in original quota units before
conversion to micro credits. Native prices retain their one-micro precision.
Image count and video duration units retain their billing-unit metadata.

Before starting v3 writers, run `migrate check` with both DSNs and the target
encryption key. It compares expected typed projections, IDs, relationships,
money, row counts, original decrypted credentials and all live ledger balances.
Changing or dropping a mapped target record fails reconciliation. After target
business activity starts, current states legitimately diverge from the frozen
source; use `migrate ledger-check` for the target-only ledger balance check.
The gateway and worker load committed ledger/catalog state on startup; migration
requires Redis only when the source contains retained background results.

Verification:

```text
go test ./internal/legacy ./cmd/migrate
V3_MIGRATION_TEST_PG_DSN=postgres://... go test -race -tags=pgintegration ./internal/legacy ./cmd/migrate
```

The integration DSN must name a disposable dedicated PostgreSQL database with
permission to create independent fixture databases. Tests prove source-login
write rejection, dry-run, atomic application, repeat import, native credential
use, reconciliation failures and monetary boundaries.
