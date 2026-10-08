# Control API contract

`openapi.json` defines the control API paths, monetary units, request bodies,
responses and security schemes. It is served at `GET /api/openapi.json`.
`types.gen.go`, `server.gen.go` and `dispatch.gen.go` are generated artifacts.

After changing the specification, run from the v3 module:

```sh
go generate ./api
go test ./api ./internal/control ./cmd/control
```

Backend generation requires only Go and pins oapi-codegen 2.5.0. The generated
stdlib router validates path/query types before dispatching to domain-owned
handlers, which enforce authentication, ownership and state transitions.
The frontend imports `paths` and `components` from
`web/app/src/lib/api.generated.ts`. Generate that file from `web/app` with
`bun run generate:api`; `bun run check:api` verifies it is current. The local
openapi-typescript dependency also applies the frontend's lossless int64 transform.

The documented identity DTOs use `X-CodeGo-API-Version: 3`. The generated
frontend client sends this header. The identity adapter preserves the historical
numeric role/status and key-budget representations for missing/`2` headers;
the generated router therefore keeps version selection optional.

Session credentials, gateway API keys, community service credentials and OIDC
access tokens have separate security schemes. `/api/log/token` accepts a gateway
API key for read-only access to that key's usage; it does not grant session or
administrator access. OIDC discovery, token and userinfo use protocol JSON/form
responses rather than application `success`/`data` envelopes.

`/api/security-audit/` accepts sessions and returns bare event/list JSON or
`{"error":"..."}` failures. Ordinary sessions see owned marketplace events;
administrator sessions may access global events. Marketplace audit aliases wrap
the same JSON DTOs; owner aliases always retain owner scope and admin aliases
require an administrator. Event IDs retain original strings, and user, token,
channel, owner and reviewer IDs are decimal strings. Owner responses redact
upstream error bodies/messages and prompt previews. Review accepts `status` and
`note` with `review_status`/`review_note` aliases; unknown fields are ignored.
CSV export contains all filtered rows, capped at 20,000, regardless of pagination.

Marketplace ratings are readable without login through
`GET /api/marketplace/groups/{id}/rating`. POST requires a main-site session and
only `stars` (1 through 5); the server derives the viewer subject, verifies real
usage, rejects self-rating and rechecks group visibility and blocks under lock.
The route accepts stable numeric public IDs or internal group IDs. Historical
ratings and creation dates remain intact. Shop summaries weight distinct
consumers equally across public channels, independent of provider/search filters.
The session-only `/api/community/channels/{id}/rating` alias is retained;
service-credential `PUT /api/community/v1/channels/{id}/rating` returns 410.
The `/api/community/v1/` bridge retains its separate read credential.

Marketplace shop profiles use independent public numeric IDs. Proposed names
and descriptions are validated and held for administrator review; public
responses retain approved text and omit pending content. Group DTOs carry the
shop reference and public rating, and shop DTOs carry consumer rating summaries.

Route pool editors use session-only `GET /api/marketplace/route-pools/group-options`
for accessible market and authorized official candidates with enabled credentials
and models. This lightweight selection response does not aggregate request
statistics, ratings or quotations. Save the returned `group_id`; `display_id`
is only for presentation. `routing_group` identifies the model-discovery and
request-header group; the conversation page uses it for friendly labels while
the user's allowed groups and the API Key still govern actual access.
Official members retain `official:<catalog group>` identifiers. The existing
`key-group-options` endpoint retains its market Key-binding semantics.

The session-scoped notification inbox supports category/unread filters, read
and unread changes, and snapshot-bounded read-all. Read-all requires the observed
list `latest_id` as `through_id` so later messages stay unread. A cookie-authenticated
SSE connection invalidates list caches on committed changes; no periodic count
polling is required. See [notification contract](../internal/notifications/README.md).

Model, vendor and prefill metadata administration supports both native
`/api/catalog/` paths and retained `/api/models/`, `/api/vendors/` and
`/api/prefill_group/` aliases, all requiring an administrator session. Model
lists include bound channel and matching summaries; price rows retain their
existing flattened amounts with optional matched model metadata and vendor.
Dynamic metadata route declarations are expanded in the contract coverage test.

`GET /api/marketplace/channels/mine/analytics` and its `/export` CSV endpoint
share optional `from`, `to`, public numeric `channel_id` and `model` filters.
Dates use RFC3339 and a half-open `[from,to)` range, defaulting to the last
seven days and limited to 366 days. Call metrics use request time; money uses
settlement creation time and its current release/reclaim state. Missing usage
does not remove financial records. The preview contains at most 100 settlements;
CSV contains every matching row. The existing owner logs, log export and user
usage endpoints accept the same filters; log pagination retains time plus ID.
Net income is income credited to the site wallet before upstream purchase costs.

