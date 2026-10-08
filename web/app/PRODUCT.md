# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Two peer audiences share one product, and neither is a guest of the other.

**逛 (browse and buy).** Developers and API consumers who arrive to shop: compare model groups and channels, read a multiplier and a price, top up a wallet, redeem a code, open a blind box, and copy an endpoint. They scan listings, compare sellers, watch price and stock, and complete a purchase in a few minutes.

**管 (operate and manage).** Site operators and admins who keep the console open for hours: channel health, latency, error rate, spend, route pools, model metadata, users, redemption codes, subscriptions, settings. Dense tables, filters, bulk actions, long sessions on a second monitor.

## Product Purpose

CodeGo AI is a unified AI API gateway operated by CodeGo AI Limited / 码高智能有限公司, a Hong Kong company: one OpenAI-compatible endpoint and key in front of many upstream models, with routing, billing in micro credits, quota, a channel marketplace, and admin operations. Success: a visitor finds a model at a visible multiplier, gets a key, and makes a call in minutes; an operator sees state and acts without hunting.

## Positioning

A public, verifiable channel marketplace: every group exposes its multiplier, declared models, verification status and latest probe result, so buyers compare suppliers before choosing a route.

## Operating Context

Public site: `/`, `/models`, `/channel-market`, `/docs`, `/status`. Console behind sign-in with grouped sidebar and Ctrl/⌘K command palette; active console destinations are registered in `src/lib/navigation.ts`; retired URLs redirect to current destinations. Public data available without auth: `/api/marketplace/groups`, `/api/marketplace/group-status`, `/api/marketplace/multiplier-trends`, `/api/marketplace/models`, `/api/packages/public`, `/api/status`.

## Capabilities and Constraints

- Preserve active capabilities and existing financial rights. Group buy, lucky draw and desktop UI are retired per user request; backend history remains. Community is the external topbar link https://community.codegoai.com. Invoices belong to billing details and paid orders produce downloadable PDFs.
- One global top bar shared by public site and console; the console adds a sidebar below it (confirmed 2026-10-03).
- Logo links always return to the public home `/`. Buyer market remains `/channel-market`; suppliers use the separate `/my-channels` workspace from the sidebar or market shortcut. `/playground` is labelled 对话, with request-scoped group and model selection.
- Show the original public numeric channel ID in market, home and conversation selectors. Keep internal group/routing identifiers and bindings stable. Published name and remark changes require joint admin review while approved text and service remain active; reject contact details, ads and impersonation on the server.
- Suppliers select up to five model-provider tags such as OpenAI and Anthropic from a fixed vocabulary. Tags support filtering and authorized recommendations; self-selected labels do not certify upstream identity.
- Personal route pools support ordered membership and server-side automatic rebuilding. Show last/next rebuild and errors; no-candidate failures preserve members. Daily schedules use explicit UTC, and recent request strips use six real hourly outcome buckets rather than probe results.
- Commercial invoices require purchaser name and address on first self-service issue, then preserve the exact issued PDF. Issuer address: UNIT 1618A, 16/F, PIONEER CENTRE, 750 NATHAN ROAD, MONG KOK, HONG KONG. No Mainland tax ID or manual approval; ownership, paid and refund checks apply on every download.
- Console uses task-first neutral surfaces: account summary and integration workspace, flat report metrics, aligned drawer/settings forms, and wallet section links without hiding subscriptions or legacy conversion rights. Preserve permission groups and all existing actions.
- Home uses “选择模型，开始构建。” with a search-led cover, a real model directory, public group comparison and copyable integration examples. Avoid repeated promotional headings and registration calls; models retain the full pricing destination.
- No new dependencies; icons from `lucide-react` only. Charts are hand-written SVG.
- Amounts stay in micro-credit bigint/strings; never coerce int64 to Number for arithmetic.
- Chinese source copy through `t()`; English in `src/locales/en-*.ts`, seven other dictionaries in `src/locales/*.json`. Support zh-HK, zh-CN, en, ja, ru, ko, fr, de and ar. Respect saved choices, detect the browser on first visit, use Hong Kong Traditional Chinese when no supported language matches. Shared menus use native language names. Arabic uses RTL; code, identifiers and the English brand remain LTR.

## Brand Commitments

Product name CodeGo AI with an original geometric CG gateway mark and copper route endpoint. Company identity: CodeGo AI Limited / 码高智能有限公司 · 香港. Neutral white/gray surfaces and charcoal dark mode in `src/styles/tokens.css`; copper is a brand detail, while primary actions and navigation use neutral contrast (confirmed 2026-10-03). Information architecture references OpenRouter; restrained typography, image-led sections and spacing reference OpenAI. No "built on new-api" copy or QuantumNous attribution in the product UI, as explicitly requested; existing legal license files stay intact. Do not invent a registered address, registration number, certifications, or service guarantees.

## Evidence on Hand

Live counts and lists from the public endpoints above. No customer logos, testimonials, uptime SLAs, or benchmark figures exist; do not invent them.

## Product Principles

- Hold 逛 and 管 as peers: one token set, two density tiers.
- Real merchandise first: models, groups, multipliers, verification, probe latency. A surface whose largest type carries no product fact has failed.
- Financial decisions in direct, auditable flows with visible price, quota, validity, and payment state.
- Console labels and data carry meaning without repetitive explanatory subtitles. Public home, help and policy pages use concise body copy to explain pricing, group selection and service boundaries (confirmed 2026-10-04). No "不是…而是…" sentences.
- One page-header, card, tab, and empty-state pattern across all routes.

## Accessibility & Inclusion

Keyboard-reachable controls, visible focus, localized copy, reduced-motion-safe interactions. Body text 4.5:1, large text 3:1; accent used as text only where it meets that floor. Payment and quota states expose clear success, pending, and failure feedback.
