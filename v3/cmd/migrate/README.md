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

Completed or failed video/music tasks retain their original public task ID,
owner, model, provider result, dates and exact historical credits in the native
read-only task history. Terminal workflow records, snapshots and settlement
results are retained with each task. Existing task-query endpoints read this
history under the current owner's authentication and model policy. Historical
results expose their original provider URL; its original availability/expiry
still applies. No old credential, reservation, upstream dispatch or second
debit is created. Historical `/content` proxy requests return 410 and direct
callers to the result URL. Pending tasks/workflows, missing terminal settlement,
inconsistent owners or duplicate public IDs block import. Every unknown populated
application table appears in `unmapped_sources` and blocks application; empty
unknown tables and explicit schema-tool bookkeeping contain no customer data.

The source coverage report also distinguishes records deliberately retained in
the frozen v2 backup from native current state:

| Source | Contract and report |
| --- | --- |
| `gateway.execution_attempts`, `gateway.route_plans`, `gateway.usage_evidence` | Old post-settlement Temporal evidence, not routing configuration. Every row needs a settled parent or a parent whose existing canonical request/account/reservation/settlement ownership, per-reservation amount identity and total funding debit prove completion. Subscription additional reservations are summed by request and funding account; raw execution/usage quota is distinct from the funding debit after scaling or caps. Present raw evidence must agree with its execution; terminal billable audit or the single replacement consumption log must agree with the total funding debit. Missing identifiers, conflicting links/amounts and orphan children block application. Counts use `archived_source_history.gateway_*` and `archived_projection_diagnostics.gateway_request_executions`. Original execution trees remain in the v2 backup; no money is replayed. Flat `gateway_*` names follow the same contract. |

An old `in_flight` audit is only admitted when canonical funding is terminal
with matching ownership and no pending provider work, durable final refusal /
terminal background evidence proves no-money completion, or the frozen source
proves no traceable request obligation. The last branch requires `billable=false`
and zero quota; an existing user and its API key; no reservation, reverse
settlement usage reference, ledger reference (including unknown/NULL reference
types), v2 subscription/wallet operation prefix, execution/usage/economics,
funding allocation, subscription pre-consume, provider task/workflow/background
or billing outbox reference. Present unknown schemas and malformed evidence
block this branch. Reverse financial checks are materialized set operations,
not an unindexed full-table scan for each audit. The target status is
`historical_unknown`, excluded from audit success scoring; original amounts and
missing completion time remain unchanged. This status does not suppress the
independent usage-log fallback. An old date or zero global reserved balance
alone never admits an unknown record. Source records are never rewritten.

V2 finite API-key adjustments use token references and random operation IDs,
without a request link. The migration preserves the current key budget and
requires agreement with its canonical account snapshot; it cannot establish
whether a particular unknown HTTP request previously debited/refunded that
budget. It never infers or replays a compensating adjustment from unknown HTTP.

Before applying the offline import, independently verify actual Temporal
executions are closed after stopping all source writers and schedulers. Paginate
the real namespace's Running visibility results and describe their exact runs:
visibility can still list a completed run. Expired retention does not prove an
old HTTP outcome. This operational freeze check is required in addition to the
database drain contract; `-offline` must not be used while source work continues.

The source's published billing outbox is routinely purged after 72 hours, and
request economics is optional when no procurement multiplier is configured.
Missing old outbox/economics projections do not undo canonical settlements;
present pending outbox entries still block the import.
| `balance_blind_box_simulation_sessions`, `balance_blind_box_simulation_batches` | Stored temporary simulation sessions/results were retired in v2. They never created real wallet credits, real prizes or real pity state. Counts use `archived_source_history`; even an old active simulation grants no v3 entitlement. The native simulator starts from its own current rules. |
| `community_resources` | The GitHub-contribution submission/review feature and its routes were removed in v2 before this rebuild. Original submissions, reviews and reward evidence stay in the v2 backup, counted as `archived_source_history.community_resources`. This is separate from the retained NodeBB identity/rating bridge. Already credited rewards remain represented by canonical wallet/funding facts; no reward is reissued and no pending submission is treated as a payout. |
| `quota_data` | Old hourly usage read projection, counted as `rebuilt_read_projections.quota_data`. Native reports compute from imported detailed usage facts. Older rollups beyond the retained detailed-log range remain in the source backup; they are not a wallet balance or an additional usage debit. |
| `setups` | Old installation version/time metadata, counted as `archived_source_history.setups`. Root/user permissions come from imported identities, not this flag; v3 schema installation has its own revision tracking. |
| `wallet_quota_conversions` | Completed old GPT/Claude dual-wallet 4:1 conversion audits. Any non-completed state blocks application. Counts use `archived_source_history` and `retired_features`, with all recorded before/after and source/target amounts reported in original v2 units. The conversion was already applied in v2; it must never run or issue credits again. |

These contracts do not authorize deleting source tables or their backups. They
are fixed to the named records and their verified old consumers; an unrelated
populated table remains an application-blocking `unmapped_source`.

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

Funding attribution, channel settlements and operational histories are read
from the same source snapshot without retaining their full tables in memory.
Native writes and SELECT-only reconciliation use batches of at most 512 rows
and 4 MiB of projected JSON; a larger individual record is processed alone.
Account mappings are loaded once. Source-wide uniqueness, allocation totals
and duplicate usage identities are checked by PostgreSQL, where aggregates
can spill to disk. Reports keep representative historical errors and exact
affected-row counts; any mismatch still blocks application. These bounds do
not predict the duration or disk space of a production migration. Measure both
using the full backup before scheduling the offline window.

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
