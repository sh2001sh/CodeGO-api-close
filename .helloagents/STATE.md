# Main Goal
Refactor the model marketplace to distinguish CodeGo official groups, third-party groups, and a user-configurable third-party Auto group.

# Current Status
- Complete: backend Auto route pool model, APIs, token group support, candidate routing, billing context, and tests.
- Complete: model marketplace has CodeGo official, third-party group, and third-party Auto views.
- Complete: users can build a personal Auto pool from eligible third-party groups.
- Complete: API key creation exposes third-party Auto and direct third-party groups.
- Complete: SQLite startup creates the Auto route pool table without PostgreSQL or Redis.
- Complete: desktop and 390px mobile browser validation passed after fixing Tabs height overflow.

# Key Decisions
- Auto token group is `market:auto`.
- Each user maintains a personal pool of eligible marketplace groups.
- Auto routing only considers pool members supporting the requested model.
- Candidate score is multiplier divided by conservative availability squared; lower is preferred.
- Third-party calls always consume general quota and retain the existing 5% marketplace commission flow.
- Local SQLite remains a supported simple test path; Redis is optional.

# Verification
- Focused Go tests passed for marketplace app/HTTP, platform store/middleware, and identity HTTP.
- Frontend ESLint, typecheck, and production build passed.
- Local `/api/status`, `/api/marketplace/groups`, and `/pricing` returned HTTP 200.
- SQLite table `marketplace_auto_route_pool_members` exists after normal local startup.
- Playwright desktop/mobile screenshots show no overlap; browser console has no errors.
- `git diff --check` passed.

# Next Actions
- None for this feature. Local service remains available at `http://127.0.0.1:3000`.

# Blockers
- None.
