# v2 to v3 migration

## Online staging and the final offline window

The staged path moves the historical bulk copy before the maintenance window.
It captures source changes without posting money to v3. Formal full verification
requires every v2 writer to stop, capture to seal and pending changes to reach zero.
It does not stop writers, switch traffic, waive financial drain checks, or prove
that the production cutover takes only minutes. Keep v2 serving users until an
independent rehearsal proves capture overhead, target capacity, final sync time,
backup restoration and all application checks on the actual release.

Use a unique `V3_ONLINE_MIGRATION_ID` containing 16–64 letters, digits, hyphens
or underscores. Keep it unchanged across retries and backup/delta files. Use
`V3_SOURCE_PG_DSN` for source reads and `V3_PG_DSN` for the separate,
non-serving v3 target. Source business reads always use READ ONLY REPEATABLE
READ transactions. The private capture namespace requires its owner login;
granting another reader access to capture metadata is rejected. The encryption
inputs below remain required. Set
`V3_ONLINE_SOURCE_ADMIN_PG_DSN` only for capture setup, acknowledgement and
seal/unseal: this login can install the capture schema/triggers and write its
metadata. It must not be supplied through command arguments or stored in logs.
All CLI pools are limited to two connections.

All three migration logins require EXECUTE on `pg_control_system()` to establish
the real cluster and database identity. The reader and capture administrator
must reach the same actual database. One capture run is permanently bound to
one actual target database; another target cannot consume its acknowledged
change journal. Native and staging schema fingerprints are checked on each
operation. A schema change requires a new isolated target and capture run;
staging from an earlier schema must not replace upgraded native tables. Custom
access policies, grants or ownership on tables being adopted are explicitly
refused rather than lost by table replacement.

The historic `online-empty-schema-upgrade` command applies only the approved
exact-price revision 108. It does not apply retention revision 109 or later
revisions. A migration using a history cutoff needs a new, fully migrated target;
do not resume an older captured run by applying unrelated schema changes.

| Command | Required flags | Effect |
| --- | --- | --- |
| `online-prepare` | None | Preview source coverage and target staging requirements. |
| `online-prepare` | `-apply` | Install source capture and prepare isolated staging. |
| `online-copy` | `-apply` | Copy the historical bulk into target staging. |
| `online-sync` | `-apply` | Apply captured changes and acknowledge committed work. |
| `online-verify` | `-apply` | Run full verification and retain verification evidence. |
| `online-backup` | `-apply` | Create a snapshot-bound legacy base archive. |
| `online-delta` | `-apply` | Export a cumulative source delta for isolated restore. |
| `online-restore-delta` | `-apply` | Apply a delta only to an explicitly named isolated restore. |
| `online-seal` | `-apply -offline` | Seal capture after every v2 writer has stopped. |
| `online-finalize` | `-apply -offline` | Sync a sealed source, enforce offline contracts and atomically finalize. |
| `online-unseal` | `-apply` | Remove this capture's write seal; no v3 transactions are reversed. |

Only `online-prepare` has a preview mode. Commands that write staging, metadata,
verification evidence or backup files require `-apply`. `online-seal` and
`online-finalize` additionally require the operator's `-offline` assertion; all
other online commands reject `-offline`. Invalid command/flag combinations are
rejected before any database connection. Capture installation itself briefly
locks tables: validate its overhead and schedule it outside peak traffic.

Run prepare, bulk copy, repeated sync and full verify while v2 stays live. Full
verification can still take substantial time; do not defer it to the final
window. Changes continue to be captured during online work. Do not start v3
writers against the staging target. At the agreed maintenance time, freeze new
admissions, drain real provider/billing work, stop every v2 writer, seal capture,
then run the final sync and finalize. A missing seal, financial blocker or
verification failure prevents finalization. Independent asset backups, account
and ledger checks, release checks, capacity checks and candidate acceptance are
still prerequisites before routing users to v3. Keep the original offline import
path below available as a fallback.

Finalization holds a real source database lock until the target commit, so a
concurrent unseal cannot reopen v2 while its frozen balances are being imported.
Failed finalization releases that lock but leaves the write seal in place.
Staging and its accounting receipts require migration transaction ownership;
ordinary application writes and replication mode cannot bypass their guards.
Each delta is applied in bounded batches within one target transaction, with
exact event acknowledgement only after commit. Individual source or projected
records above 64 MiB fail explicitly and remain unacknowledged. The final
window still validates foreign keys and financial SQL across the actual data;
moving JSON projection online does not by itself eliminate those scan costs.

