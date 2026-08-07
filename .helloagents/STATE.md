# Main Goal
Prevent GPT stream worker leaks and automatic-route candidate exhaustion from returning avoidable Codex 503 responses.

# Current Work
- Stream lifecycle repair is committed as `d57c72e19` and deployed: production gateway runs `sha-d57c72e-gateway-api-amd64`; rollback container is `new-api-v2-gateway-pre-sha-d57c72e-20260807T085510Z`.
- The scanner now closes an upstream body after any handler/ping/worker stop, owns writes in one data worker, and cancels pacing on abnormal ends. Normal `[DONE]`/EOF drains already-read frames.
- Diagnosed user `chuyuxuan` (internal ID 2704): its main Responses streams succeed and are recorded in the usage log, but follow-up Codex requests hit an all-cooling/probe-capacity window and returned an immediate sanitized 503 before a channel was selected.
- Local follow-up adds two context-aware route-selection waits (500ms then 1s) only before a channel is acquired. It records `no_selectable_candidate` in the internal route audit and does not replay semantic output or change image-generation behavior.

# Verification
- `go test ./internal/gateway/... -count=1` passed after the route-selection change.
- `go test ./internal/gateway/stream -count=5` passed for the scanner repair.
- `git diff --check` passed.
- Full-repository test baseline still has the unrelated stale subscription-order expectation failure.

# Next Actions
- Commit and push the bounded route-selection wait.
- Deploy only the resulting gateway image, retaining the current rollback container.
- Observe channel 67/32 worker shutdown logs, candidate exhaustion, user 2704's Codex follow-ups, and gateway restart count for at least 15 minutes.

# Blockers
- No active implementation blocker.
