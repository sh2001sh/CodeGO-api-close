# Main Goal
Repair the full-release verifier's lazy-account false positive and upgrade all four v2 services together.

# Current Work
- The verifier now requires a billing account only for balances or historical usage that must already have been migrated.
- `VERSION` is prepared as `v2.0.0-rc.34.8`.

# Key Context
- Gateway currently runs `v2.0.0-rc.34.7`; control, ledger, and workflow run `v2.0.0-rc.34.6`.
- The prior full deployment rolled back safely because new zero-balance users, tokens, and subscriptions are designed to create ledger accounts lazily.
- The production database backup and prior ledger backfill were completed before this release.
- Deployment script `/mnt/codego-data/deploy-rc34.7.sh` has verified migration, verification, health checks, and rollback behavior.

# Verification
- `go test ./cmd/v2-verify` passed.
- `go test ./internal/gateway/runtime ./internal/gateway/routing/app` passed.
- `git diff --check` passed.

# Next Actions
- Commit and tag `v2.0.0-rc.34.8`, push to GitHub, wait for multi-architecture images, then deploy all four v2 services with the existing guarded deployment process.

# Blockers
- Awaiting remote image build after the release is pushed.