`online-unseal` is for resuming v2 during an aborted cutover. It only cancels the
capture seal and does not restore deleted data, refund debits or migrate v3
transactions back into v2. Once v3 accepts business writes, rolling back to a
v2 snapshot can lose those writes; an independently verified reverse migration
is required for a lossless rollback.

### Base archives and cumulative deltas

`online-backup -apply` writes `V3_ONLINE_BACKUP_PATH`. The destination must be a
new absolute file on the designated backup disk; the parent must exist without
symlinks. `pg_dump` must be available and compatible with the source PostgreSQL
version. The exported source snapshot stays open until the archive finishes.
The backup report records snapshot, capture hash, bytes and archive SHA256.
Capture schema data is excluded. Its source capture trigger names are listed in
`excluded_trigger_names`; remove their trigger TOC entries when restoring the
archive, since the capture implementation is not part of the restored business
database. Preserve all unrelated triggers and constraints.

`online-delta -apply` writes a new `V3_ONLINE_DELTA_PATH`. A private mode-0600 file
is written, flushed and atomically published without replacing an existing
destination. Interrupted export leaves no published partial archive. On Windows,
also restrict the backup parent directory with private NTFS ACLs; Unix mode bits
alone do not protect Windows archives. The delta is cumulative from capture
installation, including keys already acknowledged
by online sync; it is not a maximum-sequence watermark. Late commits, updates,
deletions and tables without primary keys remain part of its reconciliation
contract. Each later export needs a new destination file.

Restore the base into a disposable database named `online_restore_<suffix>`.
Set `V3_ONLINE_RESTORE_DATABASE` to that exact name and
`V3_ONLINE_RESTORE_PG_DSN` to its independent connection string. Then run
`online-restore-delta -apply` with the matching run ID and existing delta file.
The CLI checks the database name before connecting; the restore API checks the
actual database again. It accepts neither the source business database nor the
v3 serving target. Delta validation spools beside the delta archive rather than
using the system temporary disk. Keep sufficient space there for the validation
spool and the archive. Restore verifies the transport before writing and keeps
business rows and sequence changes inside its isolated restore contract.

A delta file alone is not a backup. Restore the actual base plus final cumulative
delta in isolation, check source identities, rows, relationships, constraints,
sequences and financial totals, and retain both archive hashes. Protect archives
with private filesystem permissions and preserve independent API-host and local
copies, including configuration, user assets, invoice originals and mail receipts.
For this deployment the local database backup root is `D:/CodegoBackups`; do not
use the system C drive. Recalculate disk/WAL/temp capacity and verify both copies
before cutover. These commands do not copy the backup to another host or
automatically change Nginx.

## Original atomic offline import

Set `V3_SOURCE_PG_DSN` to the v2 PostgreSQL database with a read-only login and
`V3_PG_DSN` to the separate v3 database. Set `V3_SECRET_KEY` to the target's base64
AES-256 encryption key. Source channel-market credentials additionally require
`V3_MIGRATION_SOURCE_CRYPTO_SECRET`, the old encryption secret; retained background
results use the same source secret. Reports expose
IDs, counts and amounts, never passwords, API keys or provider credentials.

`migrate drain` is a source-only canonical readiness audit. It needs only
`V3_SOURCE_PG_DSN`, uses a READ ONLY REPEATABLE READ transaction, and never opens
target PostgreSQL, Redis or file stores. It evaluates known-source coverage and
the same financial, workflow and execution/audit drain contracts as the normal
importer. Pending background assets do not stop inspection of later blockers.
The source login must also have EXECUTE permission on `pg_control_system()` to
bind the report to its actual cluster; unavailable identity or SQL checks fail
closed with `completed=false`.

The JSON protocol is `codego-v2-canonical-drain-v1`. Exit 0 means the audit
completed with no issues; exit 3 means the complete report contains blockers;
exit 1 means invalid input or incomplete execution. The command accepts no
`-apply`, `-offline`, skip or ignore flags. `V3_DRAIN_SOURCE_SNAPSHOT` may bind an
existing exported snapshot while its owning transaction stays open, and
`V3_DRAIN_FROZEN_MANIFEST_SHA256` binds an independently verified frozen manifest
without authorizing any write. Both inputs are validated before source access.

