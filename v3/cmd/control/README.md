# Control service

Build `go build ./cmd/control` from the v3 module. Run the binary with
`-addr :3002 -assets /app/web/app/dist`; the asset directory contains the built
frontend and its `.br`/`.gz` files. The service drains requests on shutdown.
`GET /healthz` checks the process; `GET /readyz` checks PostgreSQL and Redis.

Required environment:

| Variable | Value |
| --- | --- |
| `V3_PG_DSN` | PostgreSQL 15+ connection URL |
| `V3_REDIS_ADDR` | Redis host and port |
| `V3_SECRET_KEY` | Base64 of 32 AES bytes, shared with credential storage |
| `V3_SESSION_SECRET` | Separate session signing secret, base64 of at least 32 bytes |
| `V3_PUBLIC_URL` | Public HTTP(S) origin, used for cookies and same-origin checks |

`V3_DISABLE_REGISTRATION` accepts a boolean. `V3_OAUTH_PROVIDERS` accepts an
object keyed by provider, with `client_id`, `client_secret`, `authorization_url`,
`token_url`, `user_info_url`, `redirect_url` and `scopes`. Community integration
uses the independent `CODEGO_COMMUNITY_API_SECRET`.

Archive-ledger migrations additionally require `V3_LEDGER_ARCHIVE_PG_DSN` in
the control service only. It must target the original immutable V2 database
using a separate nonprivileged read-only role, with `USAGE` on `billing`,
`SELECT` on `billing.ledger_entries` columns `entry_id`, `account_id`, `amount`,
`balance_after`, `entry_type`, `direction`, `reason_code`, `created_at`, and
`EXECUTE` on `pg_catalog.pg_control_system()`. Do not reuse a migration/admin
role. The source needs an index on `(account_id, created_at DESC, entry_id DESC)`.
Source ledger row-level security must be disabled so policies cannot silently
hide financial evidence; a genuine UTC zero timestamp is rejected because it
cannot produce a compatible historical cursor.
Connections are bounded to four and enforce read-only transactions and a
10-second query timeout. Startup and historical requests verify the source
cluster and database against migration metadata; missing configuration,
identity mismatches and archive failures return errors instead of empty history.
Full-copy migrations keep their existing V3 historical reader and require no
archive DSN. Current account balances and financial transactions always use V3.

Browser community pages use session-protected `GET /api/community/sellers`
and `POST /api/community/channels/{id}/rating` with a `stars` field only.
The service derives the viewer's persistent community subject from the session;
clients cannot select another viewer. These routes work independently of the
NodeBB `/api/community/v1/` bridge and its service secret.

The NodeBB OpenID Connect provider is optional. Configure `OIDC_ISSUER`,
`OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET` (at least 32 bytes), `OIDC_REDIRECT_URI`
and either `OIDC_SIGNING_PRIVATE_KEY_BASE64` or `OIDC_SIGNING_PRIVATE_KEY_FILE`
with an RSA private key of at least 2048 bits. `OIDC_SIGNING_KEY_ID` is optional.
Discovery is served at `/.well-known/openid-configuration`; the authorization,
token, userinfo and public key endpoints are under `/api/oidc/`. Incomplete
configuration fails startup rather than enabling an unsigned provider.

`GET /api/log/token` accepts a Bearer API key and reads only that user's key
usage. Keys are checked against current database state, expiry and the direct
client address. Forwarded address headers are not trusted. All other account
and administrator APIs require a user session.

The historical audit readers `/api/audit/events`, `/api/audit/events/export`,
`/api/audit/requests` and `/api/audit/requests/{request}/attempts` also accept a
current API key, restricted to that user's current key. Session readers follow
the normal user/admin visibility. Keys cannot use wallet or administrator APIs.

For Stripe alone, set `V3_STRIPE_SECRET_KEY` and `V3_STRIPE_WEBHOOK_SECRET`
together. `V3_PAYMENT_CURRENCY` and `V3_TOPUP_CREDITS_PER_MINOR` configure its
server-side conversion; the latter is an integer amount in micro credits.

For multiple payment providers, `V3_PAYMENT_PROVIDERS` is a JSON array. Each
entry requires `provider`, lowercase `currency` and positive integer
`credits_per_minor`. Providers are `stripe`, `epay`, `creem`, `xunhu`,
`nowpayments`, `waffo`, `waffo_pancake`. Configure the matching credentials:

| Provider | Required credential fields |
| --- | --- |
| stripe | `api_key`, `webhook_secret` |
| epay | `merchant_id`, `secret`, `base_url`; currency is `cny` |
| creem | `api_key`, `webhook_secret`, `product_id` |
| xunhu | `app_id`, `secret`; currency is `cny` |
| nowpayments | `api_key`, `ipn_secret` |
| waffo | `api_key`, `merchant_id`, `private_key`, `public_key` |
| waffo_pancake | `merchant_id`, `private_key`, `public_key`, `store_id`, `product_id` |

Optional fields are `base_url`, `notify_url`, `pay_currency`, `payment_type`,
`pay_method_type`, `pay_method_name`, `sandbox`. Callbacks default to this
service's public origin. Store these credentials in the deployment secret
store. Missing payment or community credentials disable those integrations
with explicit startup warnings and service errors.

Control and worker share the same payment configuration. Currency codes accept
3–12 lowercase alphanumeric characters; USDT and USDC amounts use six-decimal
integer minor units. `credits_per_minor` always means micro credits per one
currency minor unit. Creem's optional `products` object maps each product ID to
`amount_minor` and `credits`, with optional matching `id` and `currency`; invalid
quotes fail startup. Browser-selected product prices come from this server map.

Set `refund_enabled: true` only on an Epay merchant that supports the JianPay
refund API. The default keeps this refund integration disabled. Refunds use the
same merchant credentials in both control and worker; generic Epay checkout
alone does not imply support for refunds.

Wallet payment-password email recovery is enabled with `V3_SMTP_ADDR`
(`host:port`) and `V3_SMTP_FROM`. Optional `V3_SMTP_USERNAME` and
`V3_SMTP_PASSWORD` must be configured together; `V3_SMTP_IMPLICIT_TLS=true`
selects implicit TLS, otherwise verified STARTTLS is required. The stable
session secret also signs purpose-bound recovery codes. Recovery requires the
user's current server-verified mailbox; editing the email clears verification.
No SMTP configuration leaves the recovery API explicitly unavailable.

Set `V3_INTERNAL_GATEWAY_URL` to the trusted internal gateway origin in both
control and worker to enable charged marketplace batch tests. The shared
`V3_SECRET_KEY` derives a dedicated `market-batch` key for the durable job request ID; authentication, permissions
and billing still use a short-lived group-bound API key. Each item dispatches
once and waits for its durable usage receipt. An absent gateway URL leaves this
integration explicitly unavailable; invalid configured origins fail startup.