`GET /api/marketplace/groups/{id}/insights` requires a supported model and
accepts `window_hours=24|168`. It respects visibility, grants and blocks, deduplicates
recorded requests and excludes requests not counted in success-rate accounting.
Rates are fractions; TTFT is in milliseconds and TPS covers the measured output
transmission window. Missing measurements are omitted. Historical records and
buffered responses do not acquire invented streaming measurements.
Owner/admin `GET/PUT /api/marketplace/channels/{id}/disclosure` handles structured
source, region, retention, training and model capability self-declarations.
GET returns `data:null` until saved. Server-controlled provenance stays
`owner_declared`; connectivity checks do not certify upstream authorization.

`GET /api/policies/current` publishes current document versions and links.
`GET/POST /api/user/policy-acceptance` reads and records only the session user’s
explicit acceptance, preserving the original locale/time on replay. Public
password registration requires current `accepted_terms_version`,
`accepted_privacy_version` and `agreement_locale`, stored atomically with the
account. Internal/OAuth account creation does not fabricate historical acceptance.
New channel submission and an owner’s first public visibility change require
current supplier acceptance (428 if missing); existing channels remain manageable.

Redemption issues exactly one benefit: `credits` (the default), `subscription`
or `blind_box`. The issuance schema rejects mixed benefits and retired type
names. `/api/user/topup` and `/api/commerce/redemptions/redeem` return a typed
`RedemptionResult` object, including optional plan/subscription or blind-box
receipt fields. Stored historical used codes remain consumed even when their
optional receipt IDs are absent.

Plan request and response DTOs include optional `upgrade_group` and
`model_limits`. The upgrade group must exist and be at most 64 bytes after
trimming. Model limits map model names to exact int64 micro-credit caps;
negative values are rejected and zero entries are removed on save.

`GET /api/public/models` returns a safe public catalog with optional registered
model IDs, vendor metadata and exact group-adjusted token/cache/media prices.
Anonymous access exposes only publicly usable groups; sessions additionally
filter by current allowed groups and marketplace permissions. Missing prices
and dynamic formulas are explicit, rather than advertised as free. Upstream
addresses, credentials and purchase costs are absent. Model IDs support the
ordinary user's discovery-to-favorites workflow.

Root-only `GET/PUT /api/subscription/admin/wallet-conversion-review/{id}` reads
and reviews complex legacy subscription sources. A review binds the current
fact hash, revision and original paid orders; its segments must cover current
credits and future periodic commitments completely. The server validates
revenue and computes credited amounts, reviewer identity and timestamps.
Saving a review never converts the user's rights. The user quotes and explicitly
confirms through the existing wallet-conversion endpoints; quotes include
`review_id`, `future_credits` and `segments` when reviewed. Consumption, resets
or refunds invalidate unaccepted quotes. Accepted draining quotes retain their
frozen review version; completed conversion stops further usage, resets and
periodic issuance. Expired subscriptions cannot convert. Immutable review
versions and order-specific principal attribution preserve audit and refund
evidence.

Monetary integers are micro credits: 1 credit = 1,000,000 micro credits.
The wallet also returns `balance_micro_credits` as a decimal string, so JavaScript
clients retain exact balances above the safe integer range.
Audit usage, summary, event and request `amount` fields are also exact decimal
strings. `/api/billing/history` exposes retained financial evidence for the
session user using signed int64 amounts and nullable `balance_after_micro`.
Its source account IDs and `before`/`next_before` cursors are strings. Reading
history never changes the active balance or outbox.
`/api/billing/funding-economics` is restricted to the authenticated root role;
ordinary administrator sessions and API keys do not grant access. Its optional
`day` selects one valid calendar date at the Asia/Shanghai midnight boundary.
The report retains exact int64 revenue/cost amounts, signed profit and source
allocations; an empty day still returns `sources: []`. It reads settled economics
and frozen multipliers without modifying balances or attribution.
`POST /api/commerce/orders/{trade_no}/invoice` self-service issues a commercial
invoice using the purchaser's real `buyer_name` and `buyer_address`. It requires
the configured `InvoiceSellerAddress`; profile aliases and generic locations are
not substituted. One durable document per owned paid order freezes actual issue
time, purchaser/seller/order information and the exact PDF. Identical retries
return that document; changed purchaser details return 409. GET downloads the
existing PDF, or returns 428 to request first-issue details. Both return raw PDF
attachments and recheck ownership, payment and refunds on every call. No manual
review, profile mutation or payment/ledger mutation occurs. Historical manual
invoice requests remain readable through their existing endpoints.
Audit event and request lists use bounded cursor pagination and accept either
a session or a read-only API key. Keys remain scoped to their owner and key
even when their account has an administrator role. Attempts enforce ownership
through their parent request. Event CSV export follows
`X-Next-Cursor` until absent.
Legacy numeric decimal money and domain `json.Number` fields retain
`json.Number` in generated Go through `x-go-type`, avoiding float rounding.
Arbitrary JSON retains `json.RawMessage` so nested numbers in endpoint templates,
provider configuration and captured payloads preserve their original precision.

Optional nullable domain fields carry `x-omitempty: true` when their Go JSON
tags omit empty values. This preserves absent fields on a generated DTO round
trip; oapi-codegen otherwise emits null for some optional nullable fields.