Reports include source identity, default TimeZone, snapshot and audit times;
the actual running executable SHA256; all pending background identities and
complete original-row SHA256; and every undrained execution/audit with its
matching background dependency hash. Row hashes use PostgreSQL
`encode(sha256(convert_to(to_jsonb(row)::text,'UTF8')),'hex')` without revealing
encrypted content. `pending_background.sha256` hashes the UTF-8 compact JSON
array of sorted 12-character MD5 request hashes. `source.identity_sha256` hashes
UTF-8 compact JSON with sorted keys and no ASCII/HTML escaping for `database`,
`database_oid`, `server_addr`, `server_port`, `server_version_num`,
`system_identifier` and `time_zone`.

The SHA-bound `projection_assertion_sql` is generated by the pinned executable
from the same canonical predicates. An independently verified recovery tool may
execute this read-only assertion inside its existing recovery transaction to
see its own uncommitted writes. Any pending background or undrained canonical
projection raises `canonical_post_drain_failed` and rolls back that transaction.
The assertion does not authorize accepting blockers: recovery-specific original
row proofs, frozen manifest, stopped writers, financial fingerprints and locks
remain external requirements. After a committed recovery, run normal `drain`
again and retain a complete zero-issue report before importing.

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

Set `V3_MIGRATION_LEDGER_HISTORY=archive` to retain the original ledger in the
frozen source instead of projecting historical entries. The default is `copy`;
online runs bind this mode at preparation and reject a changed mode on resume.
Archive mode still imports current balances, funding attribution and historical
account ownership. Ordinary histories follow the optional cutoff below. Subscription reward
calculation continues to read original ledger evidence. Reports bind the source
cluster, database OID/name, preserved row count and archive timestamp; complete
backup hashes and restore verification remain required to prove source content.
Keep the source immutable after cutover, and configure the control service's
dedicated SELECT-only `V3_LEDGER_ARCHIVE_PG_DSN` as described in its README.
Historical entries never become new wallet money in either mode.

Set `V3_MIGRATION_HISTORY_CUTOFF` to one explicit past RFC3339 timestamp with
whole seconds (for example, the start of the migration minus 30 days). Omit it
to retain all history. Import, check and every online command must use the same
value; the target binds it immutably, including the all-history choice. A changed
cutoff requires a new independent migration. This is a data-selection boundary,
not permission to delete source tables or backups.

Only ordinary logs, request audits and retry attempts are filtered. Recharge,
quota-management and refund events (v2 types 1, 3 and 6), money provenance and
settlement evidence remain. Unknown or unfinished request outcomes remain,
and recent children keep their old parents and complete retry families. Recent
attempts with genuinely absent parents remain in the orphan archive.

Expired usage is reduced to exact per-user/per-Key totals, without issuing
credits or changing wallets, prices or budgets. Native Key lifetime usage adds
these totals to retained usage. The final frozen import computes and checks the
aggregate against the same source snapshot; measure this scan in the maintenance
window budget. Source financial/workflow drain checks remain mandatory. V2
channel batch tests are in-memory and must be drained before process shutdown;
V3 durable batch recovery receipts are protected by runtime retention.

After cutover, the worker cleans ordinary histories older than 30 days in short,
bounded transactions. Open reservations, unfinished workflows, pending seller
settlements, recoverable batch receipts and unknown request outcomes retain their
evidence. Deleted usage and lifetime totals change atomically. Only empty,
fully expired managed usage month partitions are removed; the boundary month
and DEFAULT remain. Row deletion alone does not return filesystem capacity.
Audit samples also default to 30 days; `V3_AUDIT_SAMPLE_RETENTION_DAYS` overrides
their period only. Keep workers stopped throughout migration and verification.

Model prices are quoted and edited in credits: an old $1 model price remains
1 credit with the same token, request or media billing unit. Input, output and
cache rates use credits per million tokens; per-request prices use credits per
request. Group multipliers and discounts retain their value. Native price
integers remain micro credits, so 2.5 credits stores as 2,500,000 without changing
the charge. Administrator price inputs perform this conversion exactly.

Monetary v2 quota units convert exactly to micro credits (`× 2`); legacy USD price
fields convert numerically to credits using exact decimal arithmetic. Each account amount must fit bigint; report aggregates
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
