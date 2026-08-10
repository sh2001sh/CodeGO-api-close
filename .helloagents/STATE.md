# Main Goal
Deploy the current invoice-flow, Guard observability, and Codex remote compaction v2 compatibility changes as one verified production image.

# Current Status
- Current worktree includes the user-owned invoice UI/backend flow updates, dashboard group overview work, Guard audit telemetry, and remote compaction v2 stream compatibility fix.
- Guard changes: bounded content-free decision ring, root-only `/api/prompt-audit/metrics`, timeout counter, and controlled fixture coverage.
- Remote compaction v2 now forwards `response.output_item.added` with item type `compaction` immediately, rather than buffering it until `response.completed`.
- Invoice admin issuance now requires an invoice number only; separate delivery URL/email metadata was removed from the workflow and its focused test updated.
- Gateway-related Go tests passed, including focused suites repeated 20 times. Full backend/frontend validation and deployment remain pending.

# Next Actions
- Run full Go and frontend build validation for all current changes.
- Commit the current validated worktree, push `v2-refactor-20260711` to origin to trigger the image workflow, then wait for a successful image.
- Deploy the built image to production using the repository production workflow/compose process and verify control, gateway, invoice, and health routes.

# Blockers
- None. The user explicitly authorized commit, build, and production deployment.
