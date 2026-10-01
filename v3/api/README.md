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

Browser community endpoints use session authentication. The rating request
contains only `stars` (1 through 5); the server derives the stable viewer subject.
The `/api/community/v1/` bridge retains its separate service credential.

Model, vendor and prefill metadata administration supports both native
`/api/catalog/` paths and retained `/api/models/`, `/api/vendors/` and
`/api/prefill_group/` aliases, all requiring an administrator session. Model
lists include bound channel and matching summaries; price rows retain their
existing flattened amounts with optional matched model metadata and vendor.
Dynamic metadata route declarations are expanded in the contract coverage test.

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
