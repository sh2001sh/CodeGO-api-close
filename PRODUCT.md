# Product

## Register

product

## Users

Two peer audiences share one product, and neither is a guest of the other.

**逛 (browse and buy).** Developers and API consumers who arrive to shop: compare model groups and channels, read a multiplier and a price, join a group-buy room, top up a wallet, redeem a code, open a blind box, and copy an endpoint. They behave like marketplace shoppers — scanning listings, comparing sellers, watching price and stock, and completing a purchase in a few minutes.

**管 (operate and manage).** Site operators and admins who keep the console open for hours: channel health, latency, error rate, spend, route pools, model metadata, user accounts, redemption codes, subscriptions, and system settings. They behave like console operators — dense tables, filters, bulk actions, and long sessions on a second monitor.

## Product Purpose

new-api provides a unified AI API gateway with authentication, billing, quota management, model availability, and administrative operations.

The product presents itself as **an AI resource marketplace and an operations console, held as two peer zones**, switched at the top level and sharing one token set across two density tiers. The 逛 zone carries listing, price, stock, seller and promotion signals in the vocabulary consumer marketplaces already taught this audience. The 管 zone carries tables, filters and state in the vocabulary operations consoles already taught this audience. Neither zone is nested inside the other, and neither borrows the other's density.

## Brand Personality

Practical, trustworthy, efficient. Commerce surfaces may carry the energy that price, scarcity and reward legitimately have. Management surfaces stay quiet, dense and auditable. Confidence in both comes from precise numbers and unambiguous state.

## Anti-references

Avoid game-like or ornamental surfaces for core billing flows, hidden purchase paths, decorative motion around payment, and marketing-heavy card grids that obscure prices, quota, validity, or risk.

Avoid slogan pages: a surface whose largest type carries no product fact has failed. Avoid descriptive body text, explanatory subtitles and guiding small print anywhere in the interface; labels and real data carry the meaning. Avoid sentences built on a "不是…而是…" contrast.

Do not remove or replace protected new-api or QuantumNous project identity.

## Design Principles

- Hold 逛 and 管 as peers: one top-level switch, one token set, two density tiers, and no zone rendered as a sub-page of the other.
- Put financial decisions in direct, auditable flows with visible price, quota, validity, and payment state.
- Lead every commerce surface with real merchandise — groups, channels, packages, prices, multipliers, stock — rather than with a headline about them.
- One page-header pattern, one card treatment, one tab treatment, one empty-state treatment across all routes. Consistency outranks per-page invention.
- Make growth mechanics explicit without coercion: group-buy bonuses, blind boxes and renewal rewards stay discoverable while ordinary purchase stays the shortest path.
- Preserve admin capability and database compatibility while simplifying the ordinary user surface.
- Icons come from `lucide-react` only, at one stroke weight.

## Accessibility & Inclusion

Maintain keyboard-reachable controls, visible focus states, localized copy through the existing i18n system, and reduced-motion-safe interactions. Body text and placeholders clear 4.5:1 against their own ground, large text clears 3:1, and the brand accent is only used as text where it meets that floor. Payment and quota states expose clear success, pending, and failure feedback.
