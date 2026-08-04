# Main Goal
Add durable, user-visible notifications for daily lucky-number rewards without changing draw rules or reward amounts.

# Current Work
- Lucky-reward notifications are implemented and ready to release.
- The web app shows an actionable winning toast after login and a persistent unread trophy entry in the app header.

# Key Context
- Production runs `v2.0.0-rc.34.8` across gateway, control, ledger, and workflow.
- Lucky-number probability, reward values, and historical draws must not change in this task.
- Existing reward settlement is idempotent through the wallet credit key; notifications require a unique reward reference for equivalent protection.

# Verification
- `go test ./internal/commerce/app -run "TestDailyLuckyRewardSettlementWritesLedgerOnce|TestListDailyLuckyNumberPublicWinsIncludesEverySettledMatch" -count=1` passed.
- `go test ./internal/platform/store ./cmd/v2-verify -count=1` passed.
- Frontend ESLint, typecheck, and production build passed.
- The full commerce app suite still has two pre-existing group-buy settlement assertion failures, unrelated to lucky notifications.

# Next Actions
- Commit only the lucky-notification files, then push and build a new image when requested.

# Blockers
- None.
