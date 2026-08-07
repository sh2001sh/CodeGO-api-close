# Main Goal
Rebuild GPT streaming failure handling so safely replayable upstream failures switch routes, while all unrecoverable upstream errors remain sanitized to the existing user-facing 503 contract.

# Current Work
- Implemented Phase 1 of `docs/gpt-stream-routing-redesign.md`; changes are local and uncommitted.
- Added `AttemptStage` as the request-scoped source of truth for selected, connected, bootstrap, semantic-committed, and completed Responses stream states.
- A Responses stream with lifecycle-only events can retry. Once a text delta or tool call is about to be written, it becomes non-replayable before the write to avoid duplicate output.
- Pre-semantic incomplete streams exclude the current fault domain before retry. Post-semantic failures do not replay.
- Unrecoverable remote 5xx failures are normalized to the existing sanitized HTTP 503 message before downstream response commitment. A committed stream emits an equivalent sanitized SSE error event instead of silently closing.
- Admin error audit now includes `attempt_stage`; upstream/channel details remain internal.

# Verification
- `go test ./internal/gateway/...` passed.
- `go test ./types` passed.
- `git diff --check` passed.
- `go test ./...` has one unrelated existing failure: `internal/commerce/transport/http: TestGetSubscriptionOrderStatusReturnsOrderPayload`; it receives `Standard月卡` and fails its stale expected payload assertion.

# Next Actions
- Review and commit only the gateway/error changes plus `docs/gpt-stream-routing-redesign.md`; do not include `artifacts/` or `scripts/audit-upstream-api.ps1`.
- Push and build only after an explicit release request, then verify the stage and retry audit fields in production.

# Blockers
- No implementation blocker. Full-repository test baseline needs the unrelated subscription order test expectation corrected separately.
